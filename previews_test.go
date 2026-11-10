package main

import (
	"testing"

	"github.com/cyokozai/kagelife/internal/control"
	"github.com/cyokozai/kagelife/internal/uniform"
)

// プレビューの uniform は本番と同じ Time・Beat・Frame・Random で、Resolution だけがセルの大きさ。
// Cursor は画面の座標をセルの大きさへ縮める。
func TestPreviewInput(t *testing.T) {
	in := uniform.Input{Time: 12.5, Width: 960, Height: 540, Beat: 0.25, CursorX: 480, CursorY: 270, Frame: 77, Random: 0.4}

	got := previewInput(in, 1920, 1080, 960, 540)
	want := uniform.Input{
		Time: 12.5, Width: control.PreviewSize, Height: control.PreviewSize, Beat: 0.25,
		CursorX: 36, CursorY: 36, Frame: 77, Random: 0.4,
	}
	if got != want {
		t.Errorf("got %+v\nwant %+v", got, want)
	}

	// 画面の大きさが 0（Draw の前）でも 0 除算しない
	if got := previewInput(in, 0, 0, 10, 10); got.CursorX != 0 || got.CursorY != 0 {
		t.Errorf("画面 0: Cursor = %d,%d, want 0,0", got.CursorX, got.CursorY)
	}
}

// 型違いのシェーダがスロットに入っていても、プレビューは panic せず、そのキーだけを
// compile_error にして（jpeg は null）、他のキーは描き続ける。
func TestRenderPreviewAtlas_UniformMismatchDoesNotPanic(t *testing.T) {
	g := newKeysTestGame(t, nil)
	g.sm.Install("/s/a.kage", mustNewShader(t, goodKage))
	g.sm.Install("/s/b.kage", mustNewShader(t, badTimeKage))
	g.sm.Install("/s/c.kage", mustNewShader(t, goodKage))

	slots := g.renderPreviewAtlas(uniform.Input{Time: 1, Beat: 0.5}, 640, 480)

	want := []control.PreviewSlot{
		{Key: "1", Name: "a"},
		{Key: "2", Name: "b", CompileError: true},
		{Key: "3", Name: "c"},
	}
	if len(slots) != len(want) {
		t.Fatalf("slots = %v, want %v", slots, want)
	}
	for i := range want {
		if slots[i] != want[i] {
			t.Errorf("[%d] = %+v, want %+v", i, slots[i], want[i])
		}
	}
	if g.preview.atlas == nil {
		t.Fatal("アトラスが作られていない")
	}
}

// previewTargets はキー 1〜9 の順に、割り当たったスロットの位置とプレビューの名前を返す（keySlots と同じ対応）。
func TestPreviewTargets(t *testing.T) {
	g := newKeysTestGame(t, []string{"llm_a", "llm_b"}, "a", "b_FAIL", "llm_a", "llm_b", "z")
	got := g.previewTargets()
	want := []previewTarget{
		{slot: control.PreviewSlot{Key: "1", Name: "a"}, idx: 0},
		{slot: control.PreviewSlot{Key: "2", Name: "b_FAIL", CompileError: true}, idx: 1},
		{slot: control.PreviewSlot{Key: "3", Name: "z"}, idx: 4},
		{slot: control.PreviewSlot{Key: "8", Name: "llm_a"}, idx: 2},
		{slot: control.PreviewSlot{Key: "9", Name: "llm_b"}, idx: 3},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}
