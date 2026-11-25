package audio

import (
	"strings"
	"testing"
)

var testDevices = []Device{
	{Name: "MacBook Pro Microphone", Default: true},
	{Name: "Scarlett 2i2 USB"},
	{Name: "BlackHole 2ch"},
	{Name: "BlackHole 16ch"},
}

// -audio の値: default は OS の既定、整数は -list-audio の番号（1 始まり）、それ以外は名前の部分一致（大小を区別しない）。
func TestResolveDevice(t *testing.T) {
	cases := []struct {
		spec string
		want int // 選ばれる番号（0 始まり）。-1 は既定
		err  string
	}{
		{"default", -1, ""},
		{"2", 1, ""},
		{"scarlett", 1, ""},
		{"blackhole 16", 3, ""},
		{"BlackHole 2ch", 2, ""}, // 完全一致は部分一致より優先
		{"blackhole", 0, "BlackHole 2ch"},
		{"0", 0, "1"},
		{"9", 0, "1"},
		{"nothing", 0, "no audio input device matches"},
	}
	for _, c := range cases {
		t.Run(c.spec, func(t *testing.T) {
			got, err := resolveDevice(testDevices, c.spec)
			if c.err != "" {
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("err = %v, want containing %q", err, c.err)
				}

				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("index = %d, want %d", got, c.want)
			}
		})
	}
}

func TestResolveDevice_NoDevices(t *testing.T) {
	if _, err := resolveDevice(nil, "default"); err == nil || !strings.Contains(err.Error(), "no audio input device") {
		t.Errorf("err = %v", err)
	}
}
