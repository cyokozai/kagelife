// Package uniform は、シェーダーに毎フレーム渡す Uniform の map を組み立てる（PRD の UniformBuilder）。
// ebiten には依存しない。map とスライスは Builder が持ち、毎フレーム使い回す。
package uniform

// Input は 1 フレーム分の Uniform の値。Random は呼び出し側が乱数を引いて渡す（テストで固定するため）。
type Input struct {
	Time             float32 // 起動からの秒数
	Width, Height    int     // 描画先の画素数
	Beat             float32 // 拍の位相（0〜1）
	CursorX, CursorY int     // カーソルの位置（画素）
	Frame            int     // フレーム番号
	Random           float32 // 0〜1 の乱数
}

// Builder は Uniform の map を使い回して組み立てる。ゲームループのゴルーチンからだけ使うこと。
type Builder struct {
	m          map[string]any
	resolution []float32
	cursor     []float32
}

// New は Builder を作る。
func New() *Builder {
	b := &Builder{
		resolution: make([]float32, 2),
		cursor:     make([]float32, 2),
	}
	b.m = map[string]any{
		"Resolution": b.resolution,
		"Cursor":     b.cursor,
	}

	return b
}

// Build は in の値で map を更新して返す。返す map とスライスは毎回同じもので、次の Build で上書きされる。
func (b *Builder) Build(in Input) map[string]any {
	b.resolution[0], b.resolution[1] = float32(in.Width), float32(in.Height)
	b.cursor[0], b.cursor[1] = float32(in.CursorX), float32(in.CursorY)
	b.m["Time"] = in.Time
	b.m["Beat"] = in.Beat
	b.m["Frame"] = in.Frame
	b.m["Random"] = in.Random

	return b.m
}
