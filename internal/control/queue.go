package control

import (
	"errors"
	"time"
)

var (
	// errNotQueued はキューが詰まっていて期限内に投入できなかったこと（処理は実行されない）。
	errNotQueued = errors.New("control: game loop queue is full")
	// errNoReply は投入はできたが期限内に返信が無かったこと（処理は後で実行されうる）。
	errNoReply = errors.New("control: game loop did not reply in time")
)

// Queue は HTTP ハンドラからゲームループ（Update）へ処理を渡す口。
type Queue struct {
	ch chan func(*Engine)
}

// NewQueue は size 件まで溜められる Queue を作る。
func NewQueue(size int) *Queue {
	return &Queue{ch: make(chan func(*Engine), size)}
}

// Drain は溜まっている処理を空になるまで順に実行する。Update から毎 tick 呼ぶ。
// 返信チャネルはバッファ 1 なので、待ち手が去っていても詰まらない。
func (q *Queue) Drain(e *Engine) {
	for {
		select {
		case fn := <-q.ch:
			fn(e)
		default:
			return
		}
	}
}

// Post は処理を待たずに投入する。キューが満杯なら捨てて false を返す。
func (q *Queue) Post(fn func(*Engine)) bool {
	select {
	case q.ch <- fn:
		return true
	default:
		return false
	}
}

// call は fn をゲームループで実行し、その戻り値を返す。
// 投入と返信を合わせて timeout 以内に終わらなければ errNotQueued か errNoReply を返す。
func call[T any](q *Queue, timeout time.Duration, fn func(*Engine) T) (T, error) {
	var zero T
	reply := make(chan T, 1)
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case q.ch <- func(e *Engine) { reply <- fn(e) }:
	case <-timer.C:
		return zero, errNotQueued
	}

	select {
	case v := <-reply:
		return v, nil
	case <-timer.C:
		return zero, errNoReply
	}
}
