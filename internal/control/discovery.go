package control

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/cyokozai/kagelife/internal/shadermgr"
)

// DiscoveryEnv は発見ファイルの場所を上書きする環境変数。
const DiscoveryEnv = "KAGELIFE_CONTROL_FILE"

// ProtocolVersion は発見ファイルの version（制御口 v1）。
const ProtocolVersion = 1

// Discovery は発見ファイルの内容。MCP 側はこれを読んで制御口に接続する。
type Discovery struct {
	Addr      string `json:"addr"`
	Token     string `json:"token"`
	PID       int    `json:"pid"`
	ShaderDir string `json:"shader_dir"`
	Version   int    `json:"version"`
}

// NewToken は 32 バイト乱数の hex（64 文字）を返す。
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return hex.EncodeToString(b), nil
}

// DiscoveryPath は発見ファイルの場所を返す。
// 環境変数 KAGELIFE_CONTROL_FILE が空でなければそれ、無ければ <UserCacheDir>/kagelife/control.json。
func DiscoveryPath() (string, error) {
	if p := os.Getenv(DiscoveryEnv); p != "" {
		return p, nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(cache, "kagelife", "control.json"), nil
}

// WriteDiscovery は d を 0600 の JSON として path に書く（親ディレクトリは 0700 で作る）。
func WriteDiscovery(path string, d Discovery) (err error) {
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	// CreateTemp は 0600 で作る。rename で置き換えるので既存ファイルの権限は引き継がない
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(b); err != nil {
		_ = tmp.Close()

		return err
	}
	if err = tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()

		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}

	return os.Rename(tmp.Name(), path)
}

// RemoveDiscovery は path の発見ファイルが token のものであれば削除する。
// 別のプロセスが上書きした発見ファイルは消さない。ファイルが無ければ何もしない。
func RemoveDiscovery(path, token string) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var d Discovery
	if json.Unmarshal(b, &d) != nil || d.Token != token {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	return nil
}

// ListenLoopback は addr（"IP:port"）で待ち受ける。IP はループバックのリテラルに限る。
func ListenLoopback(addr string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("control: invalid address %q: %w", addr, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, fmt.Errorf("control: address %q is not a loopback IP (use e.g. 127.0.0.1:0)", addr)
	}

	return net.Listen("tcp", addr)
}

// Config は Start の設定。
type Config struct {
	Addr          string // 例: "127.0.0.1:0"
	ShaderDir     string // 絶対パス
	DiscoveryFile string // 空なら DiscoveryPath()
	Compile       shadermgr.ShaderCompiler
	Queue         *Queue
	Timeout       time.Duration // 0 なら DefaultTimeout
}

// Controller は起動中の制御口。
type Controller struct {
	ln        net.Listener
	srv       *http.Server
	discovery string
	token     string
}

// Start は制御口を待ち受け、発見ファイルを書いてから HTTP の処理を始める。
func Start(cfg Config) (*Controller, error) {
	path := cfg.DiscoveryFile
	if path == "" {
		p, err := DiscoveryPath()
		if err != nil {
			return nil, fmt.Errorf("control: discovery file: %w", err)
		}
		path = p
	}
	token, err := NewToken()
	if err != nil {
		return nil, err
	}
	ln, err := ListenLoopback(cfg.Addr)
	if err != nil {
		return nil, err
	}

	c := &Controller{
		ln:        ln,
		discovery: path,
		token:     token,
		srv: &http.Server{
			Handler: NewServer(ServerConfig{
				Token:     token,
				ShaderDir: cfg.ShaderDir,
				Compile:   cfg.Compile,
				Queue:     cfg.Queue,
				Timeout:   cfg.Timeout,
			}),
			ReadHeaderTimeout: 5 * time.Second,
		},
	}

	err = WriteDiscovery(path, Discovery{
		Addr:      ln.Addr().String(),
		Token:     token,
		PID:       os.Getpid(),
		ShaderDir: cfg.ShaderDir,
		Version:   ProtocolVersion,
	})
	if err != nil {
		_ = ln.Close()

		return nil, fmt.Errorf("control: write discovery file: %w", err)
	}

	go func() { _ = c.srv.Serve(ln) }()

	return c, nil
}

// Addr は実際に待ち受けているアドレスを返す。
func (c *Controller) Addr() string { return c.ln.Addr().String() }

// DiscoveryFile は発見ファイルのパスを返す。
func (c *Controller) DiscoveryFile() string { return c.discovery }

// Close は待ち受けを止め、発見ファイルを削除する。
func (c *Controller) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	errShutdown := c.srv.Shutdown(ctx)
	if errors.Is(errShutdown, context.DeadlineExceeded) {
		errShutdown = c.srv.Close()
	}

	return errors.Join(errShutdown, RemoveDiscovery(c.discovery, c.token))
}
