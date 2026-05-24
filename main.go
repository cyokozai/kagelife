package main

import (
	"errors"
	"log"
	"os"
	"strings"
	"time"

	"github.com/cyokozai/kagelive/internal/filewatcher"
	"github.com/cyokozai/kagelive/internal/shadermgr"
	"github.com/hajimehoshi/ebiten/v2"
)

const (
	screenWidth  = 640
	screenHeight = 480
	shaderDir 	 = "shaders"
)


type Game struct {
	sm      *shadermgr.Manager
	watcher *filewatcher.Watcher
	startAt time.Time
}


func ebitenCompiler(src []byte) (shadermgr.Shader, error) {
	return ebiten.NewShader(src)
}


func NewGame() (*Game, error) {
	g := &Game{
		sm:      shadermgr.New(ebitenCompiler),
		startAt: time.Now(),
	}

	entries, err := os.ReadDir("shaders")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".kage") {
			path := filepath.Join(shaderDir, e.Name())
			if err := g.sm.Load(path); err != nil {
        log.Printf("warn: skip %s: %v", path, err)
			}
		}
	}

	return g, nil
}


func (g *Game) Update() error {
	select {
	case path := <-g.watcher.Events:
		src, err := os.ReadFile(path)
		if err != nil {
			log.Printf("error: failed to read %s: %v", path, err)
			break
		}

		err := g.sm.Reload(path, src);
		if err != nil {
			log.Printf("error: failed to reload %s: %v", path, err)
		}
	default:
	}

	for i := 0; i < g.sm.Len(); i++ {
		if ebiten.IsKeyPressed(ebiten.Key0 + ebiten.Key(i)) {
			g.sm.Switch(i)
		}
	}

	return nil
}


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
	defer g.watcher.Close()

	if err := ebiten.RunGame(g); err != nil && !errors.Is(err, ebiten.Termination) {
		log.Fatal(err)
	}
}
