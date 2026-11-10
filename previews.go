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

// disablePreviews は -previews=false の処理。描く側の状態を外し、制御口の features から previews を消す。
// 制御口には g.previews（nil）を渡すので、GET /v1/previews は 404 になる。
func (g *Game) disablePreviews() {
	g.previews = nil
	g.eng.NoPreviews = true
}

// drawPreviews は、要求が来ていて間隔も空いていれば、キー 1〜9 のシェーダーを描いて読み出し、Submit する。
// in は本番の uniform、screenW x screenH は画面の大きさ。
// ReadPixels は積んである描画命令をすべて流して待つので、Draw の先頭（本番の描画を積む前）で呼ぶ。
func (g *Game) drawPreviews(in uniform.Input, screenW, screenH int) {
	if g.previews == nil || !g.previews.Due() {
		return
	}
	slots := g.renderPreviewAtlas(in, screenW, screenH)

	// GPU からの読み出しは 1 プレビューフレームにつき 1 回
	g.preview.atlas.ReadPixels(g.preview.pix)
	g.previews.Submit(g.preview.pix, slots)
}

// newUnmanagedImage は Ebitengine の内部のテクスチャアトラスに載せない w x h の画像を作る。
func newUnmanagedImage(w, h int) *ebiten.Image {
	return ebiten.NewImageWithOptions(image.Rect(0, 0, w, h), &ebiten.NewImageOptions{Unmanaged: true})
}

// renderPreviewAtlas はキー 1〜9 のシェーダーをセルに描いてアトラスへまとめ、キーの順のスロットを返す。
// 描くと panic するシェーダー（予約 uniform の型違い）は描かず、そのキーを CompileError にする（jpeg は null になる）。
func (g *Game) renderPreviewAtlas(in uniform.Input, screenW, screenH int) []control.PreviewSlot {
	r := &g.preview
	if r.atlas == nil {
		r.uniforms = uniform.New()
		r.atlas = newUnmanagedImage(control.PreviewAtlasSize, control.PreviewAtlasSize)
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
			r.cells[cell] = newUnmanagedImage(control.PreviewSize, control.PreviewSize)
		}
		c := r.cells[cell]
		c.Clear()
		if err := drawShaderSafe(c, control.PreviewSize, control.PreviewSize, sh, uniforms); err != nil {
			slots[i].CompileError = true

			continue
		}

		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(float64(cell%control.PreviewCols*control.PreviewSize), float64(cell/control.PreviewCols*control.PreviewSize))
		r.atlas.DrawImage(c, op)
	}

	return slots
}
