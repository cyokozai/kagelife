package main

import (
	"image"
	"math"
	"strconv"

	"github.com/cyokozai/kagelife/internal/control"
	"github.com/cyokozai/kagelife/internal/uniform"

	"github.com/hajimehoshi/ebiten/v2"
)

// GET /v1/previews のプレビューを描く（Stream Deck のキーの液晶用）。
// 要求が来ている間だけ、最大 10 fps で、キー 1〜9 のシェーダーを 72x72 のセルに描いて
// 216x216 のアトラスにまとめ、1 回だけ読み出して control.Previews に渡す（JPEG はゲームループの外で作る）。

// previewTarget はプレビューに描くキー 1 つ分。idx は shadermgr のスロットの位置。
type previewTarget struct {
	slot control.PreviewSlot
	idx  int
}

// previewTargets はキー 1〜9 に割り当たっているスロットを、キーの順に返す（keySlots と同じ対応）。
func (g *Game) previewTargets() []previewTarget {
	paths := g.sm.Names()
	out := make([]previewTarget, 0, numKeys)
	for k, idx := range keySlots(paths, g.llmSlots) {
		if idx < 0 {
			continue
		}
		out = append(out, previewTarget{
			slot: control.PreviewSlot{
				Key:          strconv.Itoa(k + 1),
				Name:         control.NameFromPath(paths[idx]),
				CompileError: !g.sm.Usable(idx),
			},
			idx: idx,
		})
	}

	return out
}

// previewInput は本番の uniform（in）から、プレビューのセルに渡す uniform を作る。
// Time・Beat・Frame・Random は本番と同じ。Resolution はセルの大きさにし、
// Cursor は画面（screenW x screenH）の座標 cursorX, cursorY をセルの大きさへ縮める。
func previewInput(in uniform.Input, screenW, screenH, cursorX, cursorY int) uniform.Input {
	out := in
	out.Width, out.Height = control.PreviewSize, control.PreviewSize
	out.CursorX, out.CursorY = 0, 0
	if screenW > 0 && screenH > 0 {
		out.CursorX = int(math.Round(float64(cursorX) * control.PreviewSize / float64(screenW)))
		out.CursorY = int(math.Round(float64(cursorY) * control.PreviewSize / float64(screenH)))
	}

	return out
}

// previewRenderer はプレビューを描く画面外の画像と、読み出し先の画素を持つ。ゲームループからだけ使う。
type previewRenderer struct {
	uniforms *uniform.Builder
	// cells はセルごとの 72x72 の画像。シェーダーは dstPos / Resolution で座標を出すので、
	// アトラスの途中に直接描くと座標がずれる。原点が 0 の画像に描いてからアトラスへ写す。
	// 小さな画像は Ebitengine の内部のテクスチャアトラスに詰められ、dstPos に詰めた位置が足される
	// （Xvfb で 01_uv の x が 1 に張り付いた）。Unmanaged で作り、内部のアトラスに載せない。
	cells [numKeys]*ebiten.Image
	atlas *ebiten.Image
	pix   []byte
}

// drawPreviews は、要求が来ていて間隔も空いていれば、キー 1〜9 のシェーダーを描いて Submit する。
// in は本番の uniform、screenW x screenH は画面の大きさ。
func (g *Game) drawPreviews(in uniform.Input, screenW, screenH int) {
	if g.previews == nil || !g.previews.Due() {
		return
	}
	r := &g.preview
	if r.atlas == nil {
		r.uniforms = uniform.New()
		r.atlas = ebiten.NewImage(control.PreviewAtlasSize, control.PreviewAtlasSize)
		r.pix = make([]byte, control.PreviewAtlasSize*control.PreviewAtlasSize*4)
	}
	r.atlas.Clear()

	uniforms := r.uniforms.Build(previewInput(in, screenW, screenH, g.cursorX, g.cursorY))
	targets := g.previewTargets()
	slots := make([]control.PreviewSlot, len(targets))
	for i, t := range targets {
		slots[i] = t.slot
		sh, ok := g.sm.ShaderAt(t.idx).(*ebiten.Shader)
		if !ok || sh == nil {
			continue
		}
		cell := int(t.slot.Key[0] - '1')
		if r.cells[cell] == nil {
			r.cells[cell] = ebiten.NewImageWithOptions(
				image.Rect(0, 0, control.PreviewSize, control.PreviewSize),
				&ebiten.NewImageOptions{Unmanaged: true},
			)
		}
		c := r.cells[cell]
		c.Clear()
		c.DrawRectShader(control.PreviewSize, control.PreviewSize, sh, &ebiten.DrawRectShaderOptions{Uniforms: uniforms})

		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(float64(cell%control.PreviewCols*control.PreviewSize), float64(cell/control.PreviewCols*control.PreviewSize))
		r.atlas.DrawImage(c, op)
	}

	// GPU からの読み出しは 1 プレビューフレームにつき 1 回
	r.atlas.ReadPixels(r.pix)
	g.previews.Submit(r.pix, slots)
}
