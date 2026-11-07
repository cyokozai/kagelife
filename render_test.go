package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cyokozai/kagelife/internal/uniform"
)

func TestParseFlags_RenderScale(t *testing.T) {
	t.Run("既定は 1.0", func(t *testing.T) {
		opts, err := parseFlags(nil)
		if err != nil {
			t.Fatal(err)
		}
		if opts.renderScale != 1.0 {
			t.Fatalf("renderScale = %v, want 1.0", opts.renderScale)
		}
	})

	for _, c := range []struct {
		arg  string
		want float64
	}{{"0.25", 0.25}, {"0.5", 0.5}, {"1", 1}} {
		t.Run("受け取る: "+c.arg, func(t *testing.T) {
			opts, err := parseFlags([]string{"-render-scale", c.arg})
			if err != nil {
				t.Fatal(err)
			}
			if opts.renderScale != c.want {
				t.Fatalf("renderScale = %v, want %v", opts.renderScale, c.want)
			}
		})
	}

	for _, bad := range []string{"0.2", "1.5", "0", "abc", "NaN"} {
		t.Run("範囲外は起動エラー: "+bad, func(t *testing.T) {
			var out bytes.Buffer
			if _, err := parseFlagsTo([]string{"-render-scale", bad}, &out); err == nil || !strings.Contains(err.Error(), "-render-scale") {
				t.Fatalf("err = %v, want a -render-scale error", err)
			}
		})
	}
}

func TestScaledSize(t *testing.T) {
	cases := []struct {
		w, h   int
		s      float64
		ww, wh int
	}{
		{1920, 1080, 1, 1920, 1080},
		{1920, 1080, 0.5, 960, 540},
		{2940, 1912, 0.25, 735, 478},
		{1, 1, 0.25, 1, 1}, // 0 にはしない
	}
	for _, c := range cases {
		if w, h := scaledSize(c.w, c.h, c.s); w != c.ww || h != c.wh {
			t.Errorf("scaledSize(%d, %d, %v) = %d, %d, want %d, %d", c.w, c.h, c.s, w, h, c.ww, c.wh)
		}
	}
}

func TestShaderInput_RenderScale(t *testing.T) {
	p := frameParams{
		ScreenW: 1920, ScreenH: 1080, Scale: 0.5,
		CursorX: 800, CursorY: 600,
		Elapsed: 1500 * time.Millisecond, Beat: 0.25, Frame: 7, Random: 0.5,
	}
	got := uniform.New().Build(shaderInput(p))

	if res := got["Resolution"].([]float32); !reflect.DeepEqual(res, []float32{960, 540}) {
		t.Errorf("Resolution = %v, want 画面の半分 [960 540]", res)
	}
	if cur := got["Cursor"].([]float32); !reflect.DeepEqual(cur, []float32{400, 300}) {
		t.Errorf("Cursor = %v, want 同じ倍率で縮めた [400 300]", cur)
	}
	if got["Time"] != float32(1.5) || got["Beat"] != float32(0.25) || got["Frame"] != 7 || got["Random"] != float32(0.5) {
		t.Errorf("他の uniform が変わった: %v", got)
	}

	p.Scale = 1
	full := uniform.New().Build(shaderInput(p))
	if res := full["Resolution"].([]float32); !reflect.DeepEqual(res, []float32{1920, 1080}) {
		t.Errorf("scale 1 の Resolution = %v, want [1920 1080]", res)
	}
}

func TestFrameStats_MaxWithinWindow(t *testing.T) {
	t0 := time.Date(2026, 11, 21, 20, 0, 0, 0, time.UTC)
	ms := func(n int) time.Time { return t0.Add(time.Duration(n) * time.Millisecond) }
	fs := newFrameStats(5 * time.Second)

	if got := fs.max(t0); got != 0 {
		t.Fatalf("フレームが無いときの max = %v, want 0", got)
	}

	fs.observe(ms(0))
	if got := fs.max(ms(0)); got != 0 {
		t.Fatalf("1 フレームだけでは間隔が無い: max = %v", got)
	}
	fs.observe(ms(16))
	fs.observe(ms(32))
	fs.observe(ms(82)) // 50ms かかったフレーム
	fs.observe(ms(98))

	if got := fs.max(ms(98)); got != 50*time.Millisecond {
		t.Fatalf("max = %v, want 50ms", got)
	}

	// 50ms のフレームが終わった時刻（82ms）から 5 秒を過ぎると窓から外れる
	for n := 114; n <= 5074; n += 16 {
		fs.observe(ms(n))
	}
	if got := fs.max(ms(5074)); got != 50*time.Millisecond {
		t.Fatalf("5 秒以内なら残る: max = %v, want 50ms", got)
	}
	fs.observe(ms(5090))
	fs.observe(ms(5106))
	if got := fs.max(ms(5106)); got != 16*time.Millisecond {
		t.Fatalf("5 秒を過ぎたら外れる: max = %v, want 16ms", got)
	}
}

func TestHUDText_Perf(t *testing.T) {
	t.Run("Perf が無ければ出さない", func(t *testing.T) {
		got := hudText(hudState{BPM: 120, Measured: true, FadeBeats: 4})
		if strings.Contains(got, "FPS") {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("状態行の次に fps とフレーム時間の最大値を出す", func(t *testing.T) {
		got := hudText(hudState{BPM: 120, Measured: true, FadeBeats: 4,
			Perf: &perfState{FPS: 59.94, FrameMax: 18250 * time.Microsecond, RenderScale: 1}})
		want := "BPM: 120.0  FadeBeats: 4.0  Mix: 0.00\nFPS: 59.9  FrameMax(5s): 18.2ms"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("render-scale が 1 未満なら倍率も出す", func(t *testing.T) {
		got := hudText(hudState{BPM: 120, Measured: true, FadeBeats: 4,
			Perf: &perfState{FPS: 60, FrameMax: 17 * time.Millisecond, RenderScale: 0.5}})
		if !strings.HasSuffix(got, "FPS: 60.0  FrameMax(5s): 17.0ms  RenderScale: 0.50") {
			t.Fatalf("got %q", got)
		}
	})
}
