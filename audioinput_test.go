package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cyokozai/kagelife/internal/audio"
)

var audioT0 = time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

func TestParseFlags_Audio(t *testing.T) {
	t.Run("既定は無効", func(t *testing.T) {
		opts, err := parseFlags(nil)
		if err != nil {
			t.Fatal(err)
		}
		if opts.audio != "" || opts.listAudio || opts.audioLine {
			t.Errorf("opts = %+v, want audio off", opts)
		}
	})
	t.Run("-audio -audio-line -list-audio を受け取る", func(t *testing.T) {
		opts, err := parseFlags([]string{"-audio", "default", "-audio-line", "-list-audio"})
		if err != nil {
			t.Fatal(err)
		}
		if opts.audio != "default" || !opts.audioLine || !opts.listAudio {
			t.Errorf("opts = %+v", opts)
		}
	})
}

// 描画ループへの受け渡し: step は Latest に溜まった最新の 1 つだけを採る。
func TestAudioInput_StepTakesNewest(t *testing.T) {
	a := newAudioInput("mic", false, 0)
	a.latest.Put(audio.Frame{At: audioT0, Levels: audio.Levels{Bass: 0.1}})
	a.latest.Put(audio.Frame{At: audioT0.Add(time.Millisecond), Levels: audio.Levels{Bass: 0.9}})
	if got := a.step(audioT0.Add(time.Millisecond)); got.Bass != 0.9 {
		t.Errorf("step = %+v, want Bass 0.9", got)
	}
	// 新しい値が来なければ前の値を保つ
	if got := a.step(audioT0.Add(2 * time.Millisecond)); got.Bass != 0.9 {
		t.Errorf("値が無いときの step = %+v, want 前の値", got)
	}
}

// ライン入力（-audio-line）は -av-offset の分だけ後で出る。マイク（既定）は遅らせない。
func TestAudioInput_LineInputIsDelayedByAVOffset(t *testing.T) {
	const off = 200 * time.Millisecond
	line := newAudioInput("line", true, off)
	line.latest.Put(audio.Frame{At: audioT0, Levels: audio.Levels{Onset: 1}})
	if got := line.step(audioT0.Add(off - time.Millisecond)); got.Onset != 0 {
		t.Errorf("ライン入力 offset 前: Onset = %v, want 0", got.Onset)
	}
	if got := line.step(audioT0.Add(off)); got.Onset != 1 {
		t.Errorf("ライン入力 offset 後: Onset = %v, want 1", got.Onset)
	}

	mic := newAudioInput("mic", false, off)
	mic.latest.Put(audio.Frame{At: audioT0, Levels: audio.Levels{Onset: 1}})
	if got := mic.step(audioT0); got.Onset != 1 {
		t.Errorf("マイク: Onset = %v, want 1（遅らせない）", got.Onset)
	}
}

// デバイスが止まったら、描画は続けたまま値を 0 に戻し、HUD と記録関数に出す。
func TestAudioInput_StopZeroesAndReports(t *testing.T) {
	a := newAudioInput("mic", false, 0)
	var recorded string
	a.record = func(msg string) { recorded = msg }
	a.latest.Put(audio.Frame{At: audioT0, Levels: audio.Levels{Mid: 0.7}})
	if got := a.step(audioT0); got.Mid != 0.7 {
		t.Fatalf("step = %+v", got)
	}
	a.onStop(errors.New("device stopped"))
	if got := a.step(audioT0.Add(time.Millisecond)); got != (audio.Levels{}) {
		t.Errorf("停止後の step = %+v, want 0", got)
	}
	if !strings.Contains(a.hudText(), "device stopped") {
		t.Errorf("hudText = %q, want the error", a.hudText())
	}
	if !strings.Contains(recorded, "device stopped") {
		t.Errorf("record = %q, want the error", recorded)
	}
	// 止まった後に古い値が届いても 0 のまま
	a.latest.Put(audio.Frame{At: audioT0, Levels: audio.Levels{Mid: 0.5}})
	if got := a.step(audioT0.Add(2 * time.Millisecond)); got != (audio.Levels{}) {
		t.Errorf("停止後に届いた値: %+v, want 0", got)
	}
}

func TestHUDText_Audio(t *testing.T) {
	got := hudText(hudState{BPM: 120, Measured: true, FadeBeats: 4, Audio: "MacBook Pro Microphone"})
	if !strings.Contains(got, "Audio: MacBook Pro Microphone") {
		t.Errorf("hudText = %q", got)
	}
	got = hudText(hudState{BPM: 120, Measured: true, FadeBeats: 4})
	if strings.Contains(got, "Audio") {
		t.Errorf("-audio 無しで Audio が出る: %q", got)
	}
}

// -list-audio の出力は -list-displays に倣い、1 始まりの番号・名前・既定の印。
func TestAudioListLines(t *testing.T) {
	got := audioListLines([]audio.Device{{Name: "Mic A", Default: true}, {Name: "Line B"}})
	want := []string{"audio 1: Mic A (default)", "audio 2: Line B"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := audioListLines(nil); len(got) != 1 || !strings.Contains(got[0], "no") {
		t.Errorf("無いとき: %q", got)
	}
}
