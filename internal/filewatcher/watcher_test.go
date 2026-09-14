// Package filewatcher のホワイトボックステスト。
//
// 【なぜ package filewatcher（白箱）か】
// デバウンス処理は unexported フィールド raw chan を介して動く。
// fsnotify を使わずにイベントを注入するため、同パッケージから raw に直接書き込む。
// → ファイルシステムへの依存を完全に排除しつつ、タイミング制御ができる。
//
// 【newForTest について】
// watcher.go に実装する「テスト専用コンストラクタ」。
// fsnotify の Watch を開始せず、デバウンスループ goroutine だけ起動する。
// delay を短くすることでテストが高速に走る。
package filewatcher

import (
	"runtime"
	"testing"
	"time"
)

// ── テスト用ヘルパー ──────────────────────────────────────────────

// sendRaw は Watcher の raw チャネルにイベントを注入する（テスト専用）。
// watcher.go の raw フィールドが chan string であることを前提とする。
func sendRaw(w *Watcher, path string) {
	w.raw <- path
}

// drainEvents は timeout 以内に Events チャネルから全イベントを回収する。
func drainEvents(w *Watcher, timeout time.Duration) []string {
	var got []string
	deadline := time.After(timeout)
	for {
		select {
		case p := <-w.Events:
			got = append(got, p)
		case <-deadline:
			return got
		}
	}
}

// ── テスト ───────────────────────────────────────────────────────

// TestNew_InitialState は Watcher が正常に生成されることを確認する。
// 実装ヒント: Events は nil でない読み取り可能なチャネルであること。
func TestNew_InitialState(t *testing.T) {
	w := newForTest(50 * time.Millisecond)
	defer w.Close()

	if w.Events == nil {
		t.Fatal("Events channel must not be nil")
	}
}

// TestDebounce_SingleEvent は単一イベントがそのまま Events に届くことを確認する。
//
// シナリオ:
//
//	raw ← "shaders/a.kage"
//	delay(50ms) 経過後 → Events に "shaders/a.kage" が届く
func TestDebounce_SingleEvent(t *testing.T) {
	const delay = 50 * time.Millisecond
	w := newForTest(delay)
	defer w.Close()

	sendRaw(w, "shaders/a.kage")

	select {
	case got := <-w.Events:
		if got != "shaders/a.kage" {
			t.Errorf("got %q, want %q", got, "shaders/a.kage")
		}
	case <-time.After(delay * 5):
		t.Fatal("timeout: event not received within expected window")
	}
}

// TestDebounce_CoalescesWithinWindow はウィンドウ内の複数イベントが1つにまとめられることを確認する。
//
// シナリオ（VJ中に editor が複数回 Write イベントを発火する状況）:
//
//	raw ← "a.kage" (t=0)
//	raw ← "a.kage" (t=10ms)  ← ウィンドウ内: タイマーリセット
//	raw ← "a.kage" (t=20ms)  ← ウィンドウ内: タイマーリセット
//	delay(50ms) 経過後 → Events に "a.kage" が 1 回だけ届く
func TestDebounce_CoalescesWithinWindow(t *testing.T) {
	const delay = 50 * time.Millisecond
	w := newForTest(delay)
	defer w.Close()

	// 短間隔で連続送信（delay より短い間隔）
	sendRaw(w, "shaders/a.kage")
	time.Sleep(10 * time.Millisecond)
	sendRaw(w, "shaders/a.kage")
	time.Sleep(10 * time.Millisecond)
	sendRaw(w, "shaders/a.kage")

	// delay*2 待って Events を全回収
	got := drainEvents(w, delay*4)

	if len(got) != 1 {
		t.Errorf("got %d events, want 1 (debounce should coalesce): %v", len(got), got)
	}
	if len(got) > 0 && got[0] != "shaders/a.kage" {
		t.Errorf("got path %q, want %q", got[0], "shaders/a.kage")
	}
}

// TestDebounce_SeparateEventsAfterWindow はウィンドウ外のイベントが別々に届くことを確認する。
//
// シナリオ（1ファイルを2回保存する状況）:
//
//	raw ← "a.kage" (t=0)
//	delay(50ms) 経過 → Events に "a.kage"（1回目）
//	raw ← "a.kage" (t=200ms)  ← 新しいウィンドウ
//	delay(50ms) 経過 → Events に "a.kage"（2回目）
func TestDebounce_SeparateEventsAfterWindow(t *testing.T) {
	const delay = 30 * time.Millisecond
	w := newForTest(delay)
	defer w.Close()

	sendRaw(w, "shaders/a.kage")
	time.Sleep(delay * 4) // 確実にウィンドウが閉じるまで待つ
	sendRaw(w, "shaders/a.kage")

	got := drainEvents(w, delay*8)

	if len(got) != 2 {
		t.Errorf("got %d events, want 2 (separate saves should fire separately): %v", len(got), got)
	}
}

// TestDebounce_DifferentFiles は異なるファイルのイベントが個別に届くことを確認する。
//
// シナリオ（2つのシェーダーファイルをほぼ同時に保存）:
//
//	raw ← "a.kage"
//	raw ← "b.kage"
//	→ Events に "a.kage" と "b.kage" がそれぞれ届く（順不同）
func TestDebounce_DifferentFiles(t *testing.T) {
	const delay = 30 * time.Millisecond
	w := newForTest(delay)
	defer w.Close()

	sendRaw(w, "shaders/a.kage")
	sendRaw(w, "shaders/b.kage")

	got := drainEvents(w, delay*8)

	if len(got) != 2 {
		t.Errorf("got %d events, want 2: %v", len(got), got)
	}

	seen := map[string]bool{}
	for _, p := range got {
		seen[p] = true
	}
	if !seen["shaders/a.kage"] {
		t.Error("missing event for shaders/a.kage")
	}
	if !seen["shaders/b.kage"] {
		t.Error("missing event for shaders/b.kage")
	}
}

// TestWatcher_Close_Idempotent は Close() を複数回呼んでもパニックしないことを確認する。
func TestWatcher_Close_Idempotent(t *testing.T) {
	w := newForTest(50 * time.Millisecond)

	// パニックしなければ OK
	w.Close()
	w.Close()
}

// TestWatcher_Close_StopsGoroutine は Close() 後に goroutine がリークしないことを確認する。
//
// 実装ヒント: Close() は quit チャネルを close() して debounceLoop を終了させる。
// sync.Once を使うと Close() の二重呼び出しに安全に対処できる。
func TestWatcher_Close_StopsGoroutine(t *testing.T) {
	before := runtime.NumGoroutine()

	w := newForTest(50 * time.Millisecond)
	w.Close()

	// goroutine が終了するのを少し待つ
	time.Sleep(50 * time.Millisecond)
	after := runtime.NumGoroutine()

	if after > before+1 { // +1 は誤差許容
		t.Errorf("goroutine leak: before=%d after=%d", before, after)
	}
}
