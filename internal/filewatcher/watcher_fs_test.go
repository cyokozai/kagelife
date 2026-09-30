// 実際の fsnotify を使う New() のテスト。
//
// t.TempDir() の中でファイルを操作し、Events / Removed に何が届くかを確かめる。
// タイマーとカーネル通知の両方に依存するため、待ち時間はデバウンス間隔（100ms）より
// 十分に長く取り、CI の負荷で揺れても落ちないようにしている。
package filewatcher

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

const (
	// fsRecvTimeout は届くはずのイベントを待つ上限。
	fsRecvTimeout = 3 * time.Second
	// fsQuietWindow は「届かないこと」を確かめるための観察時間（デバウンス 100ms の数倍）。
	fsQuietWindow = 600 * time.Millisecond
)

func newWatcherInTempDir(t *testing.T) (*Watcher, string) {
	t.Helper()
	dir := t.TempDir()
	w, err := New(dir)
	if err != nil {
		t.Fatalf("New(%q): %v", dir, err)
	}
	t.Cleanup(w.Close)

	return w, dir
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// recvPath は ch から want が届くまで待つ。別のパスが来たら失敗にする。
func recvPath(t *testing.T, ch <-chan string, name, want string) {
	t.Helper()
	select {
	case got := <-ch:
		if got != want {
			t.Fatalf("%s: got %q, want %q", name, got, want)
		}
	case <-time.After(fsRecvTimeout):
		t.Fatalf("%s: timeout waiting for %q", name, want)
	}
}

// expectQuiet は window の間 ch に何も届かないことを確かめる。
func expectQuiet(t *testing.T, ch <-chan string, name string, window time.Duration) {
	t.Helper()
	select {
	case got := <-ch:
		t.Fatalf("%s: unexpected event %q", name, got)
	case <-time.After(window):
	}
}

func TestNew_NonexistentDir(t *testing.T) {
	w, err := New(filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		w.Close()
		t.Fatal("New on a missing dir must return an error")
	}
	if w != nil {
		t.Errorf("Watcher must be nil on error, got %v", w)
	}
}

func TestNew_WriteExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.kage")
	writeFile(t, path, "v1")

	w, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)

	writeFile(t, path, "v2")
	recvPath(t, w.Events, "Events", path)
	expectQuiet(t, w.Removed, "Removed", fsQuietWindow)
}

func TestNew_CreateNewFile(t *testing.T) {
	w, dir := newWatcherInTempDir(t)
	path := filepath.Join(dir, "new.kage")

	writeFile(t, path, "fresh")
	recvPath(t, w.Events, "Events", path)
}

// TestNew_AtomicSaveByRename は「一時ファイルに書いて rename で置き換える」保存方式を扱う。
// 一時ファイル名は隠しファイルと .kage 以外の 2 通りを試す。どちらも一時ファイル自体は
// 通知せず、置き換え先の .kage が Events に 1 回だけ届くこと。
func TestNew_AtomicSaveByRename(t *testing.T) {
	for _, tmpName := range []string{".a.kage.tmp", "a.kage~"} {
		t.Run(tmpName, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "a.kage")
			writeFile(t, path, "v1")

			w, err := New(dir)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(w.Close)

			tmp := filepath.Join(dir, tmpName)
			writeFile(t, tmp, "v2")
			if err := os.Rename(tmp, path); err != nil {
				t.Fatal(err)
			}

			recvPath(t, w.Events, "Events", path)
			expectQuiet(t, w.Events, "Events (duplicate)", fsQuietWindow)
			expectQuiet(t, w.Removed, "Removed", 0)
		})
	}
}

// TestNew_RenameAwayThenRecreate は元ファイルを退避名へ rename してから同名で書き直す
// 保存方式（vim の backupcopy=no など）。デバウンスでまとまり、存在する側として Events に届く。
func TestNew_RenameAwayThenRecreate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.kage")
	writeFile(t, path, "v1")

	w, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)

	if err := os.Rename(path, path+"~"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, "v2")

	recvPath(t, w.Events, "Events", path)
	expectQuiet(t, w.Removed, "Removed", fsQuietWindow)
}

func TestNew_RemoveGoesToRemoved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.kage")
	writeFile(t, path, "v1")

	w, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	recvPath(t, w.Removed, "Removed", path)
	expectQuiet(t, w.Events, "Events", fsQuietWindow)
}

func TestNew_RenameOutGoesToRemoved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.kage")
	writeFile(t, path, "v1")

	w, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)

	if err := os.Rename(path, filepath.Join(dir, "a.bak")); err != nil {
		t.Fatal(err)
	}
	recvPath(t, w.Removed, "Removed", path)
	expectQuiet(t, w.Events, "Events", fsQuietWindow)
}

func TestNew_IgnoresNonKageAndHiddenFiles(t *testing.T) {
	w, dir := newWatcherInTempDir(t)

	for _, name := range []string{"notes.txt", "a.kage.swp", ".#a.kage", "._a.kage", ".a.kage", "#a.kage#", "#a.kage"} {
		writeFile(t, filepath.Join(dir, name), "x")
	}
	expectQuiet(t, w.Events, "Events", fsQuietWindow)
	expectQuiet(t, w.Removed, "Removed", 0)
}

func TestNew_CloseClosesChannels(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	w.Close()

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

func TestNew_Close_StopsGoroutine(t *testing.T) {
	before := runtime.NumGoroutine()

	dir := t.TempDir()
	w, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "a.kage"), "x")
	time.Sleep(20 * time.Millisecond) // タイマーが保留中の状態で閉じる
	w.Close()

	if after := waitGoroutines(before, 2*time.Second); after > before {
		t.Errorf("goroutine leak: before=%d after=%d", before, after)
	}
}
