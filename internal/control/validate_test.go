package control

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyokozai/kagelife/internal/shadermgr"
)

// failUniforms は Engine.Validate の代わり。ソースに UNIFORM_FAIL を含むシェーダを型違いとして断る。
func failUniforms(s shadermgr.Shader) error {
	if f, ok := s.(*fakeShader); ok && strings.Contains(f.src, "UNIFORM_FAIL") {
		return errors.New("uniform の型がこのアプリの渡す値と合わない: ui: unexpected uniform value for Time (vec2)")
	}

	return nil
}

// 予約 uniform の型違い（Engine.Validate が断る）は、コンパイル失敗と同じく 422 compile_error。
// ファイルにも Manager にも触れず、last_error に残し、コンパイル済みのシェーダは捨てる。
func TestPutShader_ValidateRejects(t *testing.T) {
	h := newHarness(t, true, "a")
	h.eng.Validate = failUniforms

	st, body := h.do("PUT", "/v1/shaders/bad", jsonBody(map[string]string{"source": src("// UNIFORM_FAIL")}))

	assertError(t, st, body, 422, "compile_error")
	if d, _ := body["diagnostics"].([]any); len(d) != 1 {
		t.Errorf("diagnostics = %v, want 1 件", body["diagnostics"])
	} else if d0, _ := d[0].(map[string]any); d0["line"] != 0.0 || d0["col"] != 0.0 {
		t.Errorf("diagnostics[0] = %v, want line 0 / col 0（位置不明）", d0)
	}
	if msg, _ := body["message"].(string); !strings.Contains(msg, "uniform") {
		t.Errorf("message = %q, want it to mention uniform", msg)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "bad.kage")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("ファイルが書かれた: %v", err)
	}
	if sh := h.comp.last(); sh == nil || !sh.disposed.Load() {
		t.Error("コンパイル済みのシェーダが Dispose されていない")
	}
	_, s := h.do("GET", "/v1/state", "")
	if jsonBody(s["shaders"]) != `["a"]` {
		t.Errorf("shaders = %v, want [a]", s["shaders"])
	}
	if le, _ := s["last_error"].(map[string]any); le == nil || le["shader"] != "bad" {
		t.Errorf("last_error = %v, want shader=bad", s["last_error"])
	}

	// 正しいシェーダは今までどおり書ける
	st, _ = h.do("PUT", "/v1/shaders/good", jsonBody(map[string]string{"source": src("// ok")}))
	if st != 200 {
		t.Errorf("正しいシェーダ: status = %d", st)
	}
}

// Install の処理もゲームループの上で検査し、通らなければ差し替えない（PUT の検査の後に別の経路で来ても落ちない）。
func TestEngineInstallValidated(t *testing.T) {
	e := newTestEngine(t.TempDir(), "a")
	e.Validate = failUniforms

	bad := &fakeShader{src: "UNIFORM_FAIL"}
	if _, err := e.InstallValidated("x", bad); err == nil {
		t.Fatal("err = nil, want error")
	}
	if !bad.disposed.Load() {
		t.Error("断ったシェーダが Dispose されていない")
	}
	if e.SM.Len() != 1 {
		t.Errorf("Len = %d, want 1（差し替えていない）", e.SM.Len())
	}
	if le := e.LastError(); le == nil || le.Shader != "x" {
		t.Errorf("last_error = %v, want shader=x", le)
	}

	created, err := e.InstallValidated("y", &fakeShader{src: "ok"})
	if err != nil || !created {
		t.Errorf("正しいシェーダ: created=%v err=%v", created, err)
	}
}
