package control

import (
	"errors"
	"testing"
	"time"
)

func TestQueue_DrainRunsAllPendingInOrder(t *testing.T) {
	q := NewQueue(8)
	var got []int
	for i := 0; i < 3; i++ {
		if !q.Post(func(*Engine) { got = append(got, i) }) {
			t.Fatal("Post が失敗した")
		}
	}

	q.Drain(nil)

	if len(got) != 3 || got[0] != 0 || got[2] != 2 {
		t.Errorf("実行順 = %v, want [0 1 2]", got)
	}
}

func TestQueue_DrainReturnsWhenEmpty(t *testing.T) {
	q := NewQueue(8)
	done := make(chan struct{})

	go func() { q.Drain(nil); close(done) }()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("空のキューで Drain が返らない")
	}
}

func TestQueue_PostFailsWhenFull(t *testing.T) {
	q := NewQueue(1)
	q.Post(func(*Engine) {})

	if q.Post(func(*Engine) {}) {
		t.Error("満杯のキューに Post できた")
	}
}

func TestCall_ReturnsReplyFromLoop(t *testing.T) {
	q := NewQueue(8)
	stop := runLoop(q, &Engine{})
	defer stop()

	got, err := call(q, time.Second, func(*Engine) int { return 42 })

	if err != nil || got != 42 {
		t.Errorf("call = %v, %v; want 42, nil", got, err)
	}
}

func TestCall_TimesOutWithoutLoop(t *testing.T) {
	q := NewQueue(8)

	_, err := call(q, 20*time.Millisecond, func(*Engine) int { return 1 })

	if !errors.Is(err, errNoReply) {
		t.Errorf("err = %v, want errNoReply", err)
	}
}

func TestCall_NotQueuedWhenFull(t *testing.T) {
	q := NewQueue(0) // 受け手がいなければ送れない

	_, err := call(q, 20*time.Millisecond, func(*Engine) int { return 1 })

	if !errors.Is(err, errNotQueued) {
		t.Errorf("err = %v, want errNotQueued", err)
	}
}

// 応答を待つ側がタイムアウトで去った後でも、Drain（= Update）が返信で詰まらないこと。
func TestDrainDoesNotBlockAfterCallerTimedOut(t *testing.T) {
	q := NewQueue(8)
	_, err := call(q, 10*time.Millisecond, func(*Engine) int { return 1 })
	if !errors.Is(err, errNoReply) {
		t.Fatalf("err = %v, want errNoReply", err)
	}

	done := make(chan struct{})
	go func() { q.Drain(nil); close(done) }()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Drain が返信の送信で詰まった")
	}
}
