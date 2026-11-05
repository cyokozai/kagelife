package carabiner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// 接続まわりの既定値。
const (
	DefaultAddr       = "127.0.0.1:17000"
	DefaultPoll       = 100 * time.Millisecond
	DefaultMinBackoff = 100 * time.Millisecond
	DefaultMaxBackoff = 2 * time.Second

	writeTimeout = time.Second
)

// Config は Client の設定。コールバックは Client の内部ゴルーチンから呼ばれる。
type Config struct {
	Addr          string        // 接続先。空なら DefaultAddr
	Poll          time.Duration // status を投げる間隔。0 以下なら DefaultPoll
	StartStopSync bool          // 接続のたびに enable-start-stop-sync を送る

	// Dial は接続に使う関数。nil なら net.Dialer.DialContext。テストで差し替える。
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
	// Now は status を受け取った時刻を測る時計。nil なら time.Now（単調時計の読みを含む）。
	Now func() time.Time

	MinBackoff time.Duration // 再接続の最初の間隔。0 以下なら DefaultMinBackoff
	MaxBackoff time.Duration // 再接続の間隔の上限。0 以下なら DefaultMaxBackoff

	OnConnect      func()                        // 接続できたとき
	OnDisconnect   func(err error)               // 接続に失敗したとき・切れたとき（err は原因）
	OnStatus       func(st Status, at time.Time) // status を受け取ったとき（at は受信時刻）
	OnMessageError func(err error)               // エラー応答・壊れた応答を受け取ったとき
}

// Client は Carabiner に接続し、status を定期的に問い合わせる。切れたら再接続する。
type Client struct {
	cfg    Config
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

// Start は設定を補ってから接続のループを背景で始める。止めるには Close を呼ぶ。
func Start(cfg Config) *Client {
	if cfg.Addr == "" {
		cfg.Addr = DefaultAddr
	}
	if cfg.Poll <= 0 {
		cfg.Poll = DefaultPoll
	}
	if cfg.Dial == nil {
		var d net.Dialer
		cfg.Dial = d.DialContext
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.MinBackoff <= 0 {
		cfg.MinBackoff = DefaultMinBackoff
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = DefaultMaxBackoff
	}
	if cfg.MaxBackoff < cfg.MinBackoff {
		cfg.MaxBackoff = cfg.MinBackoff
	}

	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{cfg: cfg, cancel: cancel, done: make(chan struct{})}
	go c.run(ctx)
	return c
}

// Close は接続を閉じ、背景のゴルーチンが終わるまで待つ。何度呼んでもよい。
func (c *Client) Close() error {
	c.once.Do(c.cancel)
	<-c.done
	return nil
}

// nextBackoff は再接続の間隔を倍にし、hi で頭打ちにする。
func nextBackoff(cur, hi time.Duration) time.Duration {
	next := cur * 2
	if next > hi || next <= 0 {
		return hi
	}
	return next
}

func (c *Client) run(ctx context.Context) {
	defer close(c.done)
	backoff := c.cfg.MinBackoff
	for {
		conn, err := c.cfg.Dial(ctx, "tcp", c.cfg.Addr)
		if ctx.Err() != nil {
			if conn != nil {
				_ = conn.Close()
			}
			return
		}
		if err != nil {
			c.disconnected(err)
		} else {
			c.call(c.cfg.OnConnect)
			gotStatus, err := c.session(ctx, conn)
			if ctx.Err() != nil {
				return
			}
			c.disconnected(err)
			if gotStatus {
				backoff = c.cfg.MinBackoff
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = nextBackoff(backoff, c.cfg.MaxBackoff)
	}
}

func (c *Client) call(f func()) {
	if f != nil {
		f()
	}
}

func (c *Client) disconnected(err error) {
	if c.cfg.OnDisconnect != nil {
		c.cfg.OnDisconnect(err)
	}
}

// session は 1 本の接続を扱う。status を 1 度でも受け取れば gotStatus は true。
func (c *Client) session(ctx context.Context, conn net.Conn) (gotStatus bool, err error) {
	defer func() { _ = conn.Close() }()

	readErr := make(chan error, 1)
	var got sync.Once
	gotCh := make(chan struct{})
	go func() {
		readErr <- c.readLoop(conn, func() { got.Do(func() { close(gotCh) }) })
	}()
	received := func() bool {
		select {
		case <-gotCh:
			return true
		default:
			return false
		}
	}

	if c.cfg.StartStopSync {
		if err := c.send(conn, "enable-start-stop-sync"); err != nil {
			return received(), err
		}
	}
	if err := c.send(conn, "status"); err != nil {
		return received(), err
	}

	ticker := time.NewTicker(c.cfg.Poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = conn.Close()
			<-readErr
			return received(), ctx.Err()
		case err := <-readErr:
			if err == nil {
				err = io.EOF
			}
			return received(), fmt.Errorf("carabiner: connection lost: %w", err)
		case <-ticker.C:
			if err := c.send(conn, "status"); err != nil {
				_ = conn.Close()
				<-readErr
				return received(), err
			}
		}
	}
}

func (c *Client) send(conn net.Conn, cmd string) error {
	_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	if _, err := io.WriteString(conn, cmd+"\n"); err != nil {
		return fmt.Errorf("carabiner: send %s: %w", cmd, err)
	}
	return nil
}

func (c *Client) readLoop(conn net.Conn, onStatus func()) error {
	lr := NewLineReader(conn)
	for {
		line, err := lr.ReadLine()
		if err != nil {
			return err
		}
		at := c.cfg.Now()
		st, err := ParseStatus(line)
		var unknown *UnknownMessageError
		switch {
		case err == nil:
			onStatus()
			if c.cfg.OnStatus != nil {
				c.cfg.OnStatus(st, at)
			}
		case errors.As(err, &unknown):
			// version など、PoC で扱わない応答は読み飛ばす。
		default:
			if c.cfg.OnMessageError != nil {
				c.cfg.OnMessageError(err)
			}
		}
	}
}
