package control

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cyokozai/kagelife/internal/tempo"
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

	st := e.State()

	if st.Active != "01_uv" {
		t.Errorf("Active = %q", st.Active)
	}
	if len(st.Shaders) != 2 || st.Shaders[1] != "02_time_sin" {
		t.Errorf("Shaders = %v", st.Shaders)
	}
	// タップ前は既定の 120 BPM で、測定値ではない（ADR-006 / ADR-008 D3）
	if st.BPM != tempo.DefaultBPM || st.BPMMeasured {
		t.Errorf("BPM/BPMMeasured = %v/%v, want %v/false", st.BPM, st.BPMMeasured, tempo.DefaultBPM)
	}
	if st.FPS != 59.9 || st.Resolution != [2]int{1280, 720} {
		t.Errorf("FPS/Resolution = %v/%v", st.FPS, st.Resolution)
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

// タップも set_bpm もしていなくても、既定の 120 BPM でクロスフェードできる。
func TestEngineCrossfade_WorksAtDefaultBPM(t *testing.T) {
	e := newTestEngine(t.TempDir(), "a", "b")

	beats, err := e.Crossfade("b", 0)

	if err != nil || beats != 4 {
		t.Fatalf("Crossfade(b) = %v, %v; want 4, nil", beats, err)
	}
	if st := e.State(); !st.Fade.Fading || st.Fade.Target != "b" || st.BPM != tempo.DefaultBPM {
		t.Errorf("State = %+v, want 既定 BPM で b へフェード中", st)
	}
}

func TestEngineCrossfade(t *testing.T) {
	e := newTestEngine(t.TempDir(), "a", "b")

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

// フェード中に同じ先へ指示し直しても無視されるので、返す拍数は進行中のフェードのもの。
func TestEngineCrossfade_SameTargetReportsCurrentBeats(t *testing.T) {
	e := newTestEngine(t.TempDir(), "a", "b")
	if _, err := e.Crossfade("b", 8); err != nil {
		t.Fatal(err)
	}

	beats, err := e.Crossfade("b", 2)

	if err != nil || beats != 8 {
		t.Errorf("Crossfade(b, 2) = %v, %v; want 8（進行中のフェードの拍数）", beats, err)
	}
	if got := e.State().Fade.Beats; got != 8 {
		t.Errorf("fade.beats = %v, want 8", got)
	}
}

// フェードしていない active への crossfade は何もせず成功する（補足 6）。
func TestEngineCrossfade_ToActiveIsNoop(t *testing.T) {
	e := newTestEngine(t.TempDir(), "a", "b")

	beats, err := e.Crossfade("a", 2)

	if err != nil || beats != 2 {
		t.Errorf("Crossfade(a, 2) = %v, %v; want 2, nil", beats, err)
	}
	if e.State().Fade.Fading {
		t.Error("active への crossfade でフェードが始まった")
	}
}

// コンパイルに失敗したファイル（nil のスロット）は state の shaders には出るが、
// switch・crossfade では読み込まれていないものとして not_found にする。
func TestEngine_NilSlotIsNotFound(t *testing.T) {
	dir := t.TempDir()
	e := newTestEngine(dir, "a")
	e.SM.Compiler = (&fakeCompiler{}).compile
	if err := e.ReloadFile(filepath.Join(dir, "broken.kage"), []byte("SEMANTIC_FAIL")); err == nil {
		t.Fatal("ReloadFile が失敗しなかった")
	}

	if got := e.State().Shaders; len(got) != 2 || got[1] != "broken" {
		t.Errorf("Shaders = %v, want [a broken]", got)
	}
	if err := e.Switch("broken"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Switch(broken) = %v, want ErrNotFound", err)
	}
	if _, err := e.Crossfade("broken", 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("Crossfade(broken) = %v, want ErrNotFound", err)
	}
	if got := e.State().Active; got != "a" {
		t.Errorf("Active = %q, want a", got)
	}
}

// ReloadFile の失敗は shadermgr.CompileError の形（path:行:列: msg）で last_error に入る。
func TestEngineReloadFile_LastErrorUsesCompileErrorFormat(t *testing.T) {
	dir := t.TempDir()
	e := newTestEngine(dir, "a")
	e.SM.Compiler = (&fakeCompiler{}).compile
	path := filepath.Join(dir, "a.kage")

	_ = e.ReloadFile(path, []byte("SEMANTIC_FAIL"))

	if le := e.LastError(); le == nil || !strings.HasPrefix(le.Message, path+":3:5: ") {
		t.Errorf("LastError = %+v, want message が %q で始まる", le, path+":3:5: ")
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

	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	e.Now = func() time.Time { return now }

	e.SetBPM(140)

	if st := e.State(); st.BPM != 140 || !st.BPMMeasured {
		t.Errorf("BPM/BPMMeasured = %v/%v, want 140/true", st.BPM, st.BPMMeasured)
	}
	if got := e.Tapper.Phase(now); got > 0.001 {
		t.Errorf("Phase(設定時刻) = %v, want 0（設定した時刻が拍頭）", got)
	}
}

// タップで測った BPM も bpm_measured は true。
func TestEngineState_TappedBPMIsMeasured(t *testing.T) {
	e := newTestEngine(t.TempDir())
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	e.Tapper.Tap(now)
	e.Tapper.Tap(now.Add(500 * time.Millisecond))

	if st := e.State(); st.BPM != 120 || !st.BPMMeasured {
		t.Errorf("BPM/BPMMeasured = %v/%v, want 120/true", st.BPM, st.BPMMeasured)
	}
}
