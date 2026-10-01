package tempo

import (
	"math"
	"testing"
	"time"
)

func TestSetBPMFixesTempoWithoutTaps(t *testing.T) {
	tp := New(2*time.Second, 8)

	tp.SetBPM(128, base)

	if got := tp.BPM(); got != 128 {
		t.Errorf("SetBPM(128) 後: BPM = %v, want 128", got)
	}
}

func TestSetBPMMarksMeasured(t *testing.T) {
	tp := New(2*time.Second, 8)
	if tp.Measured() {
		t.Fatal("初期状態で Measured() = true")
	}

	tp.SetBPM(DefaultBPM, base) // 既定値と同じ値でも、設定したなら測定値扱い

	if !tp.Measured() {
		t.Error("SetBPM 後: Measured() = false, want true")
	}
}

func TestSetBPMMakesNowTheBeatOrigin(t *testing.T) {
	tp := New(2*time.Second, 8)
	tp.Phase(base.Add(-123 * time.Millisecond)) // 既定 BPM で起点が決まっていても
	tp.SetBPM(120, base)                        // 1 拍 = 500ms

	if got := tp.Phase(base); got > 0.001 {
		t.Errorf("設定直後の Phase = %v, want 0", got)
	}
	if got := tp.Phase(base.Add(250 * time.Millisecond)); got < 0.499 || got > 0.501 {
		t.Errorf("半拍後の Phase = %v, want 0.5", got)
	}
}

func TestSetBPMDiscardsPreviousTaps(t *testing.T) {
	tp := New(2*time.Second, 8)
	tp.Tap(base)
	tp.Tap(base.Add(500 * time.Millisecond)) // 120 BPM

	tp.SetBPM(90, base.Add(time.Second))
	tp.Tap(base.Add(1200 * time.Millisecond)) // 古いタップと組んで測り直さない

	if got := tp.BPM(); got != 90 {
		t.Errorf("BPM = %v, want 90", got)
	}
}

func TestSingleTapAfterSetBPMResyncsPhaseOnly(t *testing.T) {
	tp := New(2*time.Second, 8)
	tp.SetBPM(120, base)

	tp.Tap(base.Add(1100 * time.Millisecond))

	if got := tp.BPM(); got != 120 {
		t.Errorf("1 回タップ後の BPM = %v, want 120（設定値を保つ）", got)
	}
	if got := tp.Phase(base.Add(1100 * time.Millisecond)); got > 0.001 {
		t.Errorf("タップ直後の Phase = %v, want 0", got)
	}
}

func TestTapsOverrideSetBPM(t *testing.T) {
	tp := New(2*time.Second, 8)
	tp.SetBPM(90, base)

	// 1000ms 間隔 = 60 BPM
	tp.Tap(base.Add(3 * time.Second))
	tp.Tap(base.Add(4 * time.Second))

	if got := tp.BPM(); got < 59.9 || got > 60.1 {
		t.Errorf("タップ後の BPM = %v, want 60", got)
	}
	if !tp.Measured() {
		t.Error("Measured() = false, want true")
	}
}

func TestResetClearsSetBPM(t *testing.T) {
	tp := New(2*time.Second, 8)
	tp.SetBPM(128, base)

	tp.Reset()

	if got := tp.BPM(); got != DefaultBPM {
		t.Errorf("Reset 後: BPM = %v, want %v", got, DefaultBPM)
	}
	if tp.Measured() {
		t.Error("Reset 後: Measured() = true, want false")
	}
}

// 範囲（MinBPM..MaxBPM）の境界はそのまま受け、外はタップの測定値と同じく丸める。
// 制御口は範囲外を 400 で弾くので、丸めは他の呼び出し元のための保険。
func TestSetBPMClampsToRange(t *testing.T) {
	cases := []struct {
		in, want float64
	}{
		{39, MinBPM},
		{40, 40},
		{300, 300},
		{301, MaxBPM},
		{0, MinBPM},
		{-5, MinBPM},
		{math.Inf(1), MaxBPM},
		{math.Inf(-1), MinBPM},
	}
	for _, c := range cases {
		tp := New(2*time.Second, 8)

		tp.SetBPM(c.in, base)

		if got := tp.BPM(); got != c.want {
			t.Errorf("SetBPM(%v): BPM = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestSetBPMIgnoresNaN(t *testing.T) {
	tp := New(2*time.Second, 8)
	tp.SetBPM(100, base)

	tp.SetBPM(math.NaN(), base.Add(time.Second))

	if got := tp.BPM(); got != 100 {
		t.Errorf("BPM = %v, want 100（NaN は無視）", got)
	}
	// 100 BPM は 1 拍 600ms。拍頭が base のままなら 300ms 後は半拍
	if got := tp.Phase(base.Add(300 * time.Millisecond)); got < 0.499 || got > 0.501 {
		t.Errorf("Phase = %v, want 0.5（NaN では拍頭も動かさない）", got)
	}
}
