package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"time"
	"image/color"

	"github.com/cyokozai/kagelife/internal/filewatcher"
	"github.com/cyokozai/kagelife/internal/shadermgr"

	"golang.org/x/image/font/gofont/goregular"
	"github.com/hajimehoshi/ebiten/v2"
	// "github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)


const (
	screenWidth  = 640
	screenHeight = 480
	shaderDir 	 = "shaders"
)
 

type Game struct {
	sm        *shadermgr.Manager
	watcher   *filewatcher.Watcher
	startAt   time.Time
	beatStart time.Time
	tapTimes  []time.Time
	showHUD 	bool
  hudSize		float64
	BPM			  float64
	cursor    []float32
	frame     int
	lastErr   error
}


var hudFaceSource *text.GoTextFaceSource


func ebitenCompiler(src []byte) (shadermgr.Shader, error) {
	return ebiten.NewShader(src)
}


func NewGame() (*Game, error) {
	g := &Game{
		sm:      shadermgr.New(ebitenCompiler),
		startAt: time.Now(),
		showHUD: true,
		hudSize: 20,
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

	shift := ebiten.IsKeyPressed(ebiten.KeyShiftLeft) ||
		ebiten.IsKeyPressed(ebiten.KeyShiftRight)

	for i := 0; i < g.sm.Len() && i < 9; i++ {
		if inpututil.IsKeyJustPressed(ebiten.Key1 + ebiten.Key(i)) {
			if shift {
				g.sm.BeginFade(i)
			} else {
				g.sm.Switch(i)
			}
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

	if inpututil.IsKeyJustPressed(ebiten.KeyBracketLeft) {
    g.sm.DecFadeBeats()
  }
  
  if inpututil.IsKeyJustPressed(ebiten.KeyBracketRight) {
    g.sm.IncFadeBeats()
  }

  if inpututil.IsKeyJustPressed(ebiten.KeyH) {
  	g.showHUD = !g.showHUD
  }

  g.sm.Tick(g.BPM)

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
	
	uniforms := map[string]any{
		"Time":       elapsed,
		"Resolution": []float32{float32(w), float32(h)},
		"Beat":       beatPhase(g.BPM, g.beatStart),
    "Cursor":     g.cursor,
    "Frame":      g.frame,
    "Random":     rand.Float32(),
	}
	opA := &ebiten.DrawRectShaderOptions{Uniforms: uniforms}
	screen.DrawRectShader(w, h, shader, opA)

	if g.sm.Fading() {
		if shaderB, ok := g.sm.ActiveB().(*ebiten.Shader); ok && shaderB != nil {
			opB := &ebiten.DrawRectShaderOptions{Uniforms: uniforms}
      opB.ColorScale.ScaleAlpha(g.sm.MixRatio())
      screen.DrawRectShader(w, h, shaderB, opB)
		}
	}

	if g.showHUD {
		msg := fmt.Sprintf("BPM: %.1f  FadeBeats: %.1f  Mix: %.2f", g.BPM, g.sm.FadeBeats(), g.sm.MixRatio())
		if g.lastErr != nil {
			msg += "\nERROR: " + g.lastErr.Error()
		}
		
    face := &text.GoTextFace{Source: hudFaceSource, Size: g.hudSize}
    shadow := &text.DrawOptions{}
    shadow.GeoM.Translate(10, 10)
    shadow.ColorScale.ScaleWithColor(color.RGBA{0, 0, 0, 200})
    text.Draw(screen, msg, face, shadow)

    op := &text.DrawOptions{}
    op.GeoM.Translate(8, 8)
    op.ColorScale.ScaleWithColor(color.White)
    text.Draw(screen, msg, face, op)
	}
}


func (g *Game) LayoutF(outsideWidth, outsideHeight float64) (float64, float64) {
	s := ebiten.Monitor().DeviceScaleFactor()
	
	return outsideWidth * s, outsideHeight * s
}


func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return screenWidth, screenHeight
}


func calcBPM(taps []time.Time) float64 {
  if len(taps) < 2 {
    return 0.0
  }
  
  var sum float64
  for i := 1; i < len(taps); i++ {
    sum += taps[i].Sub(taps[i-1]).Seconds()
  }
  if sum <= 0 {
    return 0.0
  }
  
  return 60.0 / (sum / float64(len(taps)-1))
}


func beatPhase(bpm float64, beatStart time.Time) float32 {
	if bpm <= 0 {
		return 0.0
	}

	beatDuration := 60.0 / bpm
	elapsed := time.Since(beatStart).Seconds()
	phase := math.Mod(elapsed, beatDuration) / beatDuration

	return float32(phase)
}


func init() {
	s, err := text.NewGoTextFaceSource(bytes.NewReader(goregular.TTF))
	if err != nil {
		log.Fatal(err)
	}
	hudFaceSource = s
}


func main() {
	ebiten.SetWindowSize(screenWidth, screenHeight)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetWindowTitle("KageLife")

	g, err := NewGame()
	if err != nil {
		log.Fatal(err)
	}
	if err := ebiten.RunGame(g); err != nil && !errors.Is(err, ebiten.Termination) {
    log.Fatal(err)
  }
}
