package tempo

import (
	"testing"
	"time"
)

var base = time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)

func TestBPMIsDefaultUntilTwoTaps(t *testing.T) {
	tp := New(2*time.Second, 8)

	if got := tp.BPM(); got != DefaultBPM {
		t.Errorf("タップ0回: BPM = %v, want %v", got, DefaultBPM)
	}
	if tp.Measured() {
		t.Error("タップ0回: Measured = true, want false")
	}

	tp.Tap(base)
	if got := tp.BPM(); got != DefaultBPM {
		t.Errorf("タップ1回: BPM = %v, want %v", got, DefaultBPM)
	}
	if tp.Measured() {
		t.Error("タップ1回: Measured = true, want false")
	}

	tp.Tap(base.Add(time.Second))
	if !tp.Measured() {
		t.Error("タップ2回: Measured = false, want true")
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

func TestTapAfterTimeoutKeepsPreviousBPM(t *testing.T) {
	tp := New(2*time.Second, 8)

	// 1000ms 間隔 = 60 BPM
	tp.Tap(base)
	tp.Tap(base.Add(time.Second))

	// タイムアウトを超えた間隔のタップは新しい測定の 1 回目。BPM は直前の値を保つ
	tp.Tap(base.Add(10 * time.Second))

	if got := tp.BPM(); got < 59.9 || got > 60.1 {
		t.Errorf("新しい測定の1回目: BPM = %v, want 60 (直前の値を保持)", got)
	}
	if !tp.Measured() {
		t.Error("新しい測定の1回目: Measured = false, want true (直前の測定値を保持)")
	}
	// 拍頭は最後のタップに合わせ直す
	if got := tp.Phase(base.Add(10 * time.Second)); got > 0.001 {
		t.Errorf("新しい測定の1回目の直後: Phase = %v, want 0", got)
	}
	if got := tp.Phase(base.Add(10500 * time.Millisecond)); got < 0.499 || got > 0.501 {
		t.Errorf("新しい測定の1回目から半拍後: Phase = %v, want 0.5", got)
	}

	// 2 回目がそろった時点で新しい測定値に置き換わる（古い履歴は混ざらない）
	tp.Tap(base.Add(10500 * time.Millisecond))
	if got := tp.BPM(); got < 119.9 || got > 120.1 {
		t.Errorf("新しい測定の2回目: BPM = %v, want 120", got)
	}
}

func TestTapAfterTimeoutWithoutMeasurementKeepsDefault(t *testing.T) {
	tp := New(2*time.Second, 8)

	tp.Tap(base)
	tp.Tap(base.Add(10 * time.Second))

	if got := tp.BPM(); got != DefaultBPM {
		t.Errorf("測定前のタイムアウト: BPM = %v, want %v", got, DefaultBPM)
	}
	if tp.Measured() {
		t.Error("測定前のタイムアウト: Measured = true, want false")
	}
}

func TestBPMIsClamped(t *testing.T) {
	tests := []struct {
		name     string
		interval time.Duration
		want     float64
	}{
		{"二度押しは上限に丸める", 10 * time.Millisecond, MaxBPM},
		{"同時刻の二度押しも上限に丸める", 0, MaxBPM},
		{"遅すぎる間隔は下限に丸める", 1900 * time.Millisecond, MinBPM},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tp := New(2*time.Second, 8)
			tp.Tap(base)
			tp.Tap(base.Add(tt.interval))

			if got := tp.BPM(); got != tt.want {
				t.Errorf("BPM = %v, want %v", got, tt.want)
			}
		})
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

func TestPhaseRunsAtDefaultBPMWithoutTaps(t *testing.T) {
	tp := New(2*time.Second, 8)

	// 最初に Phase を呼んだ時刻が拍頭になり、以降は既定の 120 BPM (500ms/拍) で進む
	tests := []struct {
		name string
		at   time.Time
		want float32
	}{
		{"最初の呼び出しが拍頭", base, 0.0},
		{"1/4拍", base.Add(125 * time.Millisecond), 0.25},
		{"半拍", base.Add(250 * time.Millisecond), 0.5},
		{"次の拍頭で折り返す", base.Add(500 * time.Millisecond), 0.0},
		{"数拍後も連続して進む", base.Add(3250 * time.Millisecond), 0.5},
	}

	for _, tt := range tests {
		got := tp.Phase(tt.at)
		if got < tt.want-0.001 || got > tt.want+0.001 {
			t.Errorf("%s: Phase = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestFirstTapSetsBeatOriginAtDefaultBPM(t *testing.T) {
	tp := New(2*time.Second, 8)
	tp.Phase(base) // 起点を先に決めておく

	tp.Tap(base.Add(1100 * time.Millisecond))

	if got := tp.Phase(base.Add(1100 * time.Millisecond)); got > 0.001 {
		t.Errorf("1回目のタップ直後: Phase = %v, want 0", got)
	}
	if got := tp.Phase(base.Add(1350 * time.Millisecond)); got < 0.499 || got > 0.501 {
		t.Errorf("1回目のタップから250ms後: Phase = %v, want 0.5 (既定BPM)", got)
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

func TestResetRestoresDefault(t *testing.T) {
	tp := New(2*time.Second, 8)
	tp.Tap(base)
	tp.Tap(base.Add(time.Second)) // 60 BPM

	tp.Reset()

	if got := tp.BPM(); got != DefaultBPM {
		t.Errorf("Reset後: BPM = %v, want %v", got, DefaultBPM)
	}
	if tp.Measured() {
		t.Error("Reset後: Measured = true, want false")
	}
	// 拍頭の起点も捨て、次に Phase を呼んだ時刻から既定テンポで刻み直す
	if got := tp.Phase(base.Add(1300 * time.Millisecond)); got > 0.001 {
		t.Errorf("Reset後の最初の Phase = %v, want 0", got)
	}
	if got := tp.Phase(base.Add(1550 * time.Millisecond)); got < 0.499 || got > 0.501 {
		t.Errorf("Reset後250ms: Phase = %v, want 0.5", got)
	}
}
