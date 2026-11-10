package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/cyokozai/kagelife/internal/shadermgr"
	"github.com/cyokozai/kagelife/internal/uniform"

	"github.com/hajimehoshi/ebiten/v2"
)

// 予約 uniform の型違い。コンパイルは通るが、Ebitengine は描画のときに panic する。
const (
	goodKage = "//kage:unit pixels\npackage main\nvar Time float\nvar Resolution vec2\n" +
		"func Fragment(dstPos vec4, srcPos vec2, color vec4) vec4 { return vec4(dstPos.x/Resolution.x, Time, 0, 1) }\n"
	badTimeKage = "//kage:unit pixels\npackage main\nvar Time vec2\n" +
		"func Fragment(dstPos vec4, srcPos vec2, color vec4) vec4 { return vec4(Time.x, 0, 0, 1) }\n"
	badResolutionKage = "//kage:unit pixels\npackage main\nvar Resolution vec3\n" +
		"func Fragment(dstPos vec4, srcPos vec2, color vec4) vec4 { return vec4(Resolution.x, 0, 0, 1) }\n"
	syntaxErrorKage = "//kage:unit pixels\npackage main\nfunc Fragment(\n"
)

func mustNewShader(t *testing.T, src string) *ebiten.Shader {
	t.Helper()
	sh, err := ebiten.NewShader([]byte(src))
	if err != nil {
		t.Fatalf("NewShader: %v", err)
	}
	t.Cleanup(sh.Dispose)

	return sh
}

func testUniforms() map[string]any {
	return uniform.New().Build(uniform.Input{Time: 1, Width: 72, Height: 72, Beat: 0.5, Frame: 3, Random: 0.2})
}

// 前提の確認: 型違いのシェーダはコンパイルが通り、そのまま描くと panic する。
func TestUniformMismatchPanicsInEbitengine(t *testing.T) {
	sh := mustNewShader(t, badTimeKage)
	img := ebiten.NewImage(1, 1)
	defer img.Deallocate()

	defer func() {
		if recover() == nil {
			t.Error("型違いのシェーダを描いても panic しなかった（前提が崩れている）")
		}
	}()
	img.DrawRectShader(1, 1, sh, &ebiten.DrawRectShaderOptions{Uniforms: testUniforms()})
}

// drawShaderSafe は panic を error にして返し、正しいシェーダはそのまま描く。
func TestDrawShaderSafe(t *testing.T) {
	img := ebiten.NewImage(2, 2)
	defer img.Deallocate()
	u := testUniforms()

	if err := drawShaderSafe(img, 2, 2, mustNewShader(t, goodKage), u); err != nil {
		t.Errorf("正しいシェーダ: err = %v", err)
	}
	for name, src := range map[string]string{"Time vec2": badTimeKage, "Resolution vec3": badResolutionKage} {
		err := drawShaderSafe(img, 2, 2, mustNewShader(t, src), u)
		if err == nil {
			t.Errorf("%s: err = nil, want error", name)

			continue
		}
		if !strings.Contains(err.Error(), "uniform") {
			t.Errorf("%s: err = %q, want it to mention uniform", name, err)
		}
	}
	// panic を拾った後も同じ画像に描ける（状態が壊れていない）
	if err := drawShaderSafe(img, 2, 2, mustNewShader(t, goodKage), u); err != nil {
		t.Errorf("panic の後の正しいシェーダ: err = %v", err)
	}
}

// checkedCompiler は予約 uniform の型違いをコンパイル失敗として返す（本番の経路で panic させない）。
func TestCheckedCompiler(t *testing.T) {
	sh, err := checkedCompiler([]byte(goodKage))
	if err != nil || sh == nil {
		t.Fatalf("正しいシェーダ: sh=%v err=%v", sh, err)
	}
	sh.Dispose()

	for name, src := range map[string]string{"Time vec2": badTimeKage, "Resolution vec3": badResolutionKage, "構文エラー": syntaxErrorKage} {
		sh, err := checkedCompiler([]byte(src))
		if err == nil || sh != nil {
			t.Errorf("%s: sh=%v err=%v, want nil and error", name, sh, err)
		}
	}
}

// shadermgr に checkedCompiler を渡すと、型違いのファイルは nil のスロット（コンパイル失敗と同じ）になる。
func TestCheckedCompiler_ReloadKeepsOldShader(t *testing.T) {
	sm := shadermgr.New(checkedCompiler)
	if err := sm.Reload("/s/a.kage", []byte(goodKage)); err != nil {
		t.Fatal(err)
	}
	old := sm.ShaderAt(0)
	err := sm.Reload("/s/a.kage", []byte(badTimeKage))
	var ce *shadermgr.CompileError
	if !errors.As(err, &ce) {
		t.Fatalf("Reload: err = %v, want *shadermgr.CompileError", err)
	}
	if sm.ShaderAt(0) != old {
		t.Error("型違いで旧シェーダーが差し替えられた")
	}
}

// validateShader は制御口の PUT（Engine.Validate）用。型違いなら error。
func TestValidateShader(t *testing.T) {
	if err := validateShader(mustNewShader(t, goodKage)); err != nil {
		t.Errorf("正しいシェーダ: %v", err)
	}
	if err := validateShader(mustNewShader(t, badResolutionKage)); err == nil {
		t.Error("型違い: err = nil")
	}
	if err := validateShader(nopShader{}); err != nil {
		t.Errorf("ebiten のシェーダでないもの（テスト用）は検査しない: %v", err)
	}
}
