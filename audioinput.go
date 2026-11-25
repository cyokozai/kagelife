package main

import (
	"fmt"
	"io"
	"log"
	"time"

	"github.com/cyokozai/kagelife/internal/audio"
)

// audioInput は -audio の状態。取り込み（audio.Capture）から Latest 経由で最新値を受け取り、
// ライン入力なら -av-offset の分だけ遅らせて、毎フレームの uniform の値にする。
// step・hudText・close はゲームループのゴルーチンから、onStop は取り込みのスレッドから呼ばれる。
type audioInput struct {
	spec   string // -audio の値
	name   string // 開いたデバイスの名前（開けなければ spec）
	line   bool   // -audio-line（音が映像より先に届くライン入力。-av-offset の分だけ遅らせる）
	offset time.Duration

	latest  *audio.Latest
	delay   *audio.Delay
	stopped chan error // 取り込みのスレッドから 1 件だけ受ける
	cap     interface{ Close() }
	levels  audio.Levels
	err     error

	// record は失敗を制御口の last_error に残す（nil なら残さない）。
	record func(msg string)
}

func newAudioInput(spec string, line bool, avOffset time.Duration) *audioInput {
	a := &audioInput{spec: spec, name: spec, line: line, latest: audio.NewLatest(), stopped: make(chan error, 1)}
	if line {
		a.offset = avOffset
	}
	a.delay = audio.NewDelay(a.offset)

	return a
}

// start はデバイスを開いて取り込みを始める。失敗しても描画は止めず、HUD と last_error に出す（FR-184）。
func (a *audioInput) start() {
	c, name, err := audio.Open(a.spec, a.latest, a.onStop)
	if err != nil {
		a.fail(err)

		return
	}
	a.cap, a.name = c, name
	log.Printf("audio: capturing %q (%s, %d Hz)", name, a.kind(), audio.SampleRate)
}

func (a *audioInput) kind() string {
	if a.line {
		return fmt.Sprintf("line input, delayed by av-offset %dms", a.offset.Milliseconds())
	}

	return "mic, not delayed"
}

// onStop はデバイスが途中で止まったときに取り込みのスレッドから呼ばれる。待たずに 1 件だけ残す。
func (a *audioInput) onStop(err error) {
	select {
	case a.stopped <- err:
	default:
	}
}

func (a *audioInput) fail(err error) {
	a.err = err
	a.levels = audio.Levels{}
	a.delay.Reset()
	log.Printf("audio: %v (audio uniforms stay 0)", err)
	if a.record != nil {
		a.record(err.Error())
	}
}

// step は now のフレームに使う音の値を返す。Latest の最新の 1 つだけを採り、止まっていれば 0。
func (a *audioInput) step(now time.Time) audio.Levels {
	select {
	case err := <-a.stopped:
		a.fail(err)
	default:
	}
	f, ok := a.latest.Take()
	if a.err != nil {
		return audio.Levels{}
	}
	if ok {
		a.delay.Push(f)
	}
	a.levels = a.delay.At(now)

	return a.levels
}

// hudText は HUD の状態行に出す文字列。
func (a *audioInput) hudText() string {
	if a.err != nil {
		return "ERROR " + a.err.Error()
	}

	return a.name
}

func (a *audioInput) close() {
	if a.cap != nil {
		a.cap.Close()
	}
}

// audioListLines は -list-audio の出力（1 始まりの番号・名前・既定の印）。
func audioListLines(devices []audio.Device) []string {
	if len(devices) == 0 {
		return []string{"no audio input device found"}
	}
	lines := make([]string, 0, len(devices))
	for i, d := range devices {
		line := fmt.Sprintf("audio %d: %s", i+1, d.Name)
		if d.Default {
			line += " (default)"
		}
		lines = append(lines, line)
	}

	return lines
}

// printAudioDevices は -list-audio の出力を out に書く。
func printAudioDevices(out io.Writer) error {
	devices, err := audio.ListDevices()
	if err != nil {
		return err
	}
	for _, line := range audioListLines(devices) {
		if _, err := fmt.Fprintln(out, line); err != nil {
			return err
		}
	}

	return nil
}
