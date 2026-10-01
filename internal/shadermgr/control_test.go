package shadermgr_test

import (
	"testing"
	"time"

	"github.com/cyokozai/kagelife/internal/shadermgr"
)

// 制御口（internal/control）から名前で扱うための API のテスト。

func TestIndexOf(t *testing.T) {
	m := shadermgr.New(successCompiler)
	mustReload(t, m, "a.kage", nil)
	mustReload(t, m, "b.kage", nil)

	if got := m.IndexOf("b.kage"); got != 1 {
		t.Errorf("IndexOf(b) = %d, want 1", got)
	}
	if got := m.IndexOf("missing.kage"); got != -1 {
		t.Errorf("IndexOf(missing) = %d, want -1", got)
	}
}

func TestInstall_AppendsNewShader(t *testing.T) {
	m := shadermgr.New(successCompiler)
	mustReload(t, m, "a.kage", nil)
	s := &fakeShader{}

	created := m.Install("b.kage", s)

	if !created {
		t.Error("created = false, want true")
	}
	if m.Len() != 2 || m.IndexOf("b.kage") != 1 {
		t.Fatalf("Len = %d, IndexOf(b) = %d", m.Len(), m.IndexOf("b.kage"))
	}
	m.Switch(1)
	if m.Active() != s {
		t.Error("Active() が Install したシェーダーではない")
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

func TestFadeTargetIndex(t *testing.T) {
	m := shadermgr.New(successCompiler)
	mustReload(t, m, "a.kage", nil)
	mustReload(t, m, "b.kage", nil)

	if got := m.FadeTargetIndex(); got != -1 {
		t.Errorf("フェード前: FadeTargetIndex = %d, want -1", got)
	}
	m.BeginFade(1)
	if got := m.FadeTargetIndex(); got != 1 {
		t.Errorf("フェード中: FadeTargetIndex = %d, want 1", got)
	}
}

func TestBeginFadeBeats_UsesGivenBeats(t *testing.T) {
	m := shadermgr.New(successCompiler)
	mustReload(t, m, "a.kage", nil)
	mustReload(t, m, "b.kage", nil)

	m.BeginFadeBeats(1, 0.001) // 300 BPM で 0.2ms

	if got := m.CurrentFadeBeats(); got != 0.001 {
		t.Errorf("CurrentFadeBeats = %v, want 0.001", got)
	}
	if got := m.FadeBeats(); got != shadermgr.FadePresets[shadermgr.DefaultFadeBeatsIdx] {
		t.Errorf("プリセットの FadeBeats が変わった: %v", got)
	}

	time.Sleep(5 * time.Millisecond)
	m.Tick(300)

	if m.Fading() {
		t.Error("指定拍数を過ぎてもフェード中のまま")
	}
	if m.ActiveIndex() != 1 {
		t.Errorf("ActiveIndex = %d, want 1", m.ActiveIndex())
	}
}

func TestCurrentFadeBeats_FollowsPresetForBeginFade(t *testing.T) {
	m := shadermgr.New(successCompiler)
	mustReload(t, m, "a.kage", nil)
	mustReload(t, m, "b.kage", nil)

	m.BeginFade(1)
	m.IncFadeBeats()

	if got, want := m.CurrentFadeBeats(), m.FadeBeats(); got != want {
		t.Errorf("CurrentFadeBeats = %v, want プリセット %v", got, want)
	}
}

func TestBeginFadeBeats_IgnoresInvalid(t *testing.T) {
	m := shadermgr.New(successCompiler)
	mustReload(t, m, "a.kage", nil)
	mustReload(t, m, "b.kage", nil)

	m.BeginFadeBeats(5, 4)
	m.BeginFadeBeats(1, 0)

	if m.Fading() {
		t.Error("範囲外の index や 0 拍でフェードが始まった")
	}
}
