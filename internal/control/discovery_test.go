package control

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

func TestNewToken(t *testing.T) {
	a, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewToken()

	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(a) {
		t.Errorf("token = %q, want hex 64 文字", a)
	}
	if a == b {
		t.Error("2 回の生成で同じトークン")
	}
}

func TestDiscoveryPath_Env(t *testing.T) {
	t.Setenv("KAGELIFE_CONTROL_FILE", "/tmp/custom/control.json")

	got, err := DiscoveryPath()

	if err != nil || got != "/tmp/custom/control.json" {
		t.Errorf("DiscoveryPath = %q, %v", got, err)
	}
}

func TestDiscoveryPath_Default(t *testing.T) {
	t.Setenv("KAGELIFE_CONTROL_FILE", "")
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Skip("UserCacheDir が無い環境")
	}

	got, err := DiscoveryPath()

	if want := filepath.Join(cache, "kagelife", "control.json"); err != nil || got != want {
		t.Errorf("DiscoveryPath = %q, %v; want %q", got, err, want)
	}
}

func TestWriteDiscovery_ContentAndMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "control.json")
	d := Discovery{Addr: "127.0.0.1:5555", Token: testToken, PID: 42, ShaderDir: "/abs/shaders", Version: 1}

	if err := WriteDiscovery(path, d); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %o, want 600", perm)
	}
	raw, _ := os.ReadFile(path)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	want := `{"addr":"127.0.0.1:5555","pid":42,"shader_dir":"/abs/shaders","token":"` + testToken + `","version":1}`
	if got := jsonBody(m); got != want {
		t.Errorf("内容 = %s\nwant   %s", got, want)
	}
}

func TestWriteDiscovery_OverwriteKeeps0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.json")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := WriteDiscovery(path, Discovery{Version: 1}); err != nil {
		t.Fatal(err)
	}

	info, _ := os.Stat(path)
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %o, want 600", perm)
	}
}

func TestRemoveDiscovery_OnlyOwnToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.json")
	if err := WriteDiscovery(path, Discovery{Token: "other", Version: 1}); err != nil {
		t.Fatal(err)
	}

	if err := RemoveDiscovery(path, "mine"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("他のプロセスの発見ファイルを消した")
	}

	if err := RemoveDiscovery(path, "other"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("自分の発見ファイルが消えていない")
	}

	if err := RemoveDiscovery(path, "other"); err != nil {
		t.Errorf("既に無いときにエラー: %v", err)
	}
}

func TestListenLoopback(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:0", "[::1]:0"} {
		ln, err := ListenLoopback(addr)
		if err != nil {
			if addr == "[::1]:0" {
				continue // IPv6 が無い環境
			}
			t.Fatalf("ListenLoopback(%q) = %v", addr, err)
		}
		_ = ln.Close()
	}

	for _, addr := range []string{"0.0.0.0:0", ":0", "192.168.1.10:0", "example.com:0", "localhost:0", "garbage"} {
		if ln, err := ListenLoopback(addr); err == nil {
			_ = ln.Close()
			t.Errorf("ListenLoopback(%q) が通った", addr)
		}
	}
}

func TestStartAndClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.json")
	dir := t.TempDir()
	q := NewQueue(8)
	stop := runLoop(q, newTestEngine(dir, "a"))
	defer stop()

	c, err := Start(Config{
		Addr:          "127.0.0.1:0",
		ShaderDir:     dir,
		DiscoveryFile: path,
		Compile:       (&fakeCompiler{}).compile,
		Queue:         q,
	})
	if err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var d Discovery
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	if d.Addr != c.Addr() || d.PID != os.Getpid() || d.ShaderDir != dir || d.Version != 1 || len(d.Token) != 64 {
		t.Errorf("discovery = %+v, addr = %s", d, c.Addr())
	}

	req, _ := http.NewRequest("GET", "http://"+d.Addr+"/v1/state", nil)
	req.Header.Set("Authorization", "Bearer "+d.Token)
	res, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != 200 {
		t.Errorf("status = %d", res.StatusCode)
	}

	if err := c.Close(); err != nil {
		t.Errorf("Close = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Close 後も発見ファイルが残っている")
	}
	if _, err := (&http.Client{Timeout: time.Second}).Do(req); err == nil {
		t.Error("Close 後も待ち受けている")
	}
}

func TestStart_RejectsNonLoopback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.json")

	_, err := Start(Config{Addr: "0.0.0.0:0", ShaderDir: t.TempDir(), DiscoveryFile: path, Queue: NewQueue(1)})

	if err == nil {
		t.Fatal("ループバック以外で起動できた")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("起動失敗なのに発見ファイルが書かれた")
	}
}
