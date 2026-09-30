package tempo

import (
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

func TestSetBPMMakesNowTheBeatOrigin(t *testing.T) {
	tp := New(2*time.Second, 8)
	tp.SetBPM(120, base) // 1 拍 = 500ms

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
}

func TestResetClearsSetBPM(t *testing.T) {
	tp := New(2*time.Second, 8)
	tp.SetBPM(128, base)

	tp.Reset()

	if got := tp.BPM(); got != 0 {
		t.Errorf("Reset後: BPM = %v, want 0", got)
	}
}

func TestSetBPMIgnoresNonPositive(t *testing.T) {
	tp := New(2*time.Second, 8)
	tp.SetBPM(100, base)

	tp.SetBPM(0, base)
	tp.SetBPM(-5, base)

	if got := tp.BPM(); got != 100 {
		t.Errorf("BPM = %v, want 100（0 以下は無視）", got)
	}
}
