package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"image/color"
	"log"
	"math/rand/v2"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cyokozai/kagelife/internal/control"
	"github.com/cyokozai/kagelife/internal/filewatcher"
	"github.com/cyokozai/kagelife/internal/shadermgr"
	"github.com/cyokozai/kagelife/internal/tempo"

	"github.com/hajimehoshi/ebiten/v2"
	"golang.org/x/image/font/gofont/goregular"
	// "github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

const (
	screenWidth  = 640
	screenHeight = 480
)

type Game struct {
	sm      *shadermgr.Manager
	watcher *filewatcher.Watcher
	tapper  *tempo.Tapper
	startAt time.Time
	showHUD bool
	hudSize float64
	cursor  []float32
	frame   int
	eng     *control.Engine // 制御口から名前で操作する VJ 状態（sm と tapper を共有）
	queue   *control.Queue  // 制御口からの処理。Update で毎 tick 読み切る
	quit    atomic.Bool     // シグナル受信で立て、次の Update で終了する
}

var hudFaceSource *text.GoTextFaceSource

func ebitenCompiler(src []byte) (shadermgr.Shader, error) {
	return ebiten.NewShader(src)
}

// NewGame は shaderDir（絶対パス）のシェーダを読み込んだ Game を作る。
func NewGame(shaderDir string, queue *control.Queue) (*Game, error) {
	g := &Game{
		sm:      shadermgr.New(ebitenCompiler),
		tapper:  tempo.New(2*time.Second, 8),
		startAt: time.Now(),
		showHUD: true,
		hudSize: 20,
		queue:   queue,
	}
	g.eng = &control.Engine{
		SM:        g.sm,
		Tapper:    g.tapper,
		ShaderDir: shaderDir,
		FPS:       ebiten.ActualFPS,
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
	if g.quit.Load() {
		return ebiten.Termination
	}

	select {
	case path := <-g.watcher.Events:
		src, err := os.ReadFile(path)
		if err != nil {
			log.Printf("error: failed to read %s: %v", path, err)
			g.eng.RecordError(control.NameFromPath(path), err.Error())

			break
		}

		if err := g.eng.ReloadFile(path, src); err != nil {
			log.Printf("error: failed to reload %s: %v", path, err)
		} else {
			log.Printf("reloaded shader: %s", path)
		}
	default:
	}

	// 制御口からの処理（状態の読み取り・切替・差し替え）はここでだけ実行する
	g.queue.Drain(g.eng)

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
		g.tapper.Tap(time.Now())
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

	g.sm.Tick(g.tapper.BPM())

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
	g.eng.Resolution = [2]int{w, h}
	elapsed := float32(time.Since(g.startAt).Seconds())

	uniforms := map[string]any{
		"Time":       elapsed,
		"Resolution": []float32{float32(w), float32(h)},
		"Beat":       g.tapper.Phase(time.Now()),
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
		msg := fmt.Sprintf("BPM: %.1f  FadeBeats: %.1f  Mix: %.2f", g.tapper.BPM(), g.sm.FadeBeats(), g.sm.MixRatio())
		if le := g.eng.LastError(); le != nil {
			msg += "\nERROR: " + le.Message
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

	shaderDirFlag := flag.String("shaders", "shaders", "シェーダ（*.kage）を置くディレクトリ")
	controlAddr := flag.String("control-addr", "127.0.0.1:0", "制御口の待受アドレス（ループバックのみ。ポート 0 は空きポート）")
	flag.Parse()

	if err := run(*shaderDirFlag, *controlAddr); err != nil {
		log.Fatal(err)
	}
}

// run は制御口を起動してからゲームを回し、終了時に制御口を閉じて発見ファイルを消す。
func run(shaderDirArg, controlAddr string) error {
	shaderDir, err := filepath.Abs(shaderDirArg)
	if err != nil {
		return fmt.Errorf("shader dir: %w", err)
	}

	queue := control.NewQueue(64)
	g, err := NewGame(shaderDir, queue)
	if err != nil {
		return err
	}
	defer g.watcher.Close()

	ctl, err := control.Start(control.Config{
		Addr:      controlAddr,
		ShaderDir: shaderDir,
		Compile:   ebitenCompiler,
		Queue:     queue,
	})
	if err != nil {
		return err
	}
	log.Printf("control: listening on %s (discovery: %s)", ctl.Addr(), ctl.DiscoveryFile())
	defer func() {
		if err := ctl.Close(); err != nil {
			log.Printf("warn: control close: %v", err)
		}
	}()

	// Ctrl+C / SIGTERM でも正常終了の経路（発見ファイルの削除）を通す
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	go func() {
		<-sig
		g.quit.Store(true)
	}()

	if err := ebiten.RunGame(g); err != nil && !errors.Is(err, ebiten.Termination) {
		return err
	}

	return nil
}
