package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestParseFlags_Display(t *testing.T) {
	t.Run("既定は指定なし・一覧なし・ウィンドウ", func(t *testing.T) {
		opts, err := parseFlags(nil)
		if err != nil {
			t.Fatal(err)
		}
		if opts.display != 0 || opts.listDisplays || opts.fullscreen {
			t.Fatalf("opts = %+v", opts)
		}
	})

	t.Run("-display -list-displays -fullscreen を受け取る", func(t *testing.T) {
		opts, err := parseFlags([]string{"-display", "2", "-list-displays", "-fullscreen"})
		if err != nil {
			t.Fatal(err)
		}
		if opts.display != 2 || !opts.listDisplays || !opts.fullscreen {
			t.Fatalf("opts = %+v", opts)
		}
	})

	for _, bad := range []string{"0", "-1", "x"} {
		t.Run("-display は 1 以上の整数: "+bad, func(t *testing.T) {
			var out bytes.Buffer
			if _, err := parseFlagsTo([]string{"-display", bad}, &out); err == nil || !strings.Contains(err.Error(), "-display") {
				t.Fatalf("err = %v, want a -display error", err)
			}
		})
	}
}

// fakeDisplays は displayAPI の偽物。呼ばれた操作を記録する。
type fakeDisplays struct {
	list       []monitorInfo
	setMonitor []int
	fullscreen []bool
}

func (f *fakeDisplays) monitors() []monitorInfo { return f.list }
func (f *fakeDisplays) useMonitor(i int)        { f.setMonitor = append(f.setMonitor, i) }
func (f *fakeDisplays) setFullscreen(on bool)   { f.fullscreen = append(f.fullscreen, on) }

func twoDisplays() *fakeDisplays {
	return &fakeDisplays{list: []monitorInfo{
		{Name: "Built-in Retina Display", W: 1470, H: 956, Scale: 2},
		{Name: "HDMI", W: 1920, H: 1080, Scale: 1},
	}}
}

func TestDisplayLines(t *testing.T) {
	got := displayLines(twoDisplays().list)
	want := []string{
		"display 1: Built-in Retina Display 1470x956 (scale 2.0)",
		"display 2: HDMI 1920x1080 (scale 1.0)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	if got := displayLines(nil); !reflect.DeepEqual(got, []string{"display: none"}) {
		t.Fatalf("空のとき got %v", got)
	}
}

func TestSetupDisplay(t *testing.T) {
	t.Run("指定なしならモニタも全画面も触らない", func(t *testing.T) {
		d := twoDisplays()
		if err := setupDisplay(options{}, d, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		if len(d.setMonitor) != 0 || len(d.fullscreen) != 0 {
			t.Fatalf("d = %+v", d)
		}
	})

	t.Run("-display 2 -fullscreen は 2 番目（添字 1）に移して全画面にする", func(t *testing.T) {
		d := twoDisplays()
		if err := setupDisplay(options{display: 2, fullscreen: true}, d, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(d.setMonitor, []int{1}) || !reflect.DeepEqual(d.fullscreen, []bool{true}) {
			t.Fatalf("d = %+v", d)
		}
	})

	t.Run("範囲外の -display は一覧を出して起動エラー", func(t *testing.T) {
		d := twoDisplays()
		var out bytes.Buffer
		err := setupDisplay(options{display: 3}, d, &out)
		if err == nil || !strings.Contains(err.Error(), "-display 3") {
			t.Fatalf("err = %v", err)
		}
		if !strings.Contains(out.String(), "display 2: HDMI") {
			t.Fatalf("一覧が出ていない: %q", out.String())
		}
		if len(d.setMonitor) != 0 {
			t.Fatalf("範囲外なのに SetMonitor した: %v", d.setMonitor)
		}
	})
}

func TestCursorSync(t *testing.T) {
	var calls []bool
	c := &cursorSync{set: func(hidden bool) { calls = append(calls, hidden) }}

	c.update(false) // 初回はウィンドウ: 表示にそろえる
	c.update(false)
	c.update(true) // 全画面: 隠す
	c.update(true)
	c.update(false) // ウィンドウに戻す: 表示

	want := []bool{false, true, false}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v（変わったときだけ呼ぶ）", calls, want)
	}
}
