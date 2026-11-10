package main

import (
	"fmt"
	"image"

	"github.com/cyokozai/kagelife/internal/shadermgr"
	"github.com/cyokozai/kagelife/internal/uniform"

	"github.com/hajimehoshi/ebiten/v2"
)

// 予約 uniform の型違い（例: var Time vec2、var Resolution vec3）は、コンパイルは通るが、
// Ebitengine が描画のときに uniform を詰める所（ui.Shader.AppendUniforms）で panic する。
// panic は描画命令を積む前（DrawRectShader の中で DrawTriangles を呼ぶ前）に起きるので、
// recover すれば画像や命令の列は壊れない（ebiten v2.10.5 の image.go の DrawRectShader を読んで確かめた）。

// drawShaderSafe は dst に w x h で sh を描き、panic したら描かずに error を返す。
func drawShaderSafe(dst *ebiten.Image, w, h int, sh *ebiten.Shader, uniforms map[string]any) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("uniform の型がこのアプリの渡す値と合わない（Time・Beat・Random は float、Frame は int、Resolution・Cursor は vec2）: %v", r)
		}
	}()
	dst.DrawRectShader(w, h, sh, &ebiten.DrawRectShaderOptions{Uniforms: uniforms})

	return nil
}

// probe は試し描き用の 1x1 の画像と、本番と同じ型の uniform。ゲームループ（と起動時の読み込み）からだけ使う。
var probe struct {
	img      *ebiten.Image
	uniforms map[string]any
}

// validateShader は sh を 1x1 に試し描きし、予約 uniform の型違いなら error を返す。
// ebiten のシェーダでないもの（テストの偽物）は検査しない。ゲームループから呼ぶ（Engine.Validate）。
func validateShader(s shadermgr.Shader) error {
	sh, ok := s.(*ebiten.Shader)
	if !ok || sh == nil {
		return nil
	}
	if probe.img == nil {
		probe.img = ebiten.NewImageWithOptions(image.Rect(0, 0, 1, 1), &ebiten.NewImageOptions{Unmanaged: true})
		probe.uniforms = uniform.New().Build(uniform.Input{Width: 1, Height: 1})
	}

	return drawShaderSafe(probe.img, 1, 1, sh, probe.uniforms)
}

// checkedCompiler はコンパイルしてから試し描きし、予約 uniform の型違いをコンパイル失敗として返す。
// shadermgr（起動時の読み込みとファイル監視の再読込）に渡す。どちらもゲームループの側で呼ばれる。
func checkedCompiler(src []byte) (shadermgr.Shader, error) {
	sh, err := ebiten.NewShader(src)
	if err != nil {
		return nil, err
	}
	if err := validateShader(sh); err != nil {
		sh.Dispose()

		return nil, err
	}

	return sh, nil
}
