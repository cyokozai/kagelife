package main

import (
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cyokozai/kagelife/internal/filewatcher"
	"github.com/cyokozai/kagelife/internal/shadermgr"
	
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)


const (
	screenWidth  = 640
	screenHeight = 480
	shaderDir 	 = "shaders"
)


type Game struct {
	sm        *shadermgr.Manager
	watcher   *filewatcher.Watcher
	cursor 		[]float32
	startAt   time.Time
	tapTimes  []time.Time
	beatStart time.Time
	BPM			  float64
	frame 		int
	lastErr   error
}


func ebitenCompiler(src []byte) (shadermgr.Shader, error) {
	return ebiten.NewShader(src)
}


func NewGame() (*Game, error) {
	g := &Game{
		sm:      shadermgr.New(ebitenCompiler),
		startAt: time.Now(),
	}

	w, err := filewatcher.New(shaderDir)
	if err != nil {
		return nil, err
	}
	g.watcher = w

	entries, err := os.ReadDir(shaderDir)
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

	for i, name := range g.sm.Names() {
		log.Printf("loaded shader %d: %s", i, name)
	}

	return g, nil
}


func (g *Game) Update() error {
	select {
	case path := <-g.watcher.Events:
		src, err := os.ReadFile(path)
		if err != nil {
			log.Printf("error: failed to read %s: %v", path, err)
			g.lastErr = err
			
			break
		}

		err = g.sm.Reload(path, src)
		if err != nil {
			log.Printf("error: failed to reload %s: %v", path, err)
			g.lastErr = err
		} else {
			log.Printf("reloaded shader: %s", path)
			g.lastErr = nil
		}
	default:
	}

	for i := 0; i < g.sm.Len() && i < 9; i++ {
		if inpututil.IsKeyJustPressed(ebiten.Key1 + ebiten.Key(i)) {
			g.sm.Switch(i)
		}
	}

	if inpututil.IsKeyJustPressed(ebiten.KeySpace) {
		now := time.Now()
		if len(g.tapTimes) == 0 {
			g.beatStart = now
		}

		if len(g.tapTimes) >= 8 {
			g.tapTimes = append(g.tapTimes[1:], now)
		} else {
			g.tapTimes = append(g.tapTimes, now)
		}

		if len(g.tapTimes) >= 2 {
			g.BPM = calcBPM(g.tapTimes)
		}
	}

	if inpututil.IsKeyJustPressed(ebiten.KeyF) {
		ebiten.SetFullscreen(!ebiten.IsFullscreen())
	}

	cx, cy := ebiten.CursorPosition()
	g.cursor = []float32{float32(cx), float32(cy)}

	g.frame++

	return nil
}


func (g *Game) Draw(screen *ebiten.Image) {
	shader, ok := g.sm.Active().(*ebiten.Shader)
	if !ok || shader == nil {
		return
	}

	screen.Clear()

	w, h := screen.Bounds().Dx(), screen.Bounds().Dy()
	elapsed := float32(time.Since(g.startAt).Seconds())

	op := &ebiten.DrawRectShaderOptions{}
	op.Uniforms = map[string]any{
		"Time":       elapsed,
		"Resolution": []float32{float32(w), float32(h)},
		"Beat":       beatPhase(g.BPM, g.beatStart),
		"Cursor":     g.cursor,
		"Frame":			g.frame,
		"Random": 		rand.Float32(),
	}
	screen.DrawRectShader(w, h, shader, op)
	
	msg := fmt.Sprintf("BPM: %.1f", g.BPM)
	if g.lastErr != nil {
		msg += "\nERROR: " + g.lastErr.Error()
	}
	ebitenutil.DebugPrint(screen, msg)
}


func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return screenWidth, screenHeight
}


func (g *Game) LayoutF(outsideWidth, outsideHeight float64) (float64, float64) {
	s := ebiten.DeviceScaleFactor()

	return outsideWidth * s, outsideHeight * s
}


func calcBPM(taps []time.Time) float64 {
	sum_intervals := 0.0
	average       := 0.0
	for i := 1; i < len(taps); i++ {
		sum_intervals += taps[i].Sub(taps[i-1]).Seconds()
	}
	
	if len(taps) > 1 && sum_intervals > 0 {
		average = sum_intervals / float64(len(taps)-1)

		return 60.0 / average
	} else {
		return 0.0
	}
}

func beatPhase(bpm float64, beatStart time.Time) float32 {
	if bpm <= 0 {
		return 0.0
	}

	beatDuration := 60.0 / bpm
	elapsed      := time.Since(beatStart).Seconds()
	phase 			 := math.Mod(elapsed, beatDuration) / beatDuration

	return float32(phase)
}

func main() {
	ebiten.SetWindowSize(screenWidth, screenHeight)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetWindowTitle("KageLife")

	g, err := NewGame()
	if err != nil {
		log.Fatal(err)
	}
	opts := &ebiten.RunGameOptions{
		// ScreenTransparent: true,
	}
	if err := ebiten.RunGameWithOptions(g, opts); err != nil && !errors.Is(err, ebiten.Termination) {
		log.Fatal(err)
	}
}
