package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cyokozai/kagelife/internal/control"
)

// 数字キーとスロットの対応（-llm-slots）。
// shadermgr の並び（ファイル名順）は変えず、キーがどのスロットを指すかだけをここで決める。

// numKeys は数字キー 1〜9 の数。
const numKeys = 9

// plainKeys は -llm-slots 有りのとき、予約名以外のスロットに割り当てるキー（1〜7）の数。
const plainKeys = numKeys - control.MaxLLMSlots

// isReserved は path のシェーダ名が予約名かを返す。
func isReserved(path string, reserved []string) bool {
	return slices.Contains(reserved, control.NameFromPath(path))
}

// keySlots はキー 1〜9（添字 0〜8）が指すスロットの位置を返す。何も指さないキーは -1。
//
//   - reserved が空: ファイル名順の先頭 9 本（今までどおり）。
//   - reserved が有り: 1〜7 は予約名を除いたスロットのファイル名順の先頭 7 本、
//     8 / 9 は予約名の 1 個目 / 2 個目（ファイルがまだ無ければ -1）。
//
// コンパイルに失敗したスロットも位置を返す（Switch・BeginFade が無視する）。
func keySlots(paths []string, reserved []string) [numKeys]int {
	var keys [numKeys]int
	for i := range keys {
		keys[i] = -1
	}

	if len(reserved) == 0 {
		for i := 0; i < len(paths) && i < numKeys; i++ {
			keys[i] = i
		}

		return keys
	}

	k := 0
	for i, p := range paths {
		if k >= plainKeys {
			break
		}
		if !isReserved(p, reserved) {
			keys[k] = i
			k++
		}
	}
	for j, name := range reserved {
		keys[plainKeys+j] = slices.IndexFunc(paths, func(p string) bool { return control.NameFromPath(p) == name })
	}

	return keys
}

// backKey はキー 0（-llm-slots 有りのときだけ）の戻り先を覚える。
// 直前に映っていた予約名以外のシェーダをパスで持つ（スロットが増えて位置がずれても追える）。
type backKey struct {
	last string
}

// observe は毎フレームの表示状態を記録する。
// フェード中で、フェード先が予約名以外ならフェード先を、そうでなければ予約名以外の A を記録する。
func (b *backKey) observe(paths []string, reserved []string, active int, fading bool, target int) {
	if fading && target >= 0 && target < len(paths) && !isReserved(paths[target], reserved) {
		b.last = paths[target]

		return
	}
	if active >= 0 && active < len(paths) && !isReserved(paths[active], reserved) {
		b.last = paths[active]
	}
}

// target はキー 0 で戻るスロットの位置を返す。記録したシェーダが使えればそれ、
// 無ければ予約名以外の最初の使えるスロット、それも無ければ -1。
func (b *backKey) target(paths []string, reserved []string, usable func(int) bool) int {
	if i := slices.Index(paths, b.last); b.last != "" && i >= 0 && usable(i) {
		return i
	}
	for i, p := range paths {
		if !isReserved(p, reserved) && usable(i) {
			return i
		}
	}

	return -1
}

// llmKeyLabels はキー 8・9 の表示（例: "8: llm_a (LLM)"）を返す。
func llmKeyLabels(reserved []string) []string {
	labels := make([]string, len(reserved))
	for j, name := range reserved {
		labels[j] = fmt.Sprintf("%d: %s (LLM)", plainKeys+j+1, name)
	}

	return labels
}

// llmHUDLine は HUD に出すキー 8・9 の行。予約名が無ければ空。
func llmHUDLine(reserved []string) string {
	return strings.Join(llmKeyLabels(reserved), "  ")
}
