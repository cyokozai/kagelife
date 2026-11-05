package shadermgr_test

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/cyokozai/kagelife/internal/shadermgr"
)

// srcCompiler はソースを名前に持つ fakeShader を返す。
// ソースが "ERR " で始まる場合は、その後ろをメッセージとするエラーを返す。
var srcCompiler shadermgr.ShaderCompiler = func(src []byte) (shadermgr.Shader, error) {
	s := string(src)
	if msg, ok := strings.CutPrefix(s, "ERR "); ok {
		return nil, errors.New(msg)
	}
	return &fakeShader{name: s}, nil
}

// newManagerWith は paths を順に Reload した Manager を返す。
// 各シェーダーのソースはパスそのもの（srcCompiler で名前になる）。
func newManagerWith(t *testing.T, paths ...string) *shadermgr.Manager {
	t.Helper()
	m := shadermgr.New(srcCompiler)
	for _, p := range paths {
		mustReload(t, m, p, []byte(p))
	}
	return m
}

// shaderName は Shader の名前を返す（nil なら ""）。
func shaderName(s shadermgr.Shader) string {
	if s == nil {
		return ""
	}
	return s.(*fakeShader).name
}

func approx(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-4 }

// t0 はテスト用の基準時刻。
var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// at は t0 から sec 秒後の時刻を返す。
func at(sec float64) time.Time { return t0.Add(time.Duration(sec * float64(time.Second))) }

// 既定の拍数は 4 拍。BPM 60 なら 4 秒でフェードが完了する。
const bpm60 = 60.0

func TestFadeBeats_Default(t *testing.T) {
	m := shadermgr.New(successCompiler)
	if got := m.FadeBeats(); got != shadermgr.FadePresets[shadermgr.DefaultFadeBeatsIdx] {
		t.Errorf("FadeBeats() = %v, want %v", got, shadermgr.FadePresets[shadermgr.DefaultFadeBeatsIdx])
	}
}

func TestIncFadeBeats_StopsAtUpperEnd(t *testing.T) {
	m := shadermgr.New(successCompiler)
	for range len(shadermgr.FadePresets) + 3 {
		m.IncFadeBeats()
	}
	want := shadermgr.FadePresets[len(shadermgr.FadePresets)-1]
	if got := m.FadeBeats(); got != want {
		t.Errorf("FadeBeats() = %v, want %v", got, want)
	}
}

func TestDecFadeBeats_StopsAtLowerEnd(t *testing.T) {
	m := shadermgr.New(successCompiler)
	for range len(shadermgr.FadePresets) + 3 {
		m.DecFadeBeats()
	}
	if got := m.FadeBeats(); got != shadermgr.FadePresets[0] {
		t.Errorf("FadeBeats() = %v, want %v", got, shadermgr.FadePresets[0])
	}
}

func TestBeginFade_StartsFade(t *testing.T) {
	m := newManagerWith(t, "s/a.kage", "s/b.kage")

	m.BeginFade(1)

	if !m.Fading() {
		t.Fatal("Fading() = false, want true")
	}
	if got := shaderName(m.ActiveB()); got != "s/b.kage" {
		t.Errorf("ActiveB() = %q, want s/b.kage", got)
	}
	if m.MixRatio() != 0 {
		t.Errorf("MixRatio() = %v, want 0", m.MixRatio())
	}
	if m.ActiveIndex() != 0 {
		t.Errorf("ActiveIndex() = %d, want 0", m.ActiveIndex())
	}
}

func TestBeginFade_Ignored(t *testing.T) {
	tests := []struct {
		name string
		idx  int
	}{
		{name: "A と同じ", idx: 0},
		{name: "範囲外(大)", idx: 2},
		{name: "範囲外(負)", idx: -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newManagerWith(t, "s/a.kage", "s/b.kage")
			m.BeginFade(tt.idx)
			if m.Fading() {
				t.Error("Fading() = true, want false")
			}
			if m.ActiveB() != nil {
				t.Error("ActiveB() should be nil")
			}
		})
	}
}

func TestActiveB_NilWhenNotFading(t *testing.T) {
	m := newManagerWith(t, "s/a.kage")
	if m.ActiveB() != nil {
		t.Error("ActiveB() should be nil when not fading")
	}
}

func TestTickAt_AdvancesMix(t *testing.T) {
	m := newManagerWith(t, "s/a.kage", "s/b.kage")
	m.BeginFade(1)

	m.TickAt(at(0), bpm60) // 基準時刻の記録のみ
	if m.MixRatio() != 0 {
		t.Fatalf("MixRatio() after first tick = %v, want 0", m.MixRatio())
	}
	m.TickAt(at(1), bpm60)
	if !approx(m.MixRatio(), 0.25) {
		t.Errorf("MixRatio() at 1s = %v, want 0.25", m.MixRatio())
	}
	m.TickAt(at(3), bpm60)
	if !approx(m.MixRatio(), 0.75) {
		t.Errorf("MixRatio() at 3s = %v, want 0.75", m.MixRatio())
	}
}

func TestTickAt_CompletesFadeAndCommitsB(t *testing.T) {
	m := newManagerWith(t, "s/a.kage", "s/b.kage")
	m.BeginFade(1)

	m.TickAt(at(0), bpm60)
	m.TickAt(at(4), bpm60)

	if m.Fading() {
		t.Error("Fading() = true, want false after completion")
	}
	if m.ActiveIndex() != 1 {
		t.Errorf("ActiveIndex() = %d, want 1", m.ActiveIndex())
	}
	if got := shaderName(m.Active()); got != "s/b.kage" {
		t.Errorf("Active() = %q, want s/b.kage", got)
	}
	if m.ActiveB() != nil {
		t.Error("ActiveB() should be nil after completion")
	}
	if m.MixRatio() != 0 {
		t.Errorf("MixRatio() = %v, want 0", m.MixRatio())
	}
}

func TestTickAt_NoopWhenNotFading(t *testing.T) {
	m := newManagerWith(t, "s/a.kage", "s/b.kage")
	m.TickAt(at(0), bpm60)
	m.TickAt(at(100), bpm60)
	if m.Fading() || m.MixRatio() != 0 || m.ActiveIndex() != 0 {
		t.Errorf("state changed without fade: fading=%v mix=%v idx=%d", m.Fading(), m.MixRatio(), m.ActiveIndex())
	}
}

// フェード中に拍数を変えても、Mix は逆戻りも一気に完了もしない。
func TestTickAt_ChangingBeatsMidFadeIsContinuous(t *testing.T) {
	tests := []struct {
		name    string
		change  func(m *shadermgr.Manager)
		wantMix float32 // 0.5 の後、さらに 1 秒進めたときの Mix
	}{
		// 4 拍 → 8 拍: 残りは 1 秒あたり 1/8
		{name: "拍数を増やす", change: (*shadermgr.Manager).IncFadeBeats, wantMix: 0.625},
		// 4 拍 → 3 拍: 1 秒あたり 1/3
		{name: "拍数を減らす", change: (*shadermgr.Manager).DecFadeBeats, wantMix: 0.5 + 1.0/3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newManagerWith(t, "s/a.kage", "s/b.kage")
			m.BeginFade(1)
			m.TickAt(at(0), bpm60)
			m.TickAt(at(2), bpm60) // 0.5

			tt.change(m)
			m.TickAt(at(2), bpm60) // 時間が進まなければ Mix は変わらない
			if !approx(m.MixRatio(), 0.5) {
				t.Fatalf("MixRatio() right after change = %v, want 0.5", m.MixRatio())
			}
			m.TickAt(at(3), bpm60)
			if !approx(m.MixRatio(), tt.wantMix) {
				t.Errorf("MixRatio() = %v, want %v", m.MixRatio(), tt.wantMix)
			}
		})
	}
}

// フェード中に BPM を変えても Mix は連続する。
func TestTickAt_ChangingBPMMidFadeIsContinuous(t *testing.T) {
	m := newManagerWith(t, "s/a.kage", "s/b.kage")
	m.BeginFade(1)
	m.TickAt(at(0), bpm60)
	m.TickAt(at(2), bpm60) // 0.5

	m.TickAt(at(2.5), 120) // 4 拍 @120 = 2 秒 → 0.5 秒で +0.25
	if !approx(m.MixRatio(), 0.75) {
		t.Errorf("MixRatio() = %v, want 0.75", m.MixRatio())
	}
}

func TestTickAt_FreezesWhenBPMNotPositive(t *testing.T) {
	for _, bpm := range []float64{0, -10} {
		m := newManagerWith(t, "s/a.kage", "s/b.kage")
		m.BeginFade(1)
		m.TickAt(at(0), bpm60)
		m.TickAt(at(1), bpm60) // 0.25

		m.TickAt(at(2), bpm)
		m.TickAt(at(50), bpm)
		if !approx(m.MixRatio(), 0.25) || !m.Fading() {
			t.Fatalf("bpm=%v: frozen state mix=%v fading=%v, want 0.25 true", bpm, m.MixRatio(), m.Fading())
		}

		// 凍結が解けても、溜まった時間で一気に進めない
		m.TickAt(at(100), bpm60)
		if !approx(m.MixRatio(), 0.25) {
			t.Errorf("bpm=%v: MixRatio() right after unfreeze = %v, want 0.25", bpm, m.MixRatio())
		}
		m.TickAt(at(101), bpm60)
		if !approx(m.MixRatio(), 0.5) {
			t.Errorf("bpm=%v: MixRatio() 1s after unfreeze = %v, want 0.5", bpm, m.MixRatio())
		}
	}
}

func TestTick_UsesWallClock(t *testing.T) {
	m := newManagerWith(t, "s/a.kage", "s/b.kage")
	m.BeginFade(1)
	m.Tick(bpm60)
	m.Tick(bpm60)
	if !m.Fading() {
		t.Error("Fading() = false, want true (4 拍 @60 は 4 秒)")
	}
	if m.MixRatio() < 0 || m.MixRatio() >= 1 {
		t.Errorf("MixRatio() = %v, want [0,1)", m.MixRatio())
	}
}

// ── フェード中の再開始 ────────────────────────────────────────────

func TestBeginFade_DuringFade_SameBIgnored(t *testing.T) {
	m := newManagerWith(t, "s/a.kage", "s/b.kage", "s/c.kage")
	m.BeginFade(1)
	m.TickAt(at(0), bpm60)
	m.TickAt(at(1), bpm60) // 0.25

	m.BeginFade(1)

	if !approx(m.MixRatio(), 0.25) {
		t.Errorf("MixRatio() = %v, want 0.25 (unchanged)", m.MixRatio())
	}
	m.TickAt(at(2), bpm60)
	if !approx(m.MixRatio(), 0.5) {
		t.Errorf("MixRatio() = %v, want 0.5 (fade continues)", m.MixRatio())
	}
}

func TestBeginFade_DuringFade_MixBelowHalfKeepsA(t *testing.T) {
	m := newManagerWith(t, "s/a.kage", "s/b.kage", "s/c.kage")
	m.BeginFade(1)
	m.TickAt(at(0), bpm60)
	m.TickAt(at(1), bpm60) // 0.25

	m.BeginFade(2)

	if m.ActiveIndex() != 0 {
		t.Errorf("ActiveIndex() = %d, want 0", m.ActiveIndex())
	}
	if got := shaderName(m.ActiveB()); got != "s/c.kage" {
		t.Errorf("ActiveB() = %q, want s/c.kage", got)
	}
	if m.MixRatio() != 0 || !m.Fading() {
		t.Errorf("mix=%v fading=%v, want 0 true", m.MixRatio(), m.Fading())
	}
	// 新しいフェードは 0 から。直後の Tick で古い基準時刻から一気に進まない
	m.TickAt(at(2), bpm60)
	if !approx(m.MixRatio(), 0) {
		t.Errorf("MixRatio() = %v, want 0 (re-baselined)", m.MixRatio())
	}
	m.TickAt(at(3), bpm60)
	if !approx(m.MixRatio(), 0.25) {
		t.Errorf("MixRatio() = %v, want 0.25", m.MixRatio())
	}
}

func TestBeginFade_DuringFade_MixAtLeastHalfCommitsB(t *testing.T) {
	m := newManagerWith(t, "s/a.kage", "s/b.kage", "s/c.kage")
	m.BeginFade(1)
	m.TickAt(at(0), bpm60)
	m.TickAt(at(2), bpm60) // 0.5

	m.BeginFade(2)

	if m.ActiveIndex() != 1 {
		t.Errorf("ActiveIndex() = %d, want 1", m.ActiveIndex())
	}
	if got := shaderName(m.ActiveB()); got != "s/c.kage" {
		t.Errorf("ActiveB() = %q, want s/c.kage", got)
	}
	if m.MixRatio() != 0 || !m.Fading() {
		t.Errorf("mix=%v fading=%v, want 0 true", m.MixRatio(), m.Fading())
	}
}

// Mix < 0.5 で A 自身を指定したら、A のままフェードを取りやめる。
func TestBeginFade_DuringFade_BackToAEndsFade(t *testing.T) {
	m := newManagerWith(t, "s/a.kage", "s/b.kage")
	m.BeginFade(1)
	m.TickAt(at(0), bpm60)
	m.TickAt(at(1), bpm60) // 0.25

	m.BeginFade(0)

	if m.Fading() || m.ActiveB() != nil || m.ActiveIndex() != 0 {
		t.Errorf("fading=%v B=%v idx=%d, want false nil 0", m.Fading(), m.ActiveB(), m.ActiveIndex())
	}
}

// Mix ≥ 0.5 で元の A を指定したら、B を A に確定して元の A へフェードし直す。
func TestBeginFade_DuringFade_BackToOldAAfterHalf(t *testing.T) {
	m := newManagerWith(t, "s/a.kage", "s/b.kage")
	m.BeginFade(1)
	m.TickAt(at(0), bpm60)
	m.TickAt(at(3), bpm60) // 0.75

	m.BeginFade(0)

	if m.ActiveIndex() != 1 || shaderName(m.ActiveB()) != "s/a.kage" || !m.Fading() {
		t.Errorf("idx=%d B=%q fading=%v, want 1 s/a.kage true", m.ActiveIndex(), shaderName(m.ActiveB()), m.Fading())
	}
}

func TestSwitch_CancelsFade(t *testing.T) {
	m := newManagerWith(t, "s/a.kage", "s/b.kage", "s/c.kage")
	m.BeginFade(1)
	m.TickAt(at(0), bpm60)
	m.TickAt(at(1), bpm60)

	m.Switch(2)

	if m.Fading() || m.ActiveB() != nil || m.MixRatio() != 0 || m.ActiveIndex() != 2 {
		t.Errorf("fading=%v B=%v mix=%v idx=%d", m.Fading(), m.ActiveB(), m.MixRatio(), m.ActiveIndex())
	}
}
