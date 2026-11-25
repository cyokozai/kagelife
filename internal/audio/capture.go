package audio

import (
	"errors"
	"fmt"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/gen2brain/malgo"
)

// ErrStopped はデバイスが途中で止まった（抜かれた・OS に止められた）こと。
var ErrStopped = errors.New("audio: input device stopped (unplugged or taken by the OS)")

// ListDevices は入力デバイスの一覧を返す（-list-audio）。
func ListDevices() ([]Device, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("audio: init: %w", err)
	}
	defer freeContext(ctx)

	return listDevices(ctx)
}

func listDevices(ctx *malgo.AllocatedContext) ([]Device, error) {
	infos, err := ctx.Devices(malgo.Capture)
	if err != nil {
		return nil, fmt.Errorf("audio: list devices: %w", err)
	}
	out := make([]Device, 0, len(infos))
	for i := range infos {
		out = append(out, Device{Name: infos[i].Name(), Default: infos[i].IsDefault != 0})
	}

	return out, nil
}

func freeContext(ctx *malgo.AllocatedContext) {
	_ = ctx.Uninit()
	ctx.Free()
}

// Capture は開いた入力デバイス。コールバックのスレッドで Analyzer を回し、最新値を Latest に置く。
type Capture struct {
	ctx     *malgo.AllocatedContext
	dev     *malgo.Device
	closing atomic.Bool
	samples []float32 // コールバックで使い回す（確保しない）
}

// Open は spec（resolveDevice の形）の入力デバイスを 48 kHz・モノラル・float32 で開いて取り込みを始める。
// 解析の結果は latest に置く。デバイスが途中で止まったら onStop を（コールバックのスレッドから）呼ぶ。
// 返す name は選ばれたデバイスの名前。
func Open(spec string, latest *Latest, onStop func(error)) (c *Capture, name string, err error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, "", fmt.Errorf("audio: init: %w", err)
	}
	defer func() {
		if err != nil {
			freeContext(ctx)
		}
	}()

	infos, err := ctx.Devices(malgo.Capture)
	if err != nil {
		return nil, "", fmt.Errorf("audio: list devices: %w", err)
	}
	devices := make([]Device, 0, len(infos))
	for i := range infos {
		devices = append(devices, Device{Name: infos[i].Name(), Default: infos[i].IsDefault != 0})
	}
	idx, err := resolveDevice(devices, spec)
	if err != nil {
		return nil, "", err
	}

	cfg := malgo.DefaultDeviceConfig(malgo.Capture)
	cfg.SampleRate = SampleRate
	cfg.Capture.Format = malgo.FormatF32
	cfg.Capture.Channels = 1 // 多チャネルの入力は miniaudio がモノラルに落とす
	name = "default input"
	if idx >= 0 {
		cfg.Capture.DeviceID = infos[idx].ID.Pointer()
		name = infos[idx].Name()
	} else {
		for i := range infos {
			if infos[i].IsDefault != 0 {
				name = infos[i].Name()
			}
		}
	}

	c = &Capture{ctx: ctx}
	an := NewAnalyzer(SampleRate)
	cb := malgo.DeviceCallbacks{
		Data: func(_, in []byte, frames uint32) {
			c.onData(an, latest, in, int(frames))
		},
		Stop: func() {
			if !c.closing.Load() && onStop != nil {
				onStop(ErrStopped)
			}
		},
	}
	dev, err := malgo.InitDevice(ctx.Context, cfg, cb)
	if err != nil {
		return nil, "", fmt.Errorf("audio: open %q: %w", name, err)
	}
	if err := dev.Start(); err != nil {
		dev.Uninit()

		return nil, "", fmt.Errorf("audio: start %q: %w", name, err)
	}
	c.dev = dev

	return c, name, nil
}

// onData は取り込みのコールバック。in は float32（ネイティブのバイト順）のモノラル。
func (c *Capture) onData(an *Analyzer, latest *Latest, in []byte, frames int) {
	if frames <= 0 || len(in) < frames*4 {
		return
	}
	if cap(c.samples) < frames {
		c.samples = make([]float32, frames)
	}
	c.samples = c.samples[:frames]
	copy(c.samples, unsafe.Slice((*float32)(unsafe.Pointer(&in[0])), frames))
	latest.Put(Frame{At: time.Now(), Levels: an.Write(c.samples)})
}

// Close は取り込みを止めてデバイスを閉じる。止めたことは onStop に知らせない。
func (c *Capture) Close() {
	if c == nil {
		return
	}
	c.closing.Store(true)
	if c.dev != nil {
		c.dev.Uninit()
	}
	freeContext(c.ctx)
}
