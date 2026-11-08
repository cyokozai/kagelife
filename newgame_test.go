package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyokozai/kagelife/internal/filewatcher"
)

func TestNewGameErrors(t *testing.T) {
	t.Run("存在しないディレクトリならエラー", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "missing")
		g, err := NewGame(dir, nil, nil)
		if err == nil {
			g.watcher.Close()
			t.Fatal("NewGame: want error, got nil")
		}
	})

	t.Run("監視を始めた後に読み取りが失敗したら監視を閉じる", func(t *testing.T) {
		var w *filewatcher.Watcher
		origWatcher, origReadDir := newShaderWatcher, readShaderDir
		t.Cleanup(func() { newShaderWatcher, readShaderDir = origWatcher, origReadDir })
		newShaderWatcher = func(dir string) (*filewatcher.Watcher, error) {
			var err error
			w, err = origWatcher(dir)

			return w, err
		}
		errRead := errors.New("read failed")
		readShaderDir = func(string) ([]os.DirEntry, error) { return nil, errRead }

		if _, err := NewGame(t.TempDir(), nil, nil); !errors.Is(err, errRead) {
			t.Fatalf("NewGame: err = %v, want %v", err, errRead)
		}
		if w == nil {
			t.Fatal("前提: 監視が作られていない")
		}

		// Close されていれば、送信側が止まって Events が close される。
		select {
		case _, ok := <-w.Events:
			if ok {
				t.Error("Events から値を受け取った。close されているはず")
			}
		case <-time.After(2 * time.Second):
			w.Close()
			t.Error("監視が閉じられていない（Events が close されない）")
		}
	})
}
