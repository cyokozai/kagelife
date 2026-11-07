package main

import (
	"reflect"
	"testing"
)

func paths(names ...string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = "/s/" + n + ".kage"
	}

	return out
}

func allUsable(int) bool { return true }

func TestKeySlots(t *testing.T) {
	none := -1
	cases := []struct {
		label    string
		paths    []string
		reserved []string
		want     [9]int
	}{
		{
			"フラグ無し: ファイル名順の先頭 9 本",
			paths("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k"),
			nil,
			[9]int{0, 1, 2, 3, 4, 5, 6, 7, 8},
		},
		{
			"フラグ無し: 9 本未満なら残りのキーは何もしない",
			paths("a", "b", "c"),
			nil,
			[9]int{0, 1, 2, none, none, none, none, none, none},
		},
		{
			"フラグ無し: llm_ で始まる名前も普通のスロット",
			paths("a", "llm_a", "llm_b"),
			nil,
			[9]int{0, 1, 2, none, none, none, none, none, none},
		},
		{
			"フラグ有り: 1〜7 は予約名を除いたファイル名順、8・9 は予約名",
			paths("a", "b", "llm_a", "llm_b", "z"),
			[]string{"llm_a", "llm_b"},
			[9]int{0, 1, 4, none, none, none, none, 2, 3},
		},
		{
			"フラグ有り: 8・9 はフラグの順（ファイル名順ではない）",
			paths("a", "llm_a", "llm_b"),
			[]string{"llm_b", "llm_a"},
			[9]int{0, none, none, none, none, none, none, 2, 1},
		},
		{
			"フラグ有り: 予約名以外が 8 本以上でも 1〜7 だけ",
			paths("a", "b", "c", "d", "e", "f", "g", "h", "llm_a"),
			[]string{"llm_a", "llm_b"},
			[9]int{0, 1, 2, 3, 4, 5, 6, 8, none},
		},
		{
			"フラグ有り: 予約名のファイルがまだ無ければ何もしない",
			paths("a", "b"),
			[]string{"llm_a", "llm_b"},
			[9]int{0, 1, none, none, none, none, none, none, none},
		},
		{
			"フラグ有り: 予約名が 1 個ならキー 9 は何もしない",
			paths("a", "llm_a", "llm_b"),
			[]string{"llm_a"},
			[9]int{0, 2, none, none, none, none, none, 1, none},
		},
	}
	for _, c := range cases {
		if got := keySlots(c.paths, c.reserved); got != c.want {
			t.Errorf("%s: got %v, want %v", c.label, got, c.want)
		}
	}
}

func TestBackKey(t *testing.T) {
	reserved := []string{"llm_a", "llm_b"}
	ps := paths("a", "b", "llm_a", "llm_b")

	t.Run("記録が無ければ予約名以外の最初の使えるスロット", func(t *testing.T) {
		var b backKey
		usable := func(i int) bool { return i != 0 } // a はコンパイル失敗
		if got := b.target(ps, reserved, usable); got != 1 {
			t.Errorf("target = %d, want 1 (b)", got)
		}
	})

	t.Run("予約名以外が無ければ -1", func(t *testing.T) {
		var b backKey
		if got := b.target(paths("llm_a"), reserved, allUsable); got != -1 {
			t.Errorf("target = %d, want -1", got)
		}
	})

	t.Run("直前に映っていた予約名以外のスロットへ戻る", func(t *testing.T) {
		var b backKey
		b.observe(ps, reserved, 1, false, -1) // b が映っている
		b.observe(ps, reserved, 2, false, -1) // llm_a に切り替えた
		if got := b.target(ps, reserved, allUsable); got != 1 {
			t.Errorf("target = %d, want 1 (b)", got)
		}
	})

	t.Run("フェード中は予約名以外のフェード先を記録する", func(t *testing.T) {
		var b backKey
		b.observe(ps, reserved, 2, true, 0) // llm_a から a へフェード中
		if got := b.target(ps, reserved, allUsable); got != 0 {
			t.Errorf("target = %d, want 0 (a)", got)
		}
	})

	t.Run("予約名へのフェード中は A を記録する", func(t *testing.T) {
		var b backKey
		b.observe(ps, reserved, 1, true, 3) // b から llm_b へフェード中
		if got := b.target(ps, reserved, allUsable); got != 1 {
			t.Errorf("target = %d, want 1 (b)", got)
		}
	})

	t.Run("記録はパスで持ち、スロットが増えて位置がずれても同じシェーダへ戻る", func(t *testing.T) {
		var b backKey
		b.observe(ps, reserved, 1, false, -1) // b
		grown := paths("0_new", "a", "b", "llm_a", "llm_b")
		if got := b.target(grown, reserved, allUsable); got != 2 {
			t.Errorf("target = %d, want 2 (b)", got)
		}
	})

	t.Run("記録したスロットが使えなくなったら最初の使えるスロット", func(t *testing.T) {
		var b backKey
		b.observe(ps, reserved, 1, false, -1) // b
		usable := func(i int) bool { return i != 1 }
		if got := b.target(ps, reserved, usable); got != 0 {
			t.Errorf("target = %d, want 0 (a)", got)
		}
	})

	t.Run("範囲外の位置は記録しない", func(t *testing.T) {
		var b backKey
		b.observe(ps, reserved, 1, false, -1)
		b.observe(nil, reserved, 0, false, -1) // シェーダが 0 本
		if got := b.target(ps, reserved, allUsable); got != 1 {
			t.Errorf("target = %d, want 1 (b)", got)
		}
	})
}

func TestShaderListLines_LLMSlots(t *testing.T) {
	got := shaderListLines(paths("a", "llm_a", "z"), []string{"llm_a", "llm_b"})
	want := []string{
		"shader 1: /s/a.kage",
		"shader 2: /s/z.kage",
		"shader 8: llm_a (LLM)",
		"shader 9: llm_b (LLM)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestShaderListLines_LLMSlotsMoreThanSeven(t *testing.T) {
	got := shaderListLines(paths("a", "b", "c", "d", "e", "f", "g", "h"), []string{"llm_a"})
	want := []string{
		"shader 1: /s/a.kage",
		"shader 2: /s/b.kage",
		"shader 3: /s/c.kage",
		"shader 4: /s/d.kage",
		"shader 5: /s/e.kage",
		"shader 6: /s/f.kage",
		"shader 7: /s/g.kage",
		"shader -: /s/h.kage (no key)",
		"shader 8: llm_a (LLM)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestHUDText_LLMSlots(t *testing.T) {
	got := hudText(hudState{BPM: 120, Measured: true, FadeBeats: 4, Mix: 0.25, LLMSlots: []string{"llm_a", "llm_b"}})
	want := "BPM: 120.0  FadeBeats: 4.0  Mix: 0.25\n8: llm_a (LLM)  9: llm_b (LLM)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
