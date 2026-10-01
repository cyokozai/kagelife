// Package tempo はタップテンポ（キーを叩いた間隔から BPM と拍の位相を求める）を提供する。
package tempo

import (
	"math"
	"time"
)

// BPM の既定値と、測定値を丸める範囲。
const (
	// DefaultBPM はまだ測定していないとき（起動直後・Reset 後）に使うテンポ。
	DefaultBPM = 120.0
	// MinBPM は測定値の下限。これより遅い測定は MinBPM に丸める。
	MinBPM = 40.0
	// MaxBPM は測定値の上限。二度押しのような極端に短い間隔は MaxBPM に丸める。
	MaxBPM = 300.0
)

// Tapper はタップ時刻をスライド窓で保持し、BPM と拍の位相を返す。
// 測定がそろうまでは DefaultBPM で拍を刻み、新しい測定の途中でも直前の BPM を保つ。
// 並行アクセスは想定しない（呼び出し側で同一ゴルーチンから使う）。
type Tapper struct {
	timeout time.Duration
	window  int
	taps    []time.Time

	bpm       float64   // 現在使っている BPM（測定値、無ければ DefaultBPM）
	measured  bool      // bpm が測定値なら true
	origin    time.Time // 拍頭の起点
	hasOrigin bool      // origin が決まっていれば true
}

// New は Tapper を作る。timeout を超えて空いたタップは新しい測定の 1 回目とみなし、
// window は BPM の算出に使う直近のタップ数（2 未満は 2 に切り上げる）。
func New(timeout time.Duration, window int) *Tapper {
	if window < 2 {
		window = 2
	}

	return &Tapper{timeout: timeout, window: window, bpm: DefaultBPM}
}

// Tap は時刻 t のタップを記録する。t が拍頭になる。
// timeout を超えて空いたタップは新しい測定の 1 回目とし、2 回目がそろうまでは直前の BPM を使い続ける。
func (tp *Tapper) Tap(t time.Time) {
	if n := len(tp.taps); n > 0 && t.Sub(tp.taps[n-1]) > tp.timeout {
		tp.taps = tp.taps[:0]
	}

	tp.taps = append(tp.taps, t)
	if len(tp.taps) > tp.window {
		tp.taps = append(tp.taps[:0], tp.taps[len(tp.taps)-tp.window:]...)
	}

	tp.origin, tp.hasOrigin = t, true

	if n := len(tp.taps); n >= 2 {
		tp.bpm = clampBPM(tp.taps[n-1].Sub(tp.taps[0]).Seconds(), n-1)
		tp.measured = true
	}
}

// clampBPM は span 秒に intervals 個の間隔があるときの BPM を MinBPM..MaxBPM に丸めて返す。
func clampBPM(span float64, intervals int) float64 {
	if span <= 0 {
		return MaxBPM
	}

	return math.Min(MaxBPM, math.Max(MinBPM, 60/(span/float64(intervals))))
}

// BPM は現在のテンポを返す。窓内のタップ間隔の平均から求めた測定値（MinBPM..MaxBPM）、
// まだ測定していなければ DefaultBPM。
func (tp *Tapper) BPM() float64 {
	return tp.bpm
}

// Measured は BPM が測定値なら true、既定値なら false を返す。
func (tp *Tapper) Measured() bool {
	return tp.measured
}

// Phase は拍頭から見た now の拍内位置を 0..1 で返す。
// 拍頭は最後のタップ。まだタップしていなければ、最初に Phase を呼んだ時刻を起点にする。
func (tp *Tapper) Phase(now time.Time) float32 {
	if !tp.hasOrigin {
		tp.origin, tp.hasOrigin = now, true
	}

	beat := 60 / tp.bpm
	phase := math.Mod(now.Sub(tp.origin).Seconds(), beat) / beat
	if phase < 0 {
		phase += 1
	}

	return float32(phase)
}

// Reset はタップ履歴と拍頭の起点を捨て、BPM を DefaultBPM に戻す。
func (tp *Tapper) Reset() {
	tp.taps = tp.taps[:0]
	tp.bpm = DefaultBPM
	tp.measured = false
	tp.hasOrigin = false
}
