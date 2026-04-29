package main

import (
	"log"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/cyokozai/kagelive/internal/shadermgr"
)

const (
	screenWidth  = 640
	screenHeight = 480
)

// ebitenCompiler は ebiten.NewShader を shadermgr.ShaderCompiler 型に変換する。
func ebitenCompiler(src []byte) (shadermgr.Shader, error) {
	return ebiten.NewShader(src)
}

// Game は Ebitengine のゲームループを実装する。
type Game struct {
	sm      *shadermgr.Manager
	startAt time.Time
}

func NewGame() (*Game, error) {
	g := &Game{
		sm:      shadermgr.New(ebitenCompiler),
		startAt: time.Now(),
	}
	// 起動時に shaders/example.kage を読み込む
	if err := g.sm.Load("shaders/example.kage"); err != nil {
		return nil, err
	}
	return g, nil
}

// Update は毎フレーム呼ばれるロジック更新。
func (g *Game) Update() error {
	return nil
}

// Draw は毎フレーム呼ばれる描画処理。
func (g *Game) Draw(screen *ebiten.Image) {
	shader, ok := g.sm.Active().(*ebiten.Shader)
	if !ok || shader == nil {
		return
	}

	w, h := screen.Bounds().Dx(), screen.Bounds().Dy()
	elapsed := float32(time.Since(g.startAt).Seconds())

	op := &ebiten.DrawRectShaderOptions{}
	op.Uniforms = map[string]any{
		"Time":       elapsed,
		"Resolution": []float32{float32(w), float32(h)},
	}
	screen.DrawRectShader(w, h, shader, op)
}

// Layout はウィンドウサイズを返す。
func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return screenWidth, screenHeight
}

func main() {
	ebiten.SetWindowSize(screenWidth, screenHeight)
	ebiten.SetWindowTitle("KageLive")

	g, err := NewGame()
	if err != nil {
		log.Fatal(err)
	}

	if err := ebiten.RunGame(g); err != nil {
		log.Fatal(err)
	}
}
