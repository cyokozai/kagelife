package control

import (
	"path/filepath"
	"testing"
)

// 削除したファイルは一覧から消え、アクティブは残りへ寄る。
func TestEngineRemoveFile_DropsFromState(t *testing.T) {
	dir := t.TempDir()
	e := newTestEngine(dir, "a", "b")
	if err := e.Switch("a"); err != nil {
		t.Fatal(err)
	}

	e.RemoveFile(filepath.Join(dir, "a.kage"))

	st := e.State()
	if len(st.Shaders) != 1 || st.Shaders[0] != "b" {
		t.Errorf("Shaders = %v, want [b]", st.Shaders)
	}
	if st.Active != "b" {
		t.Errorf("Active = %q, want b", st.Active)
	}
}

// 消したファイルの last_error は消す。
func TestEngineRemoveFile_ClearsItsLastError(t *testing.T) {
	dir := t.TempDir()
	e := newTestEngine(dir, "a", "b")
	e.RecordError("a", "boom")

	e.RemoveFile(filepath.Join(dir, "a.kage"))

	if le := e.LastError(); le != nil {
		t.Errorf("LastError = %+v, want nil", le)
	}
}

// 他のファイルの last_error には触れない。
func TestEngineRemoveFile_KeepsOtherLastError(t *testing.T) {
	dir := t.TempDir()
	e := newTestEngine(dir, "a", "b")
	e.RecordError("b", "boom")

	e.RemoveFile(filepath.Join(dir, "a.kage"))

	if le := e.LastError(); le == nil || le.Shader != "b" {
		t.Errorf("LastError = %+v, want b のエラーが残る", le)
	}
}

// 読み込まれていないファイルの削除は何もしない。
func TestEngineRemoveFile_Unknown(t *testing.T) {
	dir := t.TempDir()
	e := newTestEngine(dir, "a")

	e.RemoveFile(filepath.Join(dir, "zzz.kage"))

	if st := e.State(); len(st.Shaders) != 1 || st.Active != "a" {
		t.Errorf("State = %+v", st)
	}
}
