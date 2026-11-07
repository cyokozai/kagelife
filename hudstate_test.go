package main

import (
	"testing"
	"time"

	"github.com/cyokozai/kagelife/internal/shadermgr"
	"github.com/cyokozai/kagelife/internal/tempo"
)

// nopShader は HUD のテスト用のダミーシェーダー。
type nopShader struct{}

func (nopShader) Dispose() {}

// newHUDTestGame は a.kage と b.kage を読み込んだ、HUD の値を集めるだけの Game を作る。
func newHUDTestGame(t *testing.T) *Game {
	t.Helper()
	sm := shadermgr.New(func([]byte) (shadermgr.Shader, error) { return nopShader{}, nil })
	for _, p := range []string{"a.kage", "b.kage"} {
		if err := sm.Reload(p, []byte("//kage:unit pixels")); err != nil {
			t.Fatalf("Reload(%q): %v", p, err)
		}
	}

	return &Game{
		sm:        sm,
		tapper:    tempo.New(2*time.Second, 8),
		frames:    newFrameStats(frameStatsWindow),
		actualFPS: func() float64 { return 60 },
	}
}

func TestGameHUDStateFadeBeats(t *testing.T) {
	t.Run("フェードしていなければプリセットの拍数", func(t *testing.T) {
		g := newHUDTestGame(t)
		if got, want := g.hudState(time.Now()).FadeBeats, g.sm.FadeBeats(); got != want {
			t.Errorf("FadeBeats = %v, want %v", got, want)
		}
	})

	t.Run("beats 指定のフェード中はその拍数（GET /v1/state の fade.beats と同じ）", func(t *testing.T) {
		g := newHUDTestGame(t)
		const beats = 16
		if g.sm.FadeBeats() == beats {
			t.Fatalf("前提: プリセットの拍数 %v が指定の拍数と同じ", g.sm.FadeBeats())
		}
		g.sm.BeginFadeBeats(1, beats)
		if got := g.hudState(time.Now()).FadeBeats; got != beats {
			t.Errorf("FadeBeats = %v, want %v", got, beats)
		}
		if got, want := g.hudState(time.Now()).FadeBeats, g.sm.CurrentFadeBeats(); got != want {
			t.Errorf("FadeBeats = %v, CurrentFadeBeats() = %v", got, want)
		}
	})
}
