package shadermgr_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/cyokozai/kagelive/internal/shadermgr"
)

// ── モック用ヘルパー ──────────────────────────────────────────────────

// fakeShader はテスト用のダミーシェーダー。
// shadermgr.Shader インターフェース（Dispose()）を実装している。
type fakeShader struct {
	disposed bool
}

func (f *fakeShader) Dispose() { f.disposed = true }

// successCompiler は常に成功するコンパイラ。
var successCompiler shadermgr.ShaderCompiler = func(src []byte) (shadermgr.Shader, error) {
	return &fakeShader{}, nil
}

// failCompiler は常に失敗するコンパイラ。
var failCompiler shadermgr.ShaderCompiler = func(src []byte) (shadermgr.Shader, error) {
	return nil, errors.New("syntax error")
}

// countingCompiler は呼び出し回数を記録する。
func countingCompiler(n *int) shadermgr.ShaderCompiler {
	return func(src []byte) (shadermgr.Shader, error) {
		*n++
		return &fakeShader{}, nil
	}
}

// capturingCompiler は生成した fakeShader を記録する（Dispose 確認用）。
func capturingCompiler(out *[]*fakeShader) shadermgr.ShaderCompiler {
	return func(src []byte) (shadermgr.Shader, error) {
		s := &fakeShader{}
		*out = append(*out, s)
		return s, nil
	}
}

// ── テスト ────────────────────────────────────────────────────────────

func TestNew_InitialState(t *testing.T) {
	m := shadermgr.New(successCompiler)

	if m.Len() != 0 {
		t.Errorf("Len() = %d, want 0", m.Len())
	}
	if m.Active() != nil {
		t.Error("Active() should be nil initially")
	}
}

// TestReload_Success はホットリロードが成功するケースをテーブル駆動で確認する。
func TestReload_Success(t *testing.T) {
	tests := []struct {
		name string
		src  []byte
	}{
		{name: "空のソース", src: []byte{}},
		{name: "有効なソース", src: []byte("package main")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := shadermgr.New(successCompiler)

			if err := m.Reload("shaders/test.kage", tt.src); err != nil {
				t.Fatalf("Reload() error = %v", err)
			}
			if m.Len() != 1 {
				t.Errorf("Len() = %d, want 1", m.Len())
			}
		})
	}
}

// TestReload_CompileError はコンパイルエラー時に旧シェーダーが維持されることを確認する。
func TestReload_CompileError(t *testing.T) {
	m := shadermgr.New(successCompiler)
	m.Reload("shaders/a.kage", []byte("valid"))
	beforeLen := m.Len()

	// エラーになるコンパイラに差し替え
	m.Compiler = failCompiler
	err := m.Reload("shaders/a.kage", []byte("broken"))

	if err == nil {
		t.Error("Reload() should return error")
	}
	if m.Len() != beforeLen {
		t.Errorf("Len() = %d, want %d (should not change)", m.Len(), beforeLen)
	}
}

// TestReload_DisposesOldShader は再ロード時に旧シェーダーの Dispose が呼ばれることを確認する。
func TestReload_DisposesOldShader(t *testing.T) {
	var captured []*fakeShader
	m := shadermgr.New(capturingCompiler(&captured))

	m.Reload("shaders/a.kage", []byte("v1"))
	first := captured[0]

	m.Reload("shaders/a.kage", []byte("v2")) // 同じパスを再ロード

	if !first.disposed {
		t.Error("old shader should be Disposed on Reload")
	}
	if m.Len() != 1 {
		t.Errorf("Len() = %d, want 1 (should replace, not append)", m.Len())
	}
}

// TestReload_NewPathAddsShader は異なるパスだと新規追加されることを確認する。
func TestReload_NewPathAddsShader(t *testing.T) {
	m := shadermgr.New(successCompiler)
	m.Reload("shaders/a.kage", []byte("a"))
	m.Reload("shaders/b.kage", []byte("b"))

	if m.Len() != 2 {
		t.Errorf("Len() = %d, want 2", m.Len())
	}
}

// TestReload_CompilerCallCount は Reload のたびにコンパイラが呼ばれることを確認する。
func TestReload_CompilerCallCount(t *testing.T) {
	count := 0
	m := shadermgr.New(countingCompiler(&count))

	m.Reload("shaders/a.kage", []byte("v1"))
	m.Reload("shaders/a.kage", []byte("v2")) // 同じパスの再ロード
	m.Reload("shaders/b.kage", []byte("v1"))

	if count != 3 {
		t.Errorf("compiler called %d times, want 3", count)
	}
}

// TestSwitch_BoundaryValues はキー切り替えの境界値テスト。
func TestSwitch_BoundaryValues(t *testing.T) {
	m := shadermgr.New(successCompiler)
	for i := range 3 {
		m.Reload(fmt.Sprintf("shaders/%d.kage", i), []byte("src"))
	}

	tests := []struct {
		name       string
		input      int
		wantActive int
	}{
		{name: "先頭(0)", input: 0, wantActive: 0},
		{name: "中間(1)", input: 1, wantActive: 1},
		{name: "末尾(2)", input: 2, wantActive: 2},
		{name: "範囲外(大)", input: 99, wantActive: 2},
		{name: "範囲外(負)", input: -1, wantActive: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.Switch(tt.input)
			if got := m.ActiveIndex(); got != tt.wantActive {
				t.Errorf("ActiveIndex() = %d, want %d", got, tt.wantActive)
			}
		})
	}
}

// TestActive はアクティブシェーダーの取得を確認する。
func TestActive(t *testing.T) {
	var captured []*fakeShader
	m := shadermgr.New(capturingCompiler(&captured))

	// ロード前は nil
	if m.Active() != nil {
		t.Error("Active() should be nil before any load")
	}

	m.Reload("shaders/a.kage", []byte("a"))
	m.Reload("shaders/b.kage", []byte("b"))

	m.Switch(0)
	if m.Active() != captured[0] {
		t.Error("Active() should return shader[0] after Switch(0)")
	}

	m.Switch(1)
	if m.Active() != captured[1] {
		t.Error("Active() should return shader[1] after Switch(1)")
	}
}

// TestNames はシェーダー名一覧の取得を確認する。
func TestNames(t *testing.T) {
	m := shadermgr.New(successCompiler)
	m.Reload("shaders/a.kage", []byte("a"))
	m.Reload("shaders/b.kage", []byte("b"))

	names := m.Names()
	if len(names) != 2 {
		t.Fatalf("Names() len = %d, want 2", len(names))
	}
	if names[0] != "shaders/a.kage" {
		t.Errorf("names[0] = %q, want %q", names[0], "shaders/a.kage")
	}
	if names[1] != "shaders/b.kage" {
		t.Errorf("names[1] = %q, want %q", names[1], "shaders/b.kage")
	}
}
