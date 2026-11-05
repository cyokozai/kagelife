package shadermgr_test

import (
	"testing"

	"github.com/cyokozai/kagelife/internal/shadermgr"
)

// 制御口（internal/control）から名前で扱うための API のテスト。

// ── IndexOf / Usable ──────────────────────────────────────────────

func TestIndexOf(t *testing.T) {
	m := newManagerWith(t, "s/a.kage", "s/b.kage")

	if got := m.IndexOf("s/b.kage"); got != 1 {
		t.Errorf("IndexOf(b) = %d, want 1", got)
	}
	if got := m.IndexOf("s/./b.kage"); got != 1 {
		t.Errorf("IndexOf(s/./b) = %d, want 1（パスは Clean して比べる）", got)
	}
	if got := m.IndexOf("s/missing.kage"); got != -1 {
		t.Errorf("IndexOf(missing) = %d, want -1", got)
	}
}

func TestIndexOf_NilSlotIsFound(t *testing.T) {
	m := nilSlotManager(t) // [a, b(nil), c]

	if got := m.IndexOf("s/b.kage"); got != 1 {
		t.Errorf("IndexOf(b) = %d, want 1（nil のスロットも枠としては在る）", got)
	}
}

func TestUsable(t *testing.T) {
	m := nilSlotManager(t) // [a, b(nil), c]

	for idx, want := range map[int]bool{-1: false, 0: true, 1: false, 2: true, 3: false} {
		if got := m.Usable(idx); got != want {
			t.Errorf("Usable(%d) = %v, want %v", idx, got, want)
		}
	}
}

// ── Install ───────────────────────────────────────────────────────

func TestInstall_InsertsInFileNameOrder(t *testing.T) {
	m := newManagerWith(t, "s/a.kage", "s/c.kage")
	s := &fakeShader{name: "b"}

	created := m.Install("s/b.kage", s)

	if !created {
		t.Error("created = false, want true")
	}
	if got := m.Names(); len(got) != 3 || got[1] != "s/b.kage" {
		t.Fatalf("Names = %v, want b が 2 番目", got)
	}
	m.Switch(1)
	if m.Active() != s {
		t.Error("Active() が Install したシェーダーではない")
	}
}

func TestInstall_CleansPath(t *testing.T) {
	m := newManagerWith(t, "s/a.kage")

	if created := m.Install("s/./a.kage", &fakeShader{}); created {
		t.Error("created = true, want false（Clean すると同じパス）")
	}
	if m.Len() != 1 {
		t.Errorf("Len = %d, want 1", m.Len())
	}
}

func TestInstall_ReplacesAndDisposesOld(t *testing.T) {
	var made []*fakeShader
	m := shadermgr.New(capturingCompiler(&made))
	mustReload(t, m, "a.kage", nil)
	s := &fakeShader{}

	created := m.Install("a.kage", s)

	if created {
		t.Error("created = true, want false")
	}
	if m.Len() != 1 {
		t.Errorf("Len = %d, want 1", m.Len())
	}
	if !made[0].disposed {
		t.Error("旧シェーダーが Dispose されていない")
	}
	if m.Active() != s {
		t.Error("Active() が差し替え後のシェーダーではない")
	}
}

// 前に挿入しても A は同じスロット（同じシェーダー）を指し続ける。PUT はアクティブを変えない。
func TestInstall_BeforeActiveKeepsActive(t *testing.T) {
	m := newManagerWith(t, "s/b.kage", "s/c.kage")
	m.Switch(1) // c

	m.Install("s/a.kage", &fakeShader{name: "a"})

	if got := shaderName(m.Active()); got != "s/c.kage" {
		t.Errorf("Active = %q, want s/c.kage", got)
	}
	if m.ActiveIndex() != 2 {
		t.Errorf("ActiveIndex = %d, want 2", m.ActiveIndex())
	}
}

// 0 本の状態で Install すると、その 1 本が表示される（ADR-006 補足 14）。
func TestInstall_IntoEmptyBecomesActive(t *testing.T) {
	m := shadermgr.New(srcCompiler)
	s := &fakeShader{name: "x"}

	m.Install("s/x.kage", s)

	if m.Active() != s {
		t.Error("0 本の状態で Install したシェーダーが表示されない")
	}
}

// コンパイル失敗の nil スロットを Install で埋めると、A が nil なら寄せる（Reload と同じ規則）。
func TestInstall_FillsNilSlotAndMovesNilA(t *testing.T) {
	m := shadermgr.New(srcCompiler)
	mustFail(t, m, "s/a.kage") // [a(nil)]、A は nil
	s := &fakeShader{name: "a"}

	created := m.Install("s/a.kage", s)

	if created {
		t.Error("created = true, want false（枠は既に在る）")
	}
	if m.Active() != s {
		t.Error("nil だった A が Install したシェーダーに寄っていない")
	}
}

// A が nil でなければ Install しても A は動かない。
func TestInstall_DoesNotMoveNonNilA(t *testing.T) {
	m := nilSlotManager(t) // [a, b(nil), c]、A = a

	m.Install("s/b.kage", &fakeShader{name: "b"})

	if got := shaderName(m.Active()); got != "s/a.kage" {
		t.Errorf("Active = %q, want s/a.kage", got)
	}
}

// フェード中に前へ挿入しても、A と B は同じスロットを指し続ける。
func TestInstall_DuringFadeKeepsAAndB(t *testing.T) {
	m := newManagerWith(t, "s/b.kage", "s/c.kage")
	m.BeginFade(1) // b → c

	m.Install("s/a.kage", &fakeShader{name: "a"})

	if got := shaderName(m.Active()); got != "s/b.kage" {
		t.Errorf("A = %q, want s/b.kage", got)
	}
	if got := shaderName(m.ActiveB()); got != "s/c.kage" {
		t.Errorf("B = %q, want s/c.kage", got)
	}
	if got := m.FadeTargetIndex(); got != 2 {
		t.Errorf("FadeTargetIndex = %d, want 2", got)
	}
}

// ── FadeTargetIndex ───────────────────────────────────────────────

func TestFadeTargetIndex(t *testing.T) {
	m := newManagerWith(t, "s/a.kage", "s/b.kage")

	if got := m.FadeTargetIndex(); got != -1 {
		t.Errorf("フェード前: FadeTargetIndex = %d, want -1", got)
	}
	m.BeginFade(1)
	if got := m.FadeTargetIndex(); got != 1 {
		t.Errorf("フェード中: FadeTargetIndex = %d, want 1", got)
	}
	m.TickAt(at(0), 60)
	m.TickAt(at(100), 60)
	if got := m.FadeTargetIndex(); got != -1 {
		t.Errorf("フェード完了後: FadeTargetIndex = %d, want -1", got)
	}
}

// ── BeginFadeBeats / CurrentFadeBeats ─────────────────────────────

func TestBeginFadeBeats_UsesGivenBeats(t *testing.T) {
	m := newManagerWith(t, "s/a.kage", "s/b.kage")

	m.BeginFadeBeats(1, 2) // 60 BPM で 2 秒

	if got := m.CurrentFadeBeats(); got != 2 {
		t.Errorf("CurrentFadeBeats = %v, want 2", got)
	}
	if got := m.FadeBeats(); got != shadermgr.FadePresets[shadermgr.DefaultFadeBeatsIdx] {
		t.Errorf("プリセットの FadeBeats が変わった: %v", got)
	}

	m.TickAt(at(0), 60)
	m.TickAt(at(1), 60)
	if !approx(m.MixRatio(), 0.5) {
		t.Errorf("1 秒後の Mix = %v, want 0.5（2 拍で進む）", m.MixRatio())
	}
	m.TickAt(at(2), 60)
	if m.Fading() {
		t.Error("指定拍数を過ぎてもフェード中のまま")
	}
	if m.ActiveIndex() != 1 {
		t.Errorf("ActiveIndex = %d, want 1", m.ActiveIndex())
	}
	if got, want := m.CurrentFadeBeats(), m.FadeBeats(); got != want {
		t.Errorf("フェード完了後の CurrentFadeBeats = %v, want プリセット %v", got, want)
	}
}

func TestCurrentFadeBeats_FollowsPresetForBeginFade(t *testing.T) {
	m := newManagerWith(t, "s/a.kage", "s/b.kage")

	m.BeginFade(1)
	m.IncFadeBeats()

	if got, want := m.CurrentFadeBeats(), m.FadeBeats(); got != want {
		t.Errorf("CurrentFadeBeats = %v, want プリセット %v", got, want)
	}
}

func TestBeginFadeBeats_IgnoresInvalid(t *testing.T) {
	m := nilSlotManager(t) // [a, b(nil), c]

	m.BeginFadeBeats(5, 4) // 範囲外
	m.BeginFadeBeats(1, 4) // nil のスロット
	m.BeginFadeBeats(2, 0) // 0 拍
	m.BeginFadeBeats(2, -1)
	m.BeginFadeBeats(0, 4) // フェードしていない A

	if m.Fading() {
		t.Error("無効な指定でフェードが始まった")
	}
}

// フェード中に同じ B を拍数付きで指示しても無視し、元の拍数のまま進める。
func TestBeginFadeBeats_DuringFade_SameBIgnored(t *testing.T) {
	m := newManagerWith(t, "s/a.kage", "s/b.kage")
	m.BeginFadeBeats(1, 8)
	m.TickAt(at(0), 60)
	m.TickAt(at(2), 60) // Mix 0.25

	m.BeginFadeBeats(1, 2)

	if got := m.CurrentFadeBeats(); got != 8 {
		t.Errorf("CurrentFadeBeats = %v, want 8（同じ B の再指示は無視）", got)
	}
	if !approx(m.MixRatio(), 0.25) {
		t.Errorf("Mix = %v, want 0.25（やり直さない）", m.MixRatio())
	}
}

// Mix ≥ 0.5 で別の先を拍数付きで指示すると、B を確定してから新しい拍数で始める。
func TestBeginFadeBeats_DuringFade_MixAtLeastHalfCommitsB(t *testing.T) {
	m := newManagerWith(t, "s/a.kage", "s/b.kage", "s/c.kage")
	m.BeginFadeBeats(1, 2)
	m.TickAt(at(0), 60)
	m.TickAt(at(1), 60) // Mix 0.5

	m.BeginFadeBeats(2, 16)

	if got := shaderName(m.Active()); got != "s/b.kage" {
		t.Errorf("A = %q, want s/b.kage（B を確定）", got)
	}
	if got := m.FadeTargetIndex(); got != 2 {
		t.Errorf("FadeTargetIndex = %d, want 2", got)
	}
	if got := m.CurrentFadeBeats(); got != 16 {
		t.Errorf("CurrentFadeBeats = %v, want 16", got)
	}
	if m.MixRatio() != 0 {
		t.Errorf("Mix = %v, want 0", m.MixRatio())
	}
}

// 拍数付きのフェード中にプリセットで別の先へ指示し直すと、プリセットの拍数に戻る。
func TestBeginFade_AfterBeginFadeBeatsUsesPreset(t *testing.T) {
	m := newManagerWith(t, "s/a.kage", "s/b.kage", "s/c.kage")
	m.BeginFadeBeats(1, 16)

	m.BeginFade(2)

	if got, want := m.CurrentFadeBeats(), m.FadeBeats(); got != want {
		t.Errorf("CurrentFadeBeats = %v, want プリセット %v", got, want)
	}
}

// Mix < 0.5 で A へ戻す指示はフェードを取りやめ、指定拍数も捨てる。
func TestBeginFadeBeats_DuringFade_BackToAEndsFade(t *testing.T) {
	m := newManagerWith(t, "s/a.kage", "s/b.kage")
	m.BeginFadeBeats(1, 16)

	m.BeginFadeBeats(0, 2)

	if m.Fading() {
		t.Error("A へ戻す指示でフェードが終わっていない")
	}
	if got, want := m.CurrentFadeBeats(), m.FadeBeats(); got != want {
		t.Errorf("CurrentFadeBeats = %v, want プリセット %v", got, want)
	}
}

// Switch でフェードを取りやめたら、指定拍数も捨てる。
func TestSwitch_ClearsGivenBeats(t *testing.T) {
	m := newManagerWith(t, "s/a.kage", "s/b.kage")
	m.BeginFadeBeats(1, 16)

	m.Switch(0)

	if got, want := m.CurrentFadeBeats(), m.FadeBeats(); got != want {
		t.Errorf("CurrentFadeBeats = %v, want プリセット %v", got, want)
	}
}
