package audio

import "time"

// Frame は 1 回の解析の結果と、その音が取り込まれた時刻。
type Frame struct {
	At     time.Time
	Levels Levels
}

// Latest は取り込みのスレッドから描画ループへ最新の Frame だけを渡す（ADR-002 の channel に一本化）。
// 容量 1 の channel で、Put は古い値を捨てて置き換え、どちらの側も待たない。
type Latest struct {
	ch chan Frame
}

func NewLatest() *Latest {
	return &Latest{ch: make(chan Frame, 1)}
}

// Put は f を置く。前の値が残っていれば捨てる。ブロックしない。
func (l *Latest) Put(f Frame) {
	for {
		select {
		case l.ch <- f:
			return
		default:
		}
		select {
		case <-l.ch:
		default:
		}
	}
}

// Take は置かれている値を取り出す。無ければ false。ブロックしない。
func (l *Latest) Take() (Frame, bool) {
	select {
	case f := <-l.ch:
		return f, true
	default:
		return Frame{}, false
	}
}

// Delay は Frame を offset の分だけ遅らせて出す（-av-offset。ライン入力の音は映像より先に届くため）。
// ゲームループのゴルーチンからだけ使う。
type Delay struct {
	offset time.Duration
	queue  []Frame // At の昇順。先頭が最も古い
	cur    Levels  // 直近に出した値
}

func NewDelay(offset time.Duration) *Delay {
	return &Delay{offset: offset}
}

// Push は f を溜める。offset が 0 なら即座に今の値にする。
func (d *Delay) Push(f Frame) {
	if d.offset <= 0 {
		d.cur = f.Levels

		return
	}
	d.queue = append(d.queue, f)
}

// At は now の時点で出す値を返す。At+offset <= now の Frame のうち最新のものを採り、それより古いものは捨てる。
func (d *Delay) At(now time.Time) Levels {
	n := 0
	for n < len(d.queue) && !d.queue[n].At.Add(d.offset).After(now) {
		n++
	}
	if n > 0 {
		d.cur = d.queue[n-1].Levels
		d.queue = append(d.queue[:0], d.queue[n:]...)
	}

	return d.cur
}

// Len は溜まっている Frame の数。
func (d *Delay) Len() int { return len(d.queue) }

// Reset は溜めた値と今の値を捨てる（0 に戻す）。
func (d *Delay) Reset() {
	d.queue = d.queue[:0]
	d.cur = Levels{}
}
