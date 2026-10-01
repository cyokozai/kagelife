// 統合テスト: 実ファイルへの書き込みから Events を受け取るまでの時間を測る。
//
// PRD の非機能要件「保存〜反映 < 200ms」のうち、ファイル監視が占める区間の確認。
// デバウンスは 100ms で確定しているので、残りの余裕は約 100ms になる。
// 測定値は t.Logf に出す（go test -v で見える）。
package filewatcher

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// saveToEventLimit は PRD の非機能要件「保存〜反映 < 200ms」。
const saveToEventLimit = 200 * time.Millisecond

func TestIntegration_SaveToEventLatency(t *testing.T) {
	if testing.Short() {
		t.Skip("skip integration test in -short mode")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "latency.kage")
	if err := os.WriteFile(path, []byte("v0"), 0o644); err != nil {
		t.Fatal(err)
	}

	w, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)

	const rounds = 5
	var worst time.Duration
	for i := 1; i <= rounds; i++ {
		start := time.Now()
		if err := os.WriteFile(path, []byte(fmt.Sprintf("v%d", i)), 0o644); err != nil {
			t.Fatal(err)
		}

		select {
		case got := <-w.Events:
			elapsed := time.Since(start)
			if got != path {
				t.Fatalf("round %d: got %q, want %q", i, got, path)
			}
			t.Logf("round %d: save -> Events = %v (debounce %v)", i, elapsed, w.delay)
			if elapsed > worst {
				worst = elapsed
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("round %d: timeout waiting for Events", i)
		}

		// 次の書き込みが同じデバウンス窓に入らないよう間を空ける
		time.Sleep(3 * w.delay)
	}

	t.Logf("worst save -> Events latency over %d rounds: %v (limit %v)", rounds, worst, saveToEventLimit)
	if worst >= saveToEventLimit {
		t.Errorf("save -> Events latency %v, want < %v", worst, saveToEventLimit)
	}
}
