package carabiner

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const statusLine = "status { :peers 1 :bpm 120.000000 :start 73743731220 :beat 597.737570 }\n"

// fakeCarabiner は 127.0.0.1 で待ち受ける偽の Carabiner。
// 接続を受けるたびに status を 1 行送り、届いた行を lines に流す。
type fakeCarabiner struct {
	ln       net.Listener
	lines    chan string
	accepted chan net.Conn
}

func newFakeCarabiner(t *testing.T) *fakeCarabiner {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	f := &fakeCarabiner{ln: ln, lines: make(chan string, 1024), accepted: make(chan net.Conn, 16)}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = conn.Close() })
			f.accepted <- conn
			_, _ = conn.Write([]byte(statusLine))
			go func() {
				sc := bufio.NewScanner(conn)
				for sc.Scan() {
					line := sc.Text()
					f.lines <- line
					if line == "status" {
						_, _ = conn.Write([]byte(statusLine))
					}
				}
			}()
		}
	}()
	return f
}

func (f *fakeCarabiner) addr() string { return f.ln.Addr().String() }

func waitLine(t *testing.T, f *fakeCarabiner, want string) {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case got := <-f.lines:
			if got == want {
				return
			}
		case <-timeout:
			t.Fatalf("%q が届かない", want)
		}
	}
}

func waitConn(t *testing.T, f *fakeCarabiner) net.Conn {
	t.Helper()
	select {
	case c := <-f.accepted:
		return c
	case <-time.After(2 * time.Second):
		t.Fatal("接続が来ない")
		return nil
	}
}

func TestClientReceivesStatusRightAfterConnect(t *testing.T) {
	f := newFakeCarabiner(t)
	fixed := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	got := make(chan Status, 16)
	at := make(chan time.Time, 16)
	connected := make(chan struct{}, 4)

	c := Start(Config{
		Addr:      f.addr(),
		Poll:      time.Hour, // 定期送信は起こさない（接続直後の自発的な status だけを見る）
		Now:       func() time.Time { return fixed },
		OnConnect: func() { connected <- struct{}{} },
		OnStatus: func(st Status, ts time.Time) {
			got <- st
			at <- ts
		},
	})
	defer func() { _ = c.Close() }()

	select {
	case <-connected:
	case <-time.After(2 * time.Second):
		t.Fatal("OnConnect が呼ばれない")
	}
	select {
	case st := <-got:
		if st.Peers != 1 || st.BPM != 120 || st.Beat != 597.737570 {
			t.Errorf("status = %+v", st)
		}
		if ts := <-at; !ts.Equal(fixed) {
			t.Errorf("受信時刻 = %v, want 注入した時計の値 %v", ts, fixed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("status を受け取らない")
	}
}

func TestClientPollsStatusPeriodically(t *testing.T) {
	f := newFakeCarabiner(t)
	c := Start(Config{Addr: f.addr(), Poll: 10 * time.Millisecond})
	defer func() { _ = c.Close() }()

	for range 3 {
		waitLine(t, f, "status")
	}
}

func TestClientEnablesStartStopSync(t *testing.T) {
	f := newFakeCarabiner(t)
	c := Start(Config{Addr: f.addr(), Poll: time.Hour, StartStopSync: true})
	defer func() { _ = c.Close() }()

	waitLine(t, f, "enable-start-stop-sync")
}

func TestClientDoesNotEnableStartStopSyncWhenOff(t *testing.T) {
	f := newFakeCarabiner(t)
	c := Start(Config{Addr: f.addr(), Poll: 10 * time.Millisecond})
	defer func() { _ = c.Close() }()

	for range 3 {
		select {
		case line := <-f.lines:
			if line == "enable-start-stop-sync" {
				t.Fatal("StartStopSync=false なのに enable-start-stop-sync が届いた")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("status が届かない")
		}
	}
}

func TestClientReconnectsAfterDisconnect(t *testing.T) {
	f := newFakeCarabiner(t)
	disconnected := make(chan error, 4)
	var connects atomic.Int32
	c := Start(Config{
		Addr:         f.addr(),
		Poll:         10 * time.Millisecond,
		MinBackoff:   5 * time.Millisecond,
		MaxBackoff:   20 * time.Millisecond,
		OnConnect:    func() { connects.Add(1) },
		OnDisconnect: func(err error) { disconnected <- err },
	})
	defer func() { _ = c.Close() }()

	first := waitConn(t, f)
	_ = first.Close()

	select {
	case <-disconnected:
	case <-time.After(2 * time.Second):
		t.Fatal("OnDisconnect が呼ばれない")
	}
	waitConn(t, f)
	// 前の接続の行が lines に残っているので、行ではなく OnConnect の回数で再接続を確かめる。
	deadline := time.Now().Add(2 * time.Second)
	for connects.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("OnConnect の回数 = %d, want 2 以上", connects.Load())
		}
		time.Sleep(time.Millisecond)
	}
}

func TestClientRetriesDialWithBackoff(t *testing.T) {
	f := newFakeCarabiner(t)
	var dials atomic.Int32
	errs := make(chan error, 16)
	c := Start(Config{
		Addr:       f.addr(),
		Poll:       time.Hour,
		MinBackoff: time.Millisecond,
		MaxBackoff: 4 * time.Millisecond,
		Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if dials.Add(1) <= 3 {
				return nil, errors.New("connection refused")
			}
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
		OnDisconnect: func(err error) { errs <- err },
	})
	defer func() { _ = c.Close() }()

	waitConn(t, f)
	if n := dials.Load(); n < 4 {
		t.Errorf("Dial の回数 = %d, want 4 以上", n)
	}
	for range 3 {
		select {
		case err := <-errs:
			if err == nil || !strings.Contains(err.Error(), "connection refused") {
				t.Errorf("OnDisconnect(err) = %v, want 接続失敗のエラー", err)
			}
		case <-time.After(time.Second):
			t.Fatal("接続失敗が OnDisconnect に届かない")
		}
	}
}

func TestClientReportsResponseErrors(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = conn.Write([]byte("unsupported enable-start-stop-sync\n" + statusLine))
		time.Sleep(time.Second)
	}()

	msgErrs := make(chan error, 4)
	got := make(chan Status, 4)
	c := Start(Config{
		Addr:           ln.Addr().String(),
		Poll:           time.Hour,
		OnMessageError: func(err error) { msgErrs <- err },
		OnStatus:       func(st Status, _ time.Time) { got <- st },
	})
	defer func() { _ = c.Close() }()

	select {
	case err := <-msgErrs:
		var re *ResponseError
		if !errors.As(err, &re) || re.Kind != "unsupported" {
			t.Errorf("OnMessageError(%v), want *ResponseError unsupported", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("エラー応答が報告されない")
	}
	select {
	case <-got: // エラー応答の後の status も読める
	case <-time.After(2 * time.Second):
		t.Fatal("エラー応答の後の status を受け取らない")
	}
}

func TestClientCloseStopsAndIsIdempotent(t *testing.T) {
	f := newFakeCarabiner(t)
	c := Start(Config{Addr: f.addr(), Poll: 10 * time.Millisecond})
	waitConn(t, f)

	done := make(chan struct{})
	go func() {
		_ = c.Close()
		_ = c.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close が戻らない")
	}
}

func TestNextBackoffDoublesUpToMax(t *testing.T) {
	lo, hi := 100*time.Millisecond, 2*time.Second
	b := lo
	var seq []time.Duration
	for range 7 {
		seq = append(seq, b)
		b = nextBackoff(b, hi)
	}
	want := []time.Duration{100, 200, 400, 800, 1600, 2000, 2000}
	for i := range want {
		if seq[i] != want[i]*time.Millisecond {
			t.Fatalf("backoff 列 = %v, want %v ms", seq, want)
		}
	}
}
