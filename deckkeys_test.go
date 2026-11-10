package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cyokozai/kagelife/internal/control"
	"github.com/cyokozai/kagelife/internal/shadermgr"
	"github.com/cyokozai/kagelife/internal/tempo"
)

// newKeysTestGame は names（拡張子なし）のスロットを /s/ の下に並べた Game を作る。
// "FAIL" を含む名前はコンパイルに失敗したスロット（nil）にする。
func newKeysTestGame(t *testing.T, llmSlots []string, names ...string) *Game {
	t.Helper()
	sm := shadermgr.New(func(src []byte) (shadermgr.Shader, error) {
		if strings.Contains(string(src), "FAIL") {
			return nil, errors.New("1:1: compile failed")
		}

		return nopShader{}, nil
	})
	for _, n := range names {
		_ = sm.Reload("/s/"+n+".kage", []byte(n)) // FAIL はエラーでもスロットが残る
	}

	return &Game{sm: sm, tapper: tempo.New(2*time.Second, 8), llmSlots: llmSlots}
}

func activeName(g *Game) string {
	return control.NameFromPath(g.sm.Names()[g.sm.ActiveIndex()])
}

// 数字キーは keySlots と同じスロットへ即時に切り替え、Shift 付きはクロスフェードを始める。
func TestPressKey_Digits(t *testing.T) {
	t.Run("フラグ無し: 数字はファイル名順のスロット", func(t *testing.T) {
		g := newKeysTestGame(t, nil, "a", "b", "c")
		if !g.pressKey("3", false) || activeName(g) != "c" {
			t.Fatalf("key 3: active = %s, want c", activeName(g))
		}
		if !g.pressKey("2", true) || !g.sm.Fading() || g.sm.FadeTargetIndex() != 1 {
			t.Errorf("Shift+2: fading=%v target=%d, want true 1", g.sm.Fading(), g.sm.FadeTargetIndex())
		}
		if activeName(g) != "c" {
			t.Errorf("Shift+2 の後の active = %s, want c（フェード中は A のまま）", activeName(g))
		}
	})

	t.Run("フラグ無し: 割り当ての無い数字と 0 は false で何もしない", func(t *testing.T) {
		g := newKeysTestGame(t, nil, "a", "b")
		g.sm.Switch(1)
		for _, k := range []string{"3", "9", "0"} {
			if g.pressKey(k, false) {
				t.Errorf("key %s: want false", k)
			}
		}
		if activeName(g) != "b" {
			t.Errorf("active = %s, want b", activeName(g))
		}
	})

	t.Run("フラグ有り: 8・9 は予約名、0 は直前の予約名以外へ戻る", func(t *testing.T) {
		g := newKeysTestGame(t, []string{"llm_a", "llm_b"}, "a", "b", "llm_a", "llm_b")
		g.pressControlKey("2", false)
		if !g.pressControlKey("8", false) || activeName(g) != "llm_a" {
			t.Fatalf("key 8: active = %s, want llm_a", activeName(g))
		}
		if !g.pressControlKey("9", true) || g.sm.FadeTargetIndex() != 3 {
			t.Errorf("Shift+9: target = %d, want 3 (llm_b)", g.sm.FadeTargetIndex())
		}
		if !g.pressControlKey("0", false) || activeName(g) != "b" {
			t.Errorf("key 0: active = %s, want b", activeName(g))
		}
	})

	t.Run("フラグ有り: 予約名のファイルが無いキーは false", func(t *testing.T) {
		g := newKeysTestGame(t, []string{"llm_a", "llm_b"}, "a", "llm_a")
		if g.pressKey("9", false) {
			t.Error("key 9: want false")
		}
		if g.pressKey("3", false) {
			t.Error("key 3: want false")
		}
		if !g.pressKey("0", false) {
			t.Error("key 0: want true（-llm-slots 有りなら割り当てがある）")
		}
	})

	t.Run("コンパイルに失敗したスロットのキーは割り当て有りだが切り替わらない", func(t *testing.T) {
		g := newKeysTestGame(t, nil, "a", "b_FAIL")
		if !g.pressKey("2", false) {
			t.Error("key 2: want true")
		}
		if activeName(g) != "a" {
			t.Errorf("active = %s, want a", activeName(g))
		}
	})
}

// [ と ] はプリセットの拍数を 1 段ずつ変える（Shift は関係ない）。
func TestPressKey_Brackets(t *testing.T) {
	g := newKeysTestGame(t, nil, "a")
	before := g.sm.FadeBeats()
	if !g.pressKey("]", true) || g.sm.FadeBeats() <= before {
		t.Errorf("]: FadeBeats = %v, want > %v", g.sm.FadeBeats(), before)
	}
	if !g.pressKey("[", false) || g.sm.FadeBeats() != before {
		t.Errorf("[: FadeBeats = %v, want %v", g.sm.FadeBeats(), before)
	}
}

// previewSlots の割り当ては keySlots（GUI の数字キー）と同じで、キーの順に並ぶ。
func TestPreviewSlots_MatchesKeySlots(t *testing.T) {
	cases := []struct {
		label    string
		llmSlots []string
		names    []string
		want     []control.PreviewSlot
	}{
		{
			"フラグ無し: ファイル名順の先頭 9 本",
			nil,
			[]string{"a", "b_FAIL", "c", "d", "e", "f", "g", "h", "i", "j"},
			[]control.PreviewSlot{
				{Key: "1", Name: "a"}, {Key: "2", Name: "b_FAIL", CompileError: true}, {Key: "3", Name: "c"},
				{Key: "4", Name: "d"}, {Key: "5", Name: "e"}, {Key: "6", Name: "f"},
				{Key: "7", Name: "g"}, {Key: "8", Name: "h"}, {Key: "9", Name: "i"},
			},
		},
		{
			"フラグ有り: 1〜7 は予約名以外、8・9 は予約名（ファイルが無いキーは出さない）",
			[]string{"llm_b", "llm_a"},
			[]string{"a", "llm_a", "z"},
			[]control.PreviewSlot{{Key: "1", Name: "a"}, {Key: "2", Name: "z"}, {Key: "9", Name: "llm_a"}},
		},
		{
			"シェーダ 0 本なら空（nil ではない）",
			nil,
			nil,
			[]control.PreviewSlot{},
		},
	}
	for _, c := range cases {
		g := newKeysTestGame(t, c.llmSlots, c.names...)
		got := g.previewSlots()
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got %v\nwant %v", c.label, got, c.want)
		}
		// キーを押したときの切替先とプレビューの名前が一致する
		for _, s := range got {
			if s.CompileError {
				continue
			}
			g.pressKey(s.Key, false)
			if activeName(g) != s.Name {
				t.Errorf("%s: key %s: active = %s, want %s", c.label, s.Key, activeName(g), s.Name)
			}
		}
	}
}
