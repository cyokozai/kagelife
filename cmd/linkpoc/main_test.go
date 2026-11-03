package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer は run の出力をテストから安全に読むための入れ物。
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestRunShowsSyncedLinesFromCarabiner(t *testing.T) {
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
		beat := 10.0
		sc := bufio.NewScanner(conn)
		for sc.Scan() {
			if sc.Text() == "status" {
				beat += 0.1
				_, _ = fmt.Fprintf(conn, "status { :peers 1 :bpm 128.000000 :start 0 :beat %.6f :playing true }\n", beat)
			}
		}
	}()

	var out syncBuffer
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	run(ctx, &out, ln.Addr().String(), 4, 20*time.Millisecond, true)

	got := out.String()
	for _, want := range []string{"に接続しました", "synced", "bpm=128.00", "play=on", "# 終了します"} {
		if !strings.Contains(got, want) {
			t.Errorf("出力に %q が無い:\n%s", want, got)
		}
	}
}

func TestRunWithoutCarabinerGuidesOnceAndKeepsRunning(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // 誰も待ち受けていないアドレス

	var out syncBuffer
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	run(ctx, &out, addr, 4, 20*time.Millisecond, true)

	got := out.String()
	if n := strings.Count(got, "Carabiner を起動してください"); n != 1 {
		t.Errorf("起動の案内の回数 = %d, want 1:\n%s", n, got)
	}
	if !strings.Contains(got, "offline") {
		t.Errorf("offline の行が無い:\n%s", got)
	}
	if !strings.Contains(got, "# 終了します") {
		t.Errorf("正常に終わっていない:\n%s", got)
	}
}
