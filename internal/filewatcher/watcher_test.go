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
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
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

// waitGoroutines は goroutine 数が want 以下に戻るまで最大 timeout 待ち、最後の値を返す。
// runtime の後始末には僅かな遅れがあるため一度きりの比較ではなく待ち合わせる。
// 許容誤差は設けない（リークした goroutine は待っても消えないので必ず検出される）。
func waitGoroutines(want int, timeout time.Duration) int {
	deadline := time.Now().Add(timeout)
	for {
		n := runtime.NumGoroutine()
		if n <= want || time.Now().After(deadline) {
			return n
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestWatcher_Close_StopsGoroutine は Close() 後に goroutine が 1 本も残らないことを確認する。
// 保留中のデバウンスタイマーがある状態で Close しても、タイマー側を含めて止まること。
func TestWatcher_Close_StopsGoroutine(t *testing.T) {
	before := runtime.NumGoroutine()

	w := newForTest(time.Hour) // タイマーを保留させたままにする
	sendRaw(w, "shaders/a.kage")
	sendRaw(w, "shaders/b.kage")
	w.Close()

	if after := waitGoroutines(before, time.Second); after > before {
		t.Errorf("goroutine leak: before=%d after=%d", before, after)
	}
}

// TestWatcher_Close_StopsGoroutineAfterFire は発火済みのタイマーが Events の空きを待って
// 止まっている状態（受け手がいない）でも、Close() で goroutine が残らないことを確認する。
func TestWatcher_Close_StopsGoroutineAfterFire(t *testing.T) {
	before := runtime.NumGoroutine()

	w := newForTest(time.Millisecond)
	for i := 0; i < cap(w.events)*3; i++ { // Events を溢れさせ、送信側を待たせる
		sendRaw(w, fmt.Sprintf("shaders/f%d.kage", i))
	}
	time.Sleep(100 * time.Millisecond)
	w.Close()

	if after := waitGoroutines(before, time.Second); after > before {
		t.Errorf("goroutine leak: before=%d after=%d", before, after)
	}
}

// TestWatcher_Close_ClosesChannels は Close() 後に Events と Removed が close されることを確認する。
func TestWatcher_Close_ClosesChannels(t *testing.T) {
	w := newForTest(50 * time.Millisecond)
	w.Close()
	w.Close() // 二重 Close でも二重 close のパニックが起きないこと

	for name, ch := range map[string]<-chan string{"Events": w.Events, "Removed": w.Removed} {
		select {
		case _, ok := <-ch:
			if ok {
				t.Errorf("%s: received a value, want closed channel", name)
			}
		case <-time.After(time.Second):
			t.Errorf("%s: not closed after Close()", name)
		}
	}
}

// TestDebounce_RemovedWhenFileMissing は発火時にファイルが無ければ Removed に届くことを確認する。
func TestDebounce_RemovedWhenFileMissing(t *testing.T) {
	const delay = 30 * time.Millisecond
	w := newForTestWithStat(delay, func(string) bool { return false })
	defer w.Close()

	sendRaw(w, "shaders/gone.kage")

	select {
	case got := <-w.Removed:
		if got != "shaders/gone.kage" {
			t.Errorf("got %q, want %q", got, "shaders/gone.kage")
		}
	case <-time.After(delay * 20):
		t.Fatal("timeout: removal not reported on Removed")
	}
	if got := drainEvents(w, delay*4); len(got) != 0 {
		t.Errorf("Events must stay empty for a removed file, got %v", got)
	}
}

// TestDebounce_RemovedDoesNotBlockWithoutReader は Removed の受け手がいなくても
// デバウンスループが止まらない（満杯なら捨てて Events の配送を続ける）ことを確認する。
func TestDebounce_RemovedDoesNotBlockWithoutReader(t *testing.T) {
	const delay = 10 * time.Millisecond
	var mu sync.Mutex
	exists := false
	w := newForTestWithStat(delay, func(string) bool {
		mu.Lock()
		defer mu.Unlock()
		return exists
	})
	defer w.Close()

	// Removed のバッファを大きく超える数の削除を流す（誰も読まない）
	for i := 0; i < cap(w.removed)*4; i++ {
		sendRaw(w, fmt.Sprintf("shaders/r%d.kage", i))
	}
	time.Sleep(delay * 20)

	mu.Lock()
	exists = true
	mu.Unlock()
	sendRaw(w, "shaders/alive.kage")

	select {
	case got := <-w.Events:
		if got != "shaders/alive.kage" {
			t.Errorf("got %q, want %q", got, "shaders/alive.kage")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("debounce loop blocked by a full Removed channel")
	}
}

// TestDebounce_ManyPathsNoMapLeak は多数のパスが発火し終えたあと、
// タイマー管理用の map にエントリが残らないことを確認する。
func TestDebounce_ManyPathsNoMapLeak(t *testing.T) {
	const delay = 5 * time.Millisecond
	w := newForTest(delay)
	defer w.Close()

	const n = 64 // 旧実装の fired バッファ(16)を超える数
	for i := 0; i < n; i++ {
		sendRaw(w, fmt.Sprintf("shaders/m%d.kage", i))
	}
	got := 0
	deadline := time.After(3 * time.Second)
	for got < n {
		select {
		case <-w.Events:
			got++
		case <-deadline:
			t.Fatalf("received %d/%d events", got, n)
		}
	}

	var pending int
	for i := 0; i < 200; i++ {
		pending = w.pendingCount()
		if pending == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if pending != 0 {
		t.Errorf("pending timers left in map: %d", pending)
	}
}

// TestShouldHandle は fsnotify イベントの取捨の規則を表で確認する。
func TestShouldHandle(t *testing.T) {
	tests := []struct {
		name string
		path string
		op   fsnotify.Op
		want bool
	}{
		{"write", "shaders/a.kage", fsnotify.Write, true},
		{"create", "shaders/a.kage", fsnotify.Create, true},
		{"rename", "shaders/a.kage", fsnotify.Rename, true},
		{"remove", "shaders/a.kage", fsnotify.Remove, true},
		{"chmod only", "shaders/a.kage", fsnotify.Chmod, false},
		{"not kage", "shaders/a.txt", fsnotify.Write, false},
		{"kage then suffix", "shaders/a.kage.swp", fsnotify.Write, false},
		{"emacs lock", "shaders/.#a.kage", fsnotify.Create, false},
		{"macos resource fork", "shaders/._a.kage", fsnotify.Create, false},
		{"dotfile", "shaders/.a.kage", fsnotify.Write, false},
		{"emacs autosave", "shaders/#a.kage", fsnotify.Write, false},
		{"hash in dir is fine", "#dir/a.kage", fsnotify.Write, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldHandle(fsnotify.Event{Name: tt.path, Op: tt.op})
			if got != tt.want {
				t.Errorf("shouldHandle(%q, %v) = %v, want %v", tt.path, tt.op, got, tt.want)
			}
		})
	}
}
