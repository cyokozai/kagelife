package tempo

import (
	"testing"
	"time"
)

var base = time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)

func TestBPMIsZeroUntilTwoTaps(t *testing.T) {
	tp := New(2*time.Second, 8)

	if got := tp.BPM(); got != 0 {
		t.Errorf("タップ0回: BPM = %v, want 0", got)
	}

	tp.Tap(base)
	if got := tp.BPM(); got != 0 {
		t.Errorf("タップ1回: BPM = %v, want 0", got)
	}
}

func TestBPMFromEvenTaps(t *testing.T) {
	tp := New(2*time.Second, 8)

	// 500ms 間隔 = 120 BPM
	for i := 0; i < 4; i++ {
		tp.Tap(base.Add(time.Duration(i) * 500 * time.Millisecond))
	}

	if got := tp.BPM(); got < 119.9 || got > 120.1 {
		t.Errorf("BPM = %v, want 120", got)
	}
}

func TestTapWindowSlides(t *testing.T) {
	tp := New(10*time.Second, 3)

	// 1000ms 間隔で 3 回 → 60 BPM
	for i := 0; i < 3; i++ {
		tp.Tap(base.Add(time.Duration(i) * time.Second))
	}
	if got := tp.BPM(); got < 59.9 || got > 60.1 {
		t.Fatalf("窓が埋まった時点: BPM = %v, want 60", got)
	}

	// 以降 500ms 間隔で 2 回叩くと、古い 1000ms 間隔が窓から落ちる
	tp.Tap(base.Add(2500 * time.Millisecond))
	tp.Tap(base.Add(3000 * time.Millisecond))

	if got := tp.BPM(); got < 119.9 || got > 120.1 {
		t.Errorf("窓がスライドした後: BPM = %v, want 120", got)
	}
}

func TestTapTimeoutResetsHistory(t *testing.T) {
	tp := New(2*time.Second, 8)

	tp.Tap(base)
	tp.Tap(base.Add(500 * time.Millisecond))
	if tp.BPM() == 0 {
		t.Fatal("前提: 2回タップで BPM が立っているはず")
	}

	// タイムアウトを超えた間隔のタップは新しい測定の開始とみなす
	tp.Tap(base.Add(10 * time.Second))

	if got := tp.BPM(); got != 0 {
		t.Errorf("タイムアウト後: BPM = %v, want 0 (履歴リセット)", got)
	}
}

func TestPhaseAdvancesWithinBeat(t *testing.T) {
	tp := New(2*time.Second, 8)
	tp.Tap(base)
	tp.Tap(base.Add(500 * time.Millisecond)) // 120 BPM, 最後のタップが拍頭

	tests := []struct {
		name   string
		at     time.Time
		want   float32
		margin float32
	}{
		{"拍頭", base.Add(500 * time.Millisecond), 0.0, 0.001},
		{"1/4拍", base.Add(625 * time.Millisecond), 0.25, 0.001},
		{"半拍", base.Add(750 * time.Millisecond), 0.5, 0.001},
		{"次の拍頭で折り返す", base.Add(1000 * time.Millisecond), 0.0, 0.001},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tp.Phase(tt.at)
			if got < tt.want-tt.margin || got > tt.want+tt.margin {
				t.Errorf("Phase = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPhaseIsZeroWithoutBPM(t *testing.T) {
	tp := New(2*time.Second, 8)

	if got := tp.Phase(base.Add(time.Second)); got != 0 {
		t.Errorf("BPM未確定: Phase = %v, want 0", got)
	}
}

func TestTapResyncsBeatOrigin(t *testing.T) {
	tp := New(2*time.Second, 8)
	tp.Tap(base)
	tp.Tap(base.Add(500 * time.Millisecond))

	// ずれた位置で叩き直したら、そのタップが新しい拍頭になる
	tp.Tap(base.Add(1200 * time.Millisecond))

	if got := tp.Phase(base.Add(1200 * time.Millisecond)); got > 0.001 {
		t.Errorf("タップ直後の Phase = %v, want 0", got)
	}
}

func TestResetClearsEverything(t *testing.T) {
	tp := New(2*time.Second, 8)
	tp.Tap(base)
	tp.Tap(base.Add(500 * time.Millisecond))

	tp.Reset()

	if got := tp.BPM(); got != 0 {
		t.Errorf("Reset後: BPM = %v, want 0", got)
	}
	if got := tp.Phase(base.Add(time.Second)); got != 0 {
		t.Errorf("Reset後: Phase = %v, want 0", got)
	}
}
