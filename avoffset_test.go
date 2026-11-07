package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/cyokozai/kagelife/internal/tempo"
)

func TestParseFlags_AVOffset(t *testing.T) {
	t.Run("既定は 0", func(t *testing.T) {
		opts, err := parseFlags(nil)
		if err != nil {
			t.Fatal(err)
		}
		if opts.avOffset != 0 {
			t.Fatalf("avOffset = %v, want 0", opts.avOffset)
		}
	})

	for _, c := range []struct {
		arg  string
		want time.Duration
	}{
		{"0", 0},
		{"200", 200 * time.Millisecond},
		{"1000", time.Second},
	} {
		t.Run("ミリ秒で受け取る: "+c.arg, func(t *testing.T) {
			opts, err := parseFlags([]string{"-av-offset", c.arg})
			if err != nil {
				t.Fatal(err)
			}
			if opts.avOffset != c.want {
				t.Fatalf("avOffset = %v, want %v", opts.avOffset, c.want)
			}
		})
	}

	for _, bad := range []string{"-1", "1001", "abc", "1.5"} {
		t.Run("範囲外・整数でない値は起動エラーで使い方を出す: "+bad, func(t *testing.T) {
			var out bytes.Buffer
			_, err := parseFlagsTo([]string{"-av-offset", bad}, &out)
			if err == nil || !strings.Contains(err.Error(), "-av-offset") {
				t.Fatalf("err = %v, want a -av-offset error", err)
			}
			if !strings.Contains(out.String(), "Usage") || !strings.Contains(out.String(), "-av-offset") {
				t.Fatalf("使い方が出ていない: %q", out.String())
			}
		})
	}
}

func TestVisualPhase(t *testing.T) {
	t0 := time.Date(2026, 11, 21, 20, 0, 0, 0, time.UTC)
	newTapper := func() *tempo.Tapper {
		tp := tempo.New(2*time.Second, 8)
		tp.SetBPM(120, t0) // 1 拍 500 ms、拍頭は t0
		return tp
	}

	t.Run("offset 0 は拍頭で 0", func(t *testing.T) {
		if got := visualPhase(newTapper(), t0, 0); got != 0 {
			t.Fatalf("phase = %v, want 0", got)
		}
	})

	t.Run("offset 200ms は 200ms 遅れて 0 に戻る", func(t *testing.T) {
		tp := newTapper()
		offset := 200 * time.Millisecond
		if got := visualPhase(tp, t0.Add(offset), offset); got != 0 {
			t.Fatalf("拍頭から 200ms 後の phase = %v, want 0", got)
		}
		// 拍頭そのものでは、まだ前の拍の 60%（-200ms / 500ms → 0.6）
		if got := visualPhase(tp, t0, offset); !near(got, 0.6) {
			t.Fatalf("拍頭の phase = %v, want 0.6", got)
		}
		// offset 0 の 100ms 後（0.2）と、offset 200ms の 300ms 後が同じ
		if a, b := visualPhase(newTapper(), t0.Add(100*time.Millisecond), 0), visualPhase(tp, t0.Add(300*time.Millisecond), offset); !near(a, b) {
			t.Fatalf("offset 0 の +100ms = %v, offset 200ms の +300ms = %v（同じはず）", a, b)
		}
	})

	t.Run("入力の時刻（BPM）はずらさない", func(t *testing.T) {
		tp := newTapper()
		visualPhase(tp, t0.Add(time.Second), 200*time.Millisecond)
		if tp.BPM() != 120 {
			t.Fatalf("BPM = %v, want 120", tp.BPM())
		}
	})
}

func TestHUDText_AVOffset(t *testing.T) {
	t.Run("offset が 0 なら出さない", func(t *testing.T) {
		got := hudText(hudState{BPM: 120, Measured: true, FadeBeats: 4})
		if strings.Contains(got, "AV offset") {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("offset があれば状態行に出す", func(t *testing.T) {
		got := hudText(hudState{BPM: 120, Measured: true, FadeBeats: 4, AVOffset: 200 * time.Millisecond})
		want := "BPM: 120.0  FadeBeats: 4.0  Mix: 0.00  AV offset: 200ms"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}

func near(a, b float32) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-4
}
