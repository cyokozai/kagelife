// Package tempo はタップテンポ（キーを叩いた間隔から BPM と拍の位相を求める）を提供する。
package tempo

import (
	"math"
	"time"
)

// Tapper はタップ時刻をスライド窓で保持し、BPM と拍の位相を返す。
// 並行アクセスは想定しない（呼び出し側で同一ゴルーチンから使う）。
type Tapper struct {
	timeout time.Duration
	window  int
	taps    []time.Time
	// fixed は SetBPM で外から与えた BPM。タップが 2 回以上たまると 0 に戻る。
	fixed float64
	// origin は fixed の拍頭（タップが 1 回も無いときの位相の基準）。
	origin time.Time
}

// New は Tapper を作る。timeout を超えて空いたタップは新しい測定の 1 回目とみなし、
// window は BPM の算出に使う直近のタップ数（2 未満は 2 に切り上げる）。
func New(timeout time.Duration, window int) *Tapper {
	if window < 2 {
		window = 2
	}

	return &Tapper{timeout: timeout, window: window}
}

// Tap は時刻 t のタップを記録する。t が最後のタップの拍頭になる。
func (tp *Tapper) Tap(t time.Time) {
	if n := len(tp.taps); n > 0 && t.Sub(tp.taps[n-1]) > tp.timeout {
		tp.taps = tp.taps[:0]
	}

	tp.taps = append(tp.taps, t)
	if len(tp.taps) > tp.window {
		tp.taps = append(tp.taps[:0], tp.taps[len(tp.taps)-tp.window:]...)
	}
	if len(tp.taps) >= 2 {
		tp.fixed = 0 // タップで測った BPM が外部設定を上書きする
	}
}

// SetBPM は BPM を外から直接設定し、now を拍頭にする。これまでのタップ履歴は捨てる。
// 以後 1 回だけのタップは拍頭の合わせ直しになり、2 回以上のタップで測った BPM が設定値を上書きする。
// bpm が 0 以下なら何もしない。
func (tp *Tapper) SetBPM(bpm float64, now time.Time) {
	if bpm <= 0 {
		return
	}

	tp.taps = tp.taps[:0]
	tp.fixed = bpm
	tp.origin = now
}

// BPM は窓内のタップ間隔の平均から求めた BPM を返す。タップが 2 回未満なら SetBPM の値（未設定なら 0）。
func (tp *Tapper) BPM() float64 {
	n := len(tp.taps)
	if n < 2 {
		return tp.fixed
	}

	span := tp.taps[n-1].Sub(tp.taps[0]).Seconds()
	if span <= 0 {
		return 0
	}

	return 60 / (span / float64(n-1))
}

// Phase は最後のタップを拍頭としたときの now の拍内位置を 0..1 で返す。
// BPM が未確定なら 0。
func (tp *Tapper) Phase(now time.Time) float32 {
	bpm := tp.BPM()
	if bpm <= 0 {
		return 0
	}

	beat := 60 / bpm
	origin := tp.origin
	if n := len(tp.taps); n > 0 {
		origin = tp.taps[n-1]
	}
	elapsed := now.Sub(origin).Seconds()
	phase := math.Mod(elapsed, beat) / beat
	if phase < 0 {
		phase += 1
	}

	return float32(phase)
}

// Reset はタップ履歴をすべて捨てる。
func (tp *Tapper) Reset() {
	tp.taps = tp.taps[:0]
	tp.fixed = 0
	tp.origin = time.Time{}
}
