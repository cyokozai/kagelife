package control

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestEngineState_Empty(t *testing.T) {
	e := newTestEngine(t.TempDir())

	st := e.State()

	if st.Active != "" {
		t.Errorf("Active = %q, want \"\"", st.Active)
	}
	if st.Shaders == nil || len(st.Shaders) != 0 {
		t.Errorf("Shaders = %#v, want 空スライス（null にしない）", st.Shaders)
	}
	if st.LastError != nil {
		t.Errorf("LastError = %+v, want nil", st.LastError)
	}
}

func TestEngineState_Populated(t *testing.T) {
	e := newTestEngine(t.TempDir(), "01_uv", "02_time_sin")
	e.Resolution = [2]int{1280, 720}
	e.Tapper.SetBPM(120, time.Now())

	st := e.State()

	if st.Active != "01_uv" {
		t.Errorf("Active = %q", st.Active)
	}
	if len(st.Shaders) != 2 || st.Shaders[1] != "02_time_sin" {
		t.Errorf("Shaders = %v", st.Shaders)
	}
	if st.BPM != 120 || st.FPS != 59.9 || st.Resolution != [2]int{1280, 720} {
		t.Errorf("BPM/FPS/Resolution = %v/%v/%v", st.BPM, st.FPS, st.Resolution)
	}
	if st.Fade.Fading || st.Fade.Target != "" || st.Fade.Beats != 4 {
		t.Errorf("Fade = %+v, want 非フェード・beats 4", st.Fade)
	}
}

func TestEngineSwitch(t *testing.T) {
	e := newTestEngine(t.TempDir(), "a", "b")

	if err := e.Switch("b"); err != nil {
		t.Fatalf("Switch(b) = %v", err)
	}
	if got := e.State().Active; got != "b" {
		t.Errorf("Active = %q, want b", got)
	}
	if err := e.Switch("zzz"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Switch(zzz) = %v, want ErrNotFound", err)
	}
}

func TestEngineCrossfade(t *testing.T) {
	e := newTestEngine(t.TempDir(), "a", "b")

	if _, err := e.Crossfade("b", 0); !errors.Is(err, ErrBPMNotSet) {
		t.Errorf("BPM 未設定: err = %v, want ErrBPMNotSet", err)
	}

	e.SetBPM(128)
	if _, err := e.Crossfade("zzz", 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("未知の名前: err = %v, want ErrNotFound", err)
	}

	beats, err := e.Crossfade("b", 8)
	if err != nil || beats != 8 {
		t.Fatalf("Crossfade(b, 8) = %v, %v", beats, err)
	}
	st := e.State()
	if !st.Fade.Fading || st.Fade.Target != "b" || st.Fade.Beats != 8 {
		t.Errorf("Fade = %+v, want b へ 8 拍", st.Fade)
	}
}

func TestEngineCrossfade_DefaultBeatsIsPreset(t *testing.T) {
	e := newTestEngine(t.TempDir(), "a", "b")
	e.SetBPM(100)

	beats, err := e.Crossfade("b", 0)

	if err != nil || beats != 4 {
		t.Errorf("Crossfade(b, 省略) = %v, %v; want 4（既定のプリセット）", beats, err)
	}
}

func TestEngineInstall(t *testing.T) {
	dir := t.TempDir()
	e := newTestEngine(dir, "a")
	e.RecordError("a", "old failure")
	s := &fakeShader{}

	e.Install("new", s)

	if got := e.SM.IndexOf(filepath.Join(dir, "new.kage")); got != 1 {
		t.Errorf("IndexOf(new) = %d, want 1", got)
	}
	if e.LastError() != nil {
		t.Error("Install 成功で last_error が消えていない")
	}
}

func TestEngineReloadFile_RecordsAndClearsError(t *testing.T) {
	dir := t.TempDir()
	e := newTestEngine(dir, "a")
	e.SM.Compiler = (&fakeCompiler{}).compile
	path := filepath.Join(dir, "a.kage")

	if err := e.ReloadFile(path, []byte("SEMANTIC_FAIL")); err == nil {
		t.Fatal("ReloadFile が失敗しなかった")
	}
	le := e.LastError()
	if le == nil || le.Shader != "a" || le.Message == "" {
		t.Fatalf("LastError = %+v, want shader=a", le)
	}

	if err := e.ReloadFile(path, []byte("ok")); err != nil {
		t.Fatalf("ReloadFile = %v", err)
	}
	if e.LastError() != nil {
		t.Error("成功で last_error が消えていない")
	}
}

func TestEngineSetBPM(t *testing.T) {
	e := newTestEngine(t.TempDir())

	e.SetBPM(140)

	if got := e.State().BPM; got != 140 {
		t.Errorf("BPM = %v, want 140", got)
	}
}
