package main

import (
	"strconv"

	"github.com/cyokozai/kagelife/internal/control"
)

// キーボードと制御口（POST /v1/keys・GET /v1/previews）で共有するキーの処理。
// どちらもゲームループ（Update）のゴルーチンから呼ぶ。

// pressKey は key（"0"〜"9"・"["・"]"）を押したときの処理をする。数字は即時切替、
// shift なら数字はクロスフェード。キーに割り当てが無ければ何もせず false を返す。
// キー 0 は -llm-slots 有りのときだけ割り当てがあり、直前の予約名以外のシェーダーへ即時に切り替える（shift は見ない）。
// [ ] はプリセットの拍数を 1 段変える（shift は見ない）。
// 割り当て有りでも、コンパイルに失敗したスロットへの切替は Switch・BeginFade が無視する（キーボードと同じ）。
// 戻り先の記録（back.observe）はここでは行わない。呼ぶ側が先に済ませる。
func (g *Game) pressKey(key string, shift bool) bool {
	switch key {
	case "[":
		g.sm.DecFadeBeats()

		return true
	case "]":
		g.sm.IncFadeBeats()

		return true
	case "0":
		if len(g.llmSlots) == 0 {
			return false
		}
		if i := g.back.target(g.sm.Names(), g.llmSlots, g.sm.Usable); i >= 0 {
			g.sm.Switch(i)
		}

		return true
	}

	n, err := strconv.Atoi(key)
	if err != nil || n < 1 || n > numKeys {
		return false
	}
	idx := keySlots(g.sm.Names(), g.llmSlots)[n-1]
	if idx < 0 {
		return false
	}
	if shift {
		g.sm.BeginFade(idx)
	} else {
		g.sm.Switch(idx)
	}

	return true
}

// observeBack はキー 0 の戻り先のために、今の表示状態を記録する（-llm-slots 有りのときだけ）。
func (g *Game) observeBack() {
	if len(g.llmSlots) > 0 {
		g.back.observe(g.sm.Names(), g.llmSlots, g.sm.ActiveIndex(), g.sm.Fading(), g.sm.FadeTargetIndex())
	}
}

// pressControlKey は制御口の POST /v1/keys の処理（Engine.PressKey）。
// このフレームでキーボードのキーを押したのと同じにするため、handleSlotKeys と同じく先に戻り先を記録する。
func (g *Game) pressControlKey(key string, shift bool) bool {
	g.observeBack()

	return g.pressKey(key, shift)
}

// previewSlots はキー 1〜9 に割り当たっているスロットをキーの順に返す（GET /v1/previews の keys）。
// 割り当ての無いキーは含めない。
func (g *Game) previewSlots() []control.PreviewSlot {
	paths := g.sm.Names()
	out := make([]control.PreviewSlot, 0, numKeys)
	for k, idx := range keySlots(paths, g.llmSlots) {
		if idx < 0 {
			continue
		}
		out = append(out, control.PreviewSlot{
			Key:          strconv.Itoa(k + 1),
			Name:         control.NameFromPath(paths[idx]),
			CompileError: !g.sm.Usable(idx),
		})
	}

	return out
}
