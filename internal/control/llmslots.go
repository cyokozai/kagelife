package control

import (
	"fmt"
	"slices"
	"strings"
)

// MaxLLMSlots は -llm-slots に書ける予約名の上限（キー 8 と 9）。
const MaxLLMSlots = 2

// ParseLLMSlots は -llm-slots の値（カンマ区切りの予約名）を検査して並びのまま返す。
// 空文字列なら nil（機能は無効）。各名前の前後の空白は除く。
// 3 個以上・名前の規則違反・重複はエラー。
func ParseLLMSlots(s string) ([]string, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	if len(parts) > MaxLLMSlots {
		return nil, fmt.Errorf("-llm-slots: at most %d names (keys 8 and 9), got %d", MaxLLMSlots, len(parts))
	}

	names := make([]string, 0, len(parts))
	for _, p := range parts {
		name := strings.TrimSpace(p)
		if !ValidName(name) {
			return nil, fmt.Errorf("-llm-slots: %q must match ^[a-z0-9][a-z0-9_-]{0,63}$ (without the .kage extension)", name)
		}
		if slices.Contains(names, name) {
			return nil, fmt.Errorf("-llm-slots: duplicate name %q", name)
		}
		names = append(names, name)
	}

	return names, nil
}

// forbiddenNameMessage は予約名以外への PUT を断る 403 の説明。
func forbiddenNameMessage(slots []string) string {
	return fmt.Sprintf("this kagelife runs with -llm-slots; PUT may write only the reserved LLM slots (%s)",
		strings.Join(slots, ", "))
}

// slotLiveMessage は映っている予約名への PUT を断る 409 の説明。
func slotLiveMessage(name string, slots []string) string {
	others := make([]string, 0, len(slots))
	for _, s := range slots {
		if s != name {
			others = append(others, s)
		}
	}
	msg := "shader " + name + " is on screen (active, or the target of a running crossfade) and cannot be written"
	if len(others) == 0 {
		return msg + "; wait until it is off screen"
	}

	return msg + "; write the other reserved slot instead (" + strings.Join(others, ", ") + ")"
}
