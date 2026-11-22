package audio

import (
	"fmt"
	"strconv"
	"strings"
)

// Device は入力デバイスの一覧の 1 件（-list-audio）。
type Device struct {
	Name    string
	Default bool // OS の既定の入力
}

// DefaultSpec は -audio の値で OS の既定の入力を選ぶ指定。
const DefaultSpec = "default"

// resolveDevice は -audio の値 spec から devices の番号を決める。
//   - "default": OS の既定の入力（-1 を返す）
//   - 整数: -list-audio の番号（1 始まり）
//   - それ以外: 名前の部分一致（大小を区別しない）。完全一致があればそれ、無ければ部分一致が 1 件のときだけ選ぶ
func resolveDevice(devices []Device, spec string) (int, error) {
	if len(devices) == 0 {
		return 0, fmt.Errorf("audio: no audio input device (check -list-audio)")
	}
	if spec == DefaultSpec {
		return -1, nil
	}
	if n, err := strconv.Atoi(spec); err == nil {
		if n < 1 || n > len(devices) {
			return 0, fmt.Errorf("audio: device number %d is out of range 1..%d (see -list-audio)", n, len(devices))
		}

		return n - 1, nil
	}
	want := strings.ToLower(spec)
	var matches []int
	for i, d := range devices {
		name := strings.ToLower(d.Name)
		if name == want {
			return i, nil
		}
		if strings.Contains(name, want) {
			matches = append(matches, i)
		}
	}
	switch len(matches) {
	case 0:
		return 0, fmt.Errorf("audio: no audio input device matches %q (see -list-audio)", spec)
	case 1:
		return matches[0], nil
	default:
		names := make([]string, 0, len(matches))
		for _, i := range matches {
			names = append(names, devices[i].Name)
		}

		return 0, fmt.Errorf("audio: %q matches more than one device: %s (use the number or the full name)",
			spec, strings.Join(names, ", "))
	}
}
