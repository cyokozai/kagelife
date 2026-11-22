package audio

import (
	"sync"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

// 取り込みのスレッドが何度置いても、描画ループは最新の 1 つだけを受け取る（古い値は捨てる）。
func TestLatest_KeepsOnlyNewest(t *testing.T) {
	l := NewLatest()
	if _, ok := l.Take(); ok {
		t.Fatal("空なのに値が取れた")
	}
	for i := range 5 {
		l.Put(Frame{At: t0.Add(time.Duration(i) * time.Millisecond), Levels: Levels{Bass: float32(i) / 10}})
	}
	f, ok := l.Take()
	if !ok || f.Levels.Bass != 0.4 {
		t.Fatalf("Take = %+v, %v; want Bass 0.4", f, ok)
	}
	if _, ok := l.Take(); ok {
		t.Error("取った後も値が残っている（古い値が溜まっている）")
	}
}

// Put はブロックしない（コールバックのスレッドを止めない）。並行に置いても最後に取れるのは 1 つ。
func TestLatest_ConcurrentPutNeverBlocks(t *testing.T) {
	l := NewLatest()
	var wg sync.WaitGroup
	done := make(chan struct{})
	wg.Go(func() {
		for i := range 10000 {
			l.Put(Frame{Levels: Levels{Mid: float32(i)}})
		}
	})
	go func() { wg.Wait(); close(done) }()
	got := 0
	for {
		select {
		case <-done:
			if _, ok := l.Take(); ok {
				got++
			}
			if got == 0 {
				t.Error("一度も値が取れなかった")
			}

			return
		case <-time.After(5 * time.Second):
			t.Fatal("Put が止まった")
		default:
			if _, ok := l.Take(); ok {
				got++
			}
		}
	}
}

// 遅らせ 0 なら最新の値をそのまま返す。
func TestDelay_ZeroReturnsNewest(t *testing.T) {
	d := NewDelay(0)
	d.Push(Frame{At: t0, Levels: Levels{High: 0.5}})
	if got := d.At(t0); got.High != 0.5 {
		t.Errorf("At = %+v, want High 0.5", got)
	}
}

// ライン入力の指定: 値は offset の分だけ後で出る。それまでは前の値（最初は 0）。
func TestDelay_ShiftsByOffset(t *testing.T) {
	d := NewDelay(200 * time.Millisecond)
	d.Push(Frame{At: t0, Levels: Levels{Bass: 1}})
	if got := d.At(t0.Add(100 * time.Millisecond)); got.Bass != 0 {
		t.Errorf("100 ms 後: Bass = %v, want 0（まだ出ない）", got.Bass)
	}
	d.Push(Frame{At: t0.Add(150 * time.Millisecond), Levels: Levels{Bass: 0.2}})
	if got := d.At(t0.Add(200 * time.Millisecond)); got.Bass != 1 {
		t.Errorf("200 ms 後: Bass = %v, want 1", got.Bass)
	}
	if got := d.At(t0.Add(349 * time.Millisecond)); got.Bass != 1 {
		t.Errorf("349 ms 後: Bass = %v, want 1（次の値はまだ）", got.Bass)
	}
	if got := d.At(t0.Add(350 * time.Millisecond)); got.Bass != 0.2 {
		t.Errorf("350 ms 後: Bass = %v, want 0.2", got.Bass)
	}
}

// 溜める量は offset の分だけ（長く走らせても増え続けない）。
func TestDelay_BoundedQueue(t *testing.T) {
	d := NewDelay(100 * time.Millisecond)
	for i := range 10000 {
		at := t0.Add(time.Duration(i) * 16 * time.Millisecond)
		d.Push(Frame{At: at, Levels: Levels{Mid: float32(i)}})
		d.At(at)
	}
	if n := d.Len(); n > 16 {
		t.Errorf("溜まっている数 = %d, want <= 16", n)
	}
}

// Reset で溜めた値を捨てる（デバイスが抜けたら 0 に戻すため）。
func TestDelay_Reset(t *testing.T) {
	d := NewDelay(0)
	d.Push(Frame{At: t0, Levels: Levels{Onset: 1}})
	d.Reset()
	if got := d.At(t0); got != (Levels{}) {
		t.Errorf("Reset の後: %+v, want 0", got)
	}
}
