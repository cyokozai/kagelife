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
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cyokozai/kagelife/internal/control"
	"github.com/cyokozai/kagelife/internal/filewatcher"
	"github.com/cyokozai/kagelife/internal/shadermgr"
	"github.com/cyokozai/kagelife/internal/tempo"
	"github.com/cyokozai/kagelife/internal/uniform"

	"github.com/hajimehoshi/ebiten/v2"
	"golang.org/x/image/font/gofont/goregular"
	// "github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

const (
	screenWidth  = 640
	screenHeight = 480

	// maxEventsPerFrame は 1 フレームで処理するファイル変更イベントの上限。
	// コンパイルは同期で走るため、溜まりすぎたときにフレームが長く止まらないようにする。
	maxEventsPerFrame = 16
	// hudMaxErrors は HUD に並べるエラーの最大件数。超えた分は残り件数だけを出す。
	hudMaxErrors = 5

	appName = "KageLife"
)

// version はビルド時に Makefile の -ldflags "-X main.version=..." で上書きされる。
var version = "dev"

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

	// 毎フレームの確保を避けるため、Uniform の map とスライスは Builder の中で使い回す。
	uniforms *uniform.Builder
	cursorX  int
	cursorY  int

	// title はウィンドウタイトル。名前が変わったときだけ SetWindowTitle を呼ぶ。
	title titleSetter

	// offscreen はクロスフェードの B 側を描く画面外の画像。画面サイズが変わったときだけ作り直す。
	offscreen *ebiten.Image

	eng   *control.Engine // 制御口から名前で操作する VJ 状態（sm と tapper を共有）
	queue *control.Queue  // 制御口からの処理。Update で毎 tick 読み切る
	quit  atomic.Bool     // シグナル受信で立て、次の Update で終了する
}

var hudFaceSource *text.GoTextFaceSource

func ebitenCompiler(src []byte) (shadermgr.Shader, error) {
	return ebiten.NewShader(src)
}

// NewGame は shaderDir（絶対パス）のシェーダーを読み込んだ Game を作る。
func NewGame(shaderDir string, queue *control.Queue) (*Game, error) {
	g := &Game{
		sm:       shadermgr.New(ebitenCompiler),
		tapper:   tempo.New(2*time.Second, 8),
		startAt:  time.Now(),
		showHUD:  true,
		hudSize:  20,
		errs:     map[string]error{},
		uniforms: uniform.New(),
		title:    titleSetter{set: ebiten.SetWindowTitle},
		queue:    queue,
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

// processRemoved は削除されたファイルを remove に渡し、そのファイルのエラーを errs から消す。
// paths は drainEvents で取り出したもので、閉じたチャネルの零値（空文字列）は含まない。
func processRemoved(paths []string, remove func(string), errs map[string]error) {
	for _, path := range paths {
		remove(path)
		delete(errs, path)
		log.Printf("removed shader: %s", path)
	}
}

// readShader は os.ReadFile を包み、読み込みに失敗したら制御口の last_error にも記録する。
// 再コンパイルの失敗は Engine.ReloadFile が自分で記録するが、読み込みの失敗はここを通らないと残らない。
func (g *Game) readShader(path string) ([]byte, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		g.eng.RecordError(control.NameFromPath(path), err.Error())

		return nil, err
	}

	return src, nil
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
// measured が false（タップも set_bpm もしていない既定値）なら、BPM に (default) を付ける。
func hudText(bpm float64, measured bool, fadeBeats, mix float32, errs map[string]error) string {
	bpmText := fmt.Sprintf("%.1f", bpm)
	if !measured {
		bpmText += " (default)"
	}
	lines := []string{fmt.Sprintf("BPM: %s  FadeBeats: %.1f  Mix: %.2f", bpmText, fadeBeats, mix)}
	lines = append(lines, errorLines(errs, hudMaxErrors)...)

	return strings.Join(lines, "\n")
}

// windowTitle はウィンドウタイトルを組み立てる。active が空ならアプリ名だけ、
// フェード中でフェード先があれば「A → B」。
func windowTitle(active, target string, fading bool) string {
	if active == "" {
		return appName
	}
	if fading && target != "" {
		return appName + " - " + active + " → " + target
	}

	return appName + " - " + active
}

// titleSetter は前回と違うタイトルのときだけ set を呼ぶ。
type titleSetter struct {
	cur string
	set func(string)
}

func (t *titleSetter) update(title string) {
	if title == t.cur {
		return
	}
	t.cur = title
	t.set(title)
}

// currentTitle はシェーダーマネージャの状態からウィンドウタイトルを組み立てる。
func (g *Game) currentTitle() string {
	names := g.sm.Names()
	nameAt := func(i int) string {
		if i < 0 || i >= len(names) {
			return ""
		}

		return control.NameFromPath(names[i])
	}

	return windowTitle(nameAt(g.sm.ActiveIndex()), nameAt(g.sm.FadeTargetIndex()), g.sm.Fading())
}

// versionString は -version で出す文字列。
func versionString() string {
	return "kagelife " + version
}

// options はコマンドラインの指定。
type options struct {
	shaderDir   string
	controlAddr string
	version     bool
}

// parseFlags は args（プログラム名を除く）を解釈する。
func parseFlags(args []string) (options, error) {
	var opts options
	fs := flag.NewFlagSet("kagelife", flag.ContinueOnError)
	fs.StringVar(&opts.shaderDir, "shaders", "shaders", "シェーダー（*.kage）を置くディレクトリ")
	fs.StringVar(&opts.controlAddr, "control-addr", "127.0.0.1:0", "制御口の待受アドレス（ループバックのみ。ポート 0 は空きポート）")
	fs.BoolVar(&opts.version, "version", false, "バージョンを出力して終了する")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}

	return opts, nil
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
	if g.quit.Load() {
		return ebiten.Termination
	}

	// drainEvents は v, ok := <-ch で close を確かめるので、Close 後の零値は処理しない。
	if paths := drainEvents(g.watcher.Events, maxEventsPerFrame); len(paths) > 0 {
		before := g.sm.Len()
		// reload を Engine 経由にして、HUD の errs と制御口の last_error の両方を更新する。
		processEvents(paths, g.readShader, g.eng.ReloadFile, g.errs)
		if g.sm.Len() > before {
			logShaderList(g.sm.Names())
		}
	}

	// 削除も Engine 経由にして、スロット・HUD の errs・制御口の last_error を揃えて消す。
	if paths := drainEvents(g.watcher.Removed, maxEventsPerFrame); len(paths) > 0 {
		processRemoved(paths, g.eng.RemoveFile, g.errs)
		logShaderList(g.sm.Names())
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

	g.cursorX, g.cursorY = ebiten.CursorPosition()

	g.title.update(g.currentTitle())

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

	g.eng.Resolution = [2]int{w, h}
	now := time.Now()
	uniforms := g.uniforms.Build(uniform.Input{
		Time:    float32(now.Sub(g.startAt).Seconds()),
		Width:   w,
		Height:  h,
		Beat:    g.tapper.Phase(now),
		CursorX: g.cursorX,
		CursorY: g.cursorY,
		Frame:   g.frame,
		Random:  rand.Float32(),
	})

	opA := &ebiten.DrawRectShaderOptions{Uniforms: uniforms}
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
	opB := &ebiten.DrawRectShaderOptions{Uniforms: uniforms}
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
	msg := hudText(g.tapper.BPM(), g.tapper.Measured(), g.sm.FadeBeats(), g.sm.MixRatio(), g.errs)
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
	opts, err := parseFlags(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		os.Exit(2) // FlagSet が使い方を出力済み
	}

	// -version はウィンドウを開かずに終わる（ディスプレイの無い環境でも動くように、ウィンドウ設定より前で返す）
	if opts.version {
		fmt.Println(versionString())

		return
	}

	ebiten.SetWindowSize(screenWidth, screenHeight)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetWindowTitle(appName)

	if err := run(opts.shaderDir, opts.controlAddr); err != nil {
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
