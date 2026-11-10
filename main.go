package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"image/color"
	"io"
	"log"
	"math/rand/v2"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
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

	// llmSlots は -llm-slots の予約名（nil なら無効）。キー 8・9 に割り当て、キー 0 で予約名以外へ戻る。
	llmSlots []string
	back     backKey

	eng   *control.Engine // 制御口から名前で操作する VJ 状態（sm と tapper を共有）
	queue *control.Queue  // 制御口からの処理。Update で毎 tick 読み切る
	quit  atomic.Bool     // シグナル受信で立て、次の Update で終了する

	// avOffset は -av-offset。映像の拍（Beat）だけを、この分だけ遅らせる（FR-156 の最小版）。
	avOffset time.Duration

	// cursor は全画面のあいだカーソルを隠す。
	cursor cursorSync

	// renderScale は -render-scale。1 未満なら render に縮めて描き、拡大して画面に出す。
	renderScale float64
	render      *ebiten.Image
	// frames は直近 5 秒のフレーム時間。fps は actualFPS（テストでは差し替え）で読む。
	frames    *frameStats
	actualFPS func() float64

	// previews は GET /v1/previews の状態（制御口と共有）。要求が来ている間だけ preview で描く。
	previews *control.Previews
	preview  previewRenderer
}

var hudFaceSource *text.GoTextFaceSource

func ebitenCompiler(src []byte) (shadermgr.Shader, error) {
	return ebiten.NewShader(src)
}

// NewGame が使う監視とディレクトリの読み取り。テストで失敗を起こすために差し替える。
var (
	newShaderWatcher = filewatcher.New
	readShaderDir    = os.ReadDir
)

// NewGame は shaderDir（絶対パス）のシェーダーを読み込んだ Game を作る。
// llmSlots は検査済みの -llm-slots の予約名（nil なら無効）。
func NewGame(shaderDir string, queue *control.Queue, llmSlots []string) (*Game, error) {
	g := &Game{
		sm:       shadermgr.New(checkedCompiler), // 予約 uniform の型違いもコンパイル失敗として弾く
		tapper:   tempo.New(2*time.Second, 8),
		startAt:  time.Now(),
		showHUD:  true,
		hudSize:  20,
		errs:     map[string]error{},
		uniforms: uniform.New(),
		title:    titleSetter{set: ebiten.SetWindowTitle},
		cursor:   cursorSync{set: setEbitenCursorHidden},

		renderScale: maxRenderScale,
		frames:      newFrameStats(frameStatsWindow),
		actualFPS:   ebiten.ActualFPS,
		queue:       queue,
		llmSlots:    llmSlots,
		previews:    control.NewPreviews(),
	}
	g.eng = &control.Engine{
		SM:        g.sm,
		Tapper:    g.tapper,
		ShaderDir: shaderDir,
		FPS:       ebiten.ActualFPS,
		LLMSlots:  llmSlots,
	}
	// 制御口の POST /v1/keys と GET /v1/previews は、キーボードと同じ数字キーの対応を使う
	g.eng.PressKey = g.pressControlKey
	g.eng.PreviewSlots = g.previewSlots
	// PUT のシェーダも、ゲームループで試し描きしてから入れる（描いたときの panic を防ぐ）
	g.eng.Validate = validateShader

	w, err := newShaderWatcher(shaderDir)
	if err != nil {
		return nil, err
	}
	g.watcher = w

	entries, err := readShaderDir(shaderDir)
	if err != nil {
		w.Close() // 監視の goroutine を残さない

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

	logShaderList(g.sm.Names(), g.llmSlots)

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

// hudState は HUD に出す値。
type hudState struct {
	BPM       float64
	Measured  bool // false（タップも set_bpm もしていない既定値）なら BPM に (default) を付ける
	FadeBeats float32
	Mix       float32
	AVOffset  time.Duration // 0 なら出さない
	Perf      *perfState    // 有れば状態行の次に fps とフレーム時間の最大値の行を出す
	Errs      map[string]error
	LLMSlots  []string // 有ればキー 8・9 の行を状態行の次に出す
}

// hudText は HUD に描く文字列を組み立てる。
func hudText(st hudState) string {
	bpmText := fmt.Sprintf("%.1f", st.BPM)
	if !st.Measured {
		bpmText += " (default)"
	}
	status := fmt.Sprintf("BPM: %s  FadeBeats: %.1f  Mix: %.2f", bpmText, st.FadeBeats, st.Mix)
	if st.AVOffset != 0 {
		status += fmt.Sprintf("  AV offset: %dms", st.AVOffset.Milliseconds())
	}
	lines := []string{status}
	if st.Perf != nil {
		lines = append(lines, perfLine(*st.Perf))
	}
	if line := llmHUDLine(st.LLMSlots); line != "" {
		lines = append(lines, line)
	}
	lines = append(lines, errorLines(st.Errs, hudMaxErrors)...)

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
	// llmSlots は検査済みの -llm-slots の予約名（nil なら無効）。
	llmSlots []string
	// avOffset は -av-offset（映像の拍を遅らせる時間）。
	avOffset time.Duration
	// display は -display（1 始まり。0 は指定なし）。
	display int
	// listDisplays は -list-displays（ディスプレイの一覧を出して終了する）。
	listDisplays bool
	// fullscreen は -fullscreen（起動時から全画面）。
	fullscreen bool
	// renderScale は -render-scale（シェーダーを描く解像度の倍率。1.0 は画面のまま）。
	renderScale float64
	// previews は -previews（GET /v1/previews。false なら描かず、404 にし、features に出さない）。
	previews bool
}

// parseFlags は args（プログラム名を除く）を解釈する。エラーと使い方は標準エラーに出す。
func parseFlags(args []string) (options, error) {
	return parseFlagsTo(args, os.Stderr)
}

// parseFlagsTo は args を解釈し、エラーと使い方を out に出す。
// -llm-slots の値は control.ParseLLMSlots で検査し、違反なら（FlagSet がエラーと使い方を出して）起動エラーにする。
func parseFlagsTo(args []string, out io.Writer) (options, error) {
	opts := options{renderScale: maxRenderScale}
	fs := flag.NewFlagSet("kagelife", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&opts.shaderDir, "shaders", "shaders", "シェーダー（*.kage）を置くディレクトリ")
	fs.StringVar(&opts.controlAddr, "control-addr", "127.0.0.1:0", "制御口の待受アドレス（ループバックのみ。ポート 0 は空きポート）")
	fs.BoolVar(&opts.version, "version", false, "バージョンを出力して終了する")
	fs.Func("llm-slots",
		"LLM が書き込む予約名（カンマ区切りで 1〜2 個。例: llm_a,llm_b）。1 個目がキー 8、2 個目がキー 9。既定は無効",
		func(s string) error {
			slots, err := control.ParseLLMSlots(s)
			opts.llmSlots = slots

			return err
		})
	fs.Func("av-offset",
		"映像の拍を遅らせるミリ秒（0〜1000、既定 0）。Bluetooth スピーカーの遅れに合わせる",
		func(s string) error {
			d, err := parseAVOffset(s)
			opts.avOffset = d

			return err
		})
	fs.Func("display",
		"全画面を出すディスプレイの番号（1 始まり。-list-displays の番号）。既定は指定なし",
		func(s string) error {
			n, err := parseDisplay(s)
			opts.display = n

			return err
		})
	fs.BoolVar(&opts.listDisplays, "list-displays", false, "ディスプレイの番号・名前・大きさを出力して終了する")
	fs.BoolVar(&opts.fullscreen, "fullscreen", false, "起動時から全画面にする（全画面のあいだはカーソルを隠す）")
	fs.Func("render-scale",
		"シェーダーを描く解像度の倍率（0.25〜1.0、既定 1.0）。1.0 未満なら縮めて描き、拡大して出す",
		func(s string) error {
			v, err := parseRenderScale(s)
			opts.renderScale = v

			return err
		})
	fs.BoolVar(&opts.previews, "previews", true,
		"制御口の GET /v1/previews（Stream Deck のキーに出す数字キーのシェーダの動くデモ）を有効にする。false なら描かない")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}

	return opts, nil
}

// shaderListLines はシェーダーの一覧を、キーと揃えた 1 始まりの番号で組み立てる。
// llmSlots が無ければファイル名順に 1, 2, ... と振る（1〜9 がキー）。
// 有れば予約名以外に 1〜7 を振り（8 本目以降はキー無し）、最後に予約名を 8・9 として出す。
func shaderListLines(names []string, llmSlots []string) []string {
	lines := make([]string, 0, len(names)+len(llmSlots))
	if len(llmSlots) == 0 {
		for i, name := range names {
			lines = append(lines, fmt.Sprintf("shader %d: %s", i+1, name))
		}

		return lines
	}

	k := 0
	for _, name := range names {
		if isReserved(name, llmSlots) {
			continue
		}
		if k < plainKeys {
			k++
			lines = append(lines, fmt.Sprintf("shader %d: %s", k, name))
		} else {
			lines = append(lines, fmt.Sprintf("shader -: %s (no key)", name))
		}
	}
	for _, label := range llmKeyLabels(llmSlots) {
		lines = append(lines, "shader "+label)
	}

	return lines
}

func logShaderList(names []string, llmSlots []string) {
	for _, line := range shaderListLines(names, llmSlots) {
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
			logShaderList(g.sm.Names(), g.llmSlots)
		}
	}

	// 削除も Engine 経由にして、スロット・HUD の errs・制御口の last_error を揃えて消す。
	if paths := drainEvents(g.watcher.Removed, maxEventsPerFrame); len(paths) > 0 {
		processRemoved(paths, g.eng.RemoveFile, g.errs)
		logShaderList(g.sm.Names(), g.llmSlots)
	}

	// 制御口からの処理（状態の読み取り・切替・差し替え）はここでだけ実行する
	g.queue.Drain(g.eng)

	g.handleSlotKeys()

	if inpututil.IsKeyJustPressed(ebiten.KeySpace) {
		g.tapper.Tap(time.Now())
	}

	if inpututil.IsKeyJustPressed(ebiten.KeyBracketLeft) {
		g.pressKey("[", false) // 制御口の POST /v1/keys と同じ処理
	}

	if inpututil.IsKeyJustPressed(ebiten.KeyBracketRight) {
		g.pressKey("]", false)
	}

	if inpututil.IsKeyJustPressed(ebiten.KeyH) {
		g.showHUD = !g.showHUD
	}

	g.sm.Tick(g.tapper.BPM())

	if inpututil.IsKeyJustPressed(ebiten.KeyF) {
		ebiten.SetFullscreen(!ebiten.IsFullscreen())
	}
	g.cursor.update(ebiten.IsFullscreen())

	g.cursorX, g.cursorY = ebiten.CursorPosition()

	g.title.update(g.currentTitle())

	g.frame++

	return nil
}

// handleSlotKeys は数字キーでシェーダーを切り替える。数字は即時切替、Shift＋数字はクロスフェード。
// -llm-slots 有りのときは 8・9 が予約名、0 が直前に映っていた予約名以外のシェーダーへの即時切替。
// 処理は制御口の POST /v1/keys と同じ pressKey を通す（[ ] は Update で同じく pressKey を呼ぶ）。
func (g *Game) handleSlotKeys() {
	// キーを処理する前に記録するので、予約名へ切り替えた直後も切替前のシェーダーが残る
	g.observeBack()

	shift := ebiten.IsKeyPressed(ebiten.KeyShiftLeft) ||
		ebiten.IsKeyPressed(ebiten.KeyShiftRight)

	for k := range numKeys {
		if inpututil.IsKeyJustPressed(ebiten.Key1 + ebiten.Key(k)) {
			g.pressKey(strconv.Itoa(k+1), shift)
		}
	}
	if inpututil.IsKeyJustPressed(ebiten.Key0) {
		g.pressKey("0", shift)
	}
}

func (g *Game) Draw(screen *ebiten.Image) {
	g.frames.observe(time.Now())

	// プレビュー（GET /v1/previews）は drawShaders の中で、本番の描画を積む前に描く
	shader, _ := g.sm.Active().(*ebiten.Shader)
	g.drawShaders(screen, shader)

	if g.showHUD {
		g.drawHUD(screen)
	}
}

// drawShaders はアクティブなシェーダー A を描き、フェード中なら B を画面外に描いてから重ねる。
// -render-scale が 1 未満なら、A と B を画面 × S の画面外の画像に描き、線形補間で拡大して画面に出す。
// 画面は既定で毎フレーム消去されるので、画面そのものに描くときは Clear しない。
// 先にプレビュー（GET /v1/previews）を同じ uniform で描く。shader が nil ならプレビューだけを描く。
func (g *Game) drawShaders(screen *ebiten.Image, shader *ebiten.Shader) {
	sw, sh := screen.Bounds().Dx(), screen.Bounds().Dy()

	// 制御口の state の resolution は画面の大きさのまま（契約を変えない）
	g.eng.Resolution = [2]int{sw, sh}
	now := time.Now()
	in := shaderInput(frameParams{
		ScreenW: sw,
		ScreenH: sh,
		Scale:   g.renderScale,
		CursorX: g.cursorX,
		CursorY: g.cursorY,
		Elapsed: now.Sub(g.startAt),
		Beat:    visualPhase(g.tapper, now, g.avOffset),
		Frame:   g.frame,
		Random:  rand.Float32(),
	})

	// プレビューは本番と同じ uniform で、本番の描画を積む前に描いて読み出す。
	// ReadPixels は積んである命令をすべて流して待つので、後にすると本番のフレームを巻き込む。要求が無ければ何もしない
	g.drawPreviews(in, sw, sh)

	if shader == nil {
		return
	}
	uniforms := g.uniforms.Build(in)
	w, h := in.Width, in.Height

	target := screen
	if g.renderScale < maxRenderScale {
		g.render = resizedImage(g.render, w, h)
		target = g.render
		target.Clear()
	}

	opA := &ebiten.DrawRectShaderOptions{Uniforms: uniforms}
	target.DrawRectShader(w, h, shader, opA)

	if shaderB, ok := g.sm.ActiveB().(*ebiten.Shader); g.sm.Fading() && ok && shaderB != nil {
		// DrawRectShaderOptions.ColorScale はシェーダーの color 引数に渡るだけで、
		// color を使わないシェーダーでは効かない。B を画面外に描き、DrawImage の ColorScale で薄めて重ねる。
		// ScaleAlpha は RGBA の 4 成分に掛かるので、乗算済みアルファのまま正しく補間される。
		off := g.offscreenFor(w, h)
		off.Clear()
		opB := &ebiten.DrawRectShaderOptions{Uniforms: uniforms}
		off.DrawRectShader(w, h, shaderB, opB)

		opMix := &ebiten.DrawImageOptions{}
		opMix.ColorScale.ScaleAlpha(g.sm.MixRatio())
		target.DrawImage(off, opMix)
	}

	if target != screen {
		op := &ebiten.DrawImageOptions{Filter: ebiten.FilterLinear}
		op.GeoM.Scale(float64(sw)/float64(w), float64(sh)/float64(h))
		screen.DrawImage(target, op)
	}
}

// offscreenFor は w x h の画面外の画像（クロスフェードの B 側）を返す。
func (g *Game) offscreenFor(w, h int) *ebiten.Image {
	g.offscreen = resizedImage(g.offscreen, w, h)

	return g.offscreen
}

// resizedImage は img が w x h ならそのまま返し、違えば解放して作り直す。
func resizedImage(img *ebiten.Image, w, h int) *ebiten.Image {
	if img != nil {
		b := img.Bounds()
		if b.Dx() == w && b.Dy() == h {
			return img
		}
		img.Deallocate()
	}

	return ebiten.NewImage(w, h)
}

// hudState は now の時点で HUD に出す値を集める。
func (g *Game) hudState(now time.Time) hudState {
	return hudState{
		BPM:       g.tapper.BPM(),
		Measured:  g.tapper.Measured(),
		FadeBeats: g.sm.CurrentFadeBeats(), // beats 指定のフェード中はその拍数（GET /v1/state の fade.beats と同じ）
		Mix:       g.sm.MixRatio(),
		AVOffset:  g.avOffset,
		Perf: &perfState{
			FPS:         g.actualFPS(),
			FrameMax:    g.frames.max(now),
			RenderScale: g.renderScale,
		},
		Errs:     g.errs,
		LLMSlots: g.llmSlots,
	}
}

// drawHUD は状態とエラーを左上に描く。LayoutF がデバイスピクセルの論理画面を返すため、
// 文字サイズと位置はデバイスの拡大率に合わせる。
func (g *Game) drawHUD(screen *ebiten.Image) {
	s := ebiten.Monitor().DeviceScaleFactor()
	msg := hudText(g.hudState(time.Now()))
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

	// -list-displays もウィンドウを開かずに終わる（AppendMonitors は RunGame の前に GLFW を初期化して一覧を返す）
	displays := &ebitenDisplays{}
	if opts.listDisplays {
		printDisplays(displays, os.Stdout)

		return
	}

	ebiten.SetWindowSize(screenWidth, screenHeight)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetWindowTitle(appName)

	if err := setupDisplay(opts, displays, os.Stderr); err != nil {
		log.Print(err)
		os.Exit(2)
	}

	if err := run(opts); err != nil {
		log.Fatal(err)
	}
}

// logLLMSlots は -llm-slots の設定を起動ログに出す。
func logLLMSlots(llmSlots []string) {
	if len(llmSlots) == 0 {
		return
	}
	log.Printf("llm-slots: %s (PUT is limited to these names; key 0 returns to the last non-LLM shader)",
		strings.Join(llmKeyLabels(llmSlots), ", "))
	if len(llmSlots) < control.MaxLLMSlots {
		log.Print("llm-slots: 2 names are recommended (the LLM writes the slot that is off screen while the other is live)")
	}
}

// run は制御口を起動してからゲームを回し、終了時に制御口を閉じて発見ファイルを消す。
func run(opts options) error {
	controlAddr, llmSlots := opts.controlAddr, opts.llmSlots
	shaderDir, err := filepath.Abs(opts.shaderDir)
	if err != nil {
		return fmt.Errorf("shader dir: %w", err)
	}

	logLLMSlots(llmSlots)

	queue := control.NewQueue(64)
	g, err := NewGame(shaderDir, queue, llmSlots)
	if err != nil {
		return err
	}
	defer g.watcher.Close()
	g.avOffset = opts.avOffset
	g.renderScale = opts.renderScale
	if g.renderScale < maxRenderScale {
		log.Printf("render-scale: %.2f (shaders are drawn at the reduced size and upscaled)", g.renderScale)
	}
	if g.avOffset != 0 {
		log.Printf("av-offset: %dms (Beat only)", g.avOffset.Milliseconds())
	}
	if !opts.previews {
		g.disablePreviews()
		log.Print("previews: disabled (GET /v1/previews returns 404)")
	}

	ctl, err := control.Start(control.Config{
		Addr:      controlAddr,
		ShaderDir: shaderDir,
		Compile:   ebitenCompiler,
		Queue:     queue,
		LLMSlots:  llmSlots,
		Previews:  g.previews,
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
