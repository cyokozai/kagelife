package main

import (
	"bytes"
	"errors"
	"fmt"
	"image/color"
	"log"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

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
	shaderDir    = "shaders"

	// maxEventsPerFrame は 1 フレームで処理するファイル変更イベントの上限。
	// コンパイルは同期で走るため、溜まりすぎたときにフレームが長く止まらないようにする。
	maxEventsPerFrame = 16
	// hudMaxErrors は HUD に並べるエラーの最大件数。超えた分は残り件数だけを出す。
	hudMaxErrors = 5
)

type Game struct {
	sm      *shadermgr.Manager
	watcher *filewatcher.Watcher
	tapper  *tempo.Tapper
	startAt time.Time
	showHUD bool
	hudSize float64
	frame   int

	// errs はファイルごとの最新のエラー。再読み込みが成功したファイルだけを消す。
	errs map[string]error

	// 毎フレームの確保を避けるため、Uniform とそのスライスは使い回す。
	uniforms   map[string]any
	resolution []float32
	cursor     []float32

	// offscreen はクロスフェードの B 側を描く画面外の画像。画面サイズが変わったときだけ作り直す。
	offscreen *ebiten.Image
}

var hudFaceSource *text.GoTextFaceSource

func ebitenCompiler(src []byte) (shadermgr.Shader, error) {
	return ebiten.NewShader(src)
}

func NewGame() (*Game, error) {
	g := &Game{
		sm:         shadermgr.New(ebitenCompiler),
		tapper:     tempo.New(2*time.Second, 8),
		startAt:    time.Now(),
		showHUD:    true,
		hudSize:    20,
		errs:       map[string]error{},
		resolution: make([]float32, 2),
		cursor:     make([]float32, 2),
	}
	g.uniforms = map[string]any{
		"Resolution": g.resolution,
		"Cursor":     g.cursor,
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
				log.Printf("warn: skip: %v", err) // Load のエラーはパスを含む
				g.errs[path] = err
			}
		}
	}

	logShaderList(g.sm.Names())

	return g, nil
}

// drainEvents は ch に溜まっているイベントを、ブロックせずに最大 limit 件まで取り出す。
func drainEvents(ch <-chan string, limit int) []string {
	var paths []string
	for len(paths) < limit {
		select {
		case path, ok := <-ch:
			if !ok {
				return paths
			}
			paths = append(paths, path)
		default:
			return paths
		}
	}

	return paths
}

// processEvents は変更のあったファイルを読み直して reload に渡し、結果を errs にファイルごとに記録する。
// 成功したファイルのエラーだけを消し、他のファイルのエラーには触れない。
func processEvents(
	paths []string,
	read func(string) ([]byte, error),
	reload func(string, []byte) error,
	errs map[string]error,
) {
	for _, path := range paths {
		src, err := read(path)
		if err != nil {
			log.Printf("error: %v", err) // read のエラー（*fs.PathError）はパスを含む
			errs[path] = err

			continue
		}

		if err := reload(path, src); err != nil {
			log.Printf("error: %v", err) // reload のエラーはパスを含む
			errs[path] = err

			continue
		}

		log.Printf("reloaded shader: %s", path)
		delete(errs, path)
	}
}

// errorLines は HUD に出すエラー行を、ファイル名順に最大 limit 件まで組み立てる。
// エラー文にはパスが含まれるので、ファイル名は前に付けない。
func errorLines(errs map[string]error, limit int) []string {
	if len(errs) == 0 {
		return nil
	}

	paths := make([]string, 0, len(errs))
	for path := range errs {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	lines := make([]string, 0, min(len(paths), limit)+1)
	for _, path := range paths[:min(len(paths), limit)] {
		lines = append(lines, "ERROR: "+errs[path].Error())
	}
	if rest := len(paths) - limit; rest > 0 {
		lines = append(lines, fmt.Sprintf("... and %d more errors", rest))
	}

	return lines
}

// hudText は HUD に描く文字列を組み立てる。
func hudText(bpm float64, fadeBeats, mix float32, errs map[string]error) string {
	lines := []string{fmt.Sprintf("BPM: %.1f  FadeBeats: %.1f  Mix: %.2f", bpm, fadeBeats, mix)}
	lines = append(lines, errorLines(errs, hudMaxErrors)...)

	return strings.Join(lines, "\n")
}

// shaderListLines はシェーダーの一覧を、キーの 1〜9 と揃えた 1 始まりの番号で組み立てる。
func shaderListLines(names []string) []string {
	lines := make([]string, len(names))
	for i, name := range names {
		lines[i] = fmt.Sprintf("shader %d: %s", i+1, name)
	}

	return lines
}

func logShaderList(names []string) {
	for _, line := range shaderListLines(names) {
		log.Print(line)
	}
}

func (g *Game) Update() error {
	if paths := drainEvents(g.watcher.Events, maxEventsPerFrame); len(paths) > 0 {
		before := g.sm.Len()
		processEvents(paths, os.ReadFile, g.sm.Reload, g.errs)
		if g.sm.Len() > before {
			logShaderList(g.sm.Names())
		}
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
	g.cursor[0], g.cursor[1] = float32(cx), float32(cy)

	g.frame++

	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	if shader, ok := g.sm.Active().(*ebiten.Shader); ok && shader != nil {
		g.drawShaders(screen, shader)
	}

	if g.showHUD {
		g.drawHUD(screen)
	}
}

// drawShaders はアクティブなシェーダー A を描き、フェード中なら B を画面外に描いてから重ねる。
// 画面は既定で毎フレーム消去されるので、ここでは Clear しない。
func (g *Game) drawShaders(screen *ebiten.Image, shader *ebiten.Shader) {
	w, h := screen.Bounds().Dx(), screen.Bounds().Dy()

	g.resolution[0], g.resolution[1] = float32(w), float32(h)
	g.uniforms["Time"] = float32(time.Since(g.startAt).Seconds())
	g.uniforms["Beat"] = g.tapper.Phase(time.Now())
	g.uniforms["Frame"] = g.frame
	g.uniforms["Random"] = rand.Float32()

	opA := &ebiten.DrawRectShaderOptions{Uniforms: g.uniforms}
	screen.DrawRectShader(w, h, shader, opA)

	if !g.sm.Fading() {
		return
	}
	shaderB, ok := g.sm.ActiveB().(*ebiten.Shader)
	if !ok || shaderB == nil {
		return
	}

	// DrawRectShaderOptions.ColorScale はシェーダーの color 引数に渡るだけで、
	// color を使わないシェーダーでは効かない。B を画面外に描き、DrawImage の ColorScale で薄めて重ねる。
	// ScaleAlpha は RGBA の 4 成分に掛かるので、乗算済みアルファのまま正しく補間される。
	off := g.offscreenFor(w, h)
	off.Clear()
	opB := &ebiten.DrawRectShaderOptions{Uniforms: g.uniforms}
	off.DrawRectShader(w, h, shaderB, opB)

	opMix := &ebiten.DrawImageOptions{}
	opMix.ColorScale.ScaleAlpha(g.sm.MixRatio())
	screen.DrawImage(off, opMix)
}

// offscreenFor は w x h の画面外の画像を返す。サイズが変わったときだけ作り直し、古いものは解放する。
func (g *Game) offscreenFor(w, h int) *ebiten.Image {
	if g.offscreen != nil {
		b := g.offscreen.Bounds()
		if b.Dx() == w && b.Dy() == h {
			return g.offscreen
		}
		g.offscreen.Deallocate()
	}
	g.offscreen = ebiten.NewImage(w, h)

	return g.offscreen
}

// drawHUD は状態とエラーを左上に描く。LayoutF がデバイスピクセルの論理画面を返すため、
// 文字サイズと位置はデバイスの拡大率に合わせる。
func (g *Game) drawHUD(screen *ebiten.Image) {
	s := ebiten.Monitor().DeviceScaleFactor()
	msg := hudText(g.tapper.BPM(), g.sm.FadeBeats(), g.sm.MixRatio(), g.errs)
	face := &text.GoTextFace{Source: hudFaceSource, Size: g.hudSize * s}
	// text/v2 は LineSpacing が 0 だと改行しても行が重なるので、複数行のエラーに備えて指定する。
	lineSpacing := face.Size * 1.2

	shadow := &text.DrawOptions{}
	shadow.GeoM.Translate(10*s, 10*s)
	shadow.LineSpacing = lineSpacing
	shadow.ColorScale.ScaleWithColor(color.RGBA{0, 0, 0, 200})
	text.Draw(screen, msg, face, shadow)

	op := &text.DrawOptions{}
	op.GeoM.Translate(8*s, 8*s)
	op.LineSpacing = lineSpacing
	op.ColorScale.ScaleWithColor(color.White)
	text.Draw(screen, msg, face, op)
}

func (g *Game) LayoutF(outsideWidth, outsideHeight float64) (float64, float64) {
	s := ebiten.Monitor().DeviceScaleFactor()

	return outsideWidth * s, outsideHeight * s
}

// Layout は LayoutFer（LayoutF）を実装しているため Ebitengine からは呼ばれない。ebiten.Game を満たすためだけにある。
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

	g, err := NewGame()
	if err != nil {
		log.Fatal(err)
	}
	if err := ebiten.RunGame(g); err != nil && !errors.Is(err, ebiten.Termination) {
		log.Fatal(err)
	}
}
