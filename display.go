package main

import (
	"fmt"
	"io"
	"strconv"

	"github.com/hajimehoshi/ebiten/v2"
)

// monitorInfo は 1 台のディスプレイの情報（-list-displays で出す）。
type monitorInfo struct {
	Name  string
	W, H  int     // デバイスに依らない画素数（ebiten.MonitorType.Size）
	Scale float64 // デバイスの拡大率
}

// displayAPI はディスプレイの操作。本番は ebitenDisplays、テストでは偽物に差し替える。
type displayAPI interface {
	monitors() []monitorInfo
	useMonitor(i int) // 0 始まりの添字
	setFullscreen(on bool)
}

// parseDisplay は -display の値（1 始まりの整数）を解釈する。
func parseDisplay(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("-display: 1 以上の整数で指定する（-list-displays の番号）: %q", s)
	}

	return n, nil
}

// displayLines はディスプレイの一覧を 1 始まりの番号で組み立てる。番号は -display に渡す値。
func displayLines(ms []monitorInfo) []string {
	if len(ms) == 0 {
		return []string{"display: none"}
	}
	lines := make([]string, 0, len(ms))
	for i, m := range ms {
		lines = append(lines, fmt.Sprintf("display %d: %s %dx%d (scale %.1f)", i+1, m.Name, m.W, m.H, m.Scale))
	}

	return lines
}

func printDisplays(d displayAPI, out io.Writer) {
	for _, line := range displayLines(d.monitors()) {
		_, _ = fmt.Fprintln(out, line)
	}
}

// setupDisplay は -display と -fullscreen を RunGame の前に反映する。
// -display が範囲外なら一覧を out に出してエラーを返す。
func setupDisplay(opts options, d displayAPI, out io.Writer) error {
	if opts.display > 0 {
		ms := d.monitors()
		if opts.display > len(ms) {
			printDisplays(d, out)

			return fmt.Errorf("-display %d: ディスプレイは %d 台", opts.display, len(ms))
		}
		d.useMonitor(opts.display - 1)
	}
	if opts.fullscreen {
		d.setFullscreen(true)
	}

	return nil
}

// cursorSync は全画面のあいだマウスカーソルを隠し、ウィンドウでは表示に戻す。
// 変わったときだけ set を呼ぶ（F キー以外の手段で全画面を抜けても毎フレームの確認で追従する）。
type cursorSync struct {
	known  bool
	hidden bool
	set    func(hidden bool)
}

func (c *cursorSync) update(fullscreen bool) {
	if c.known && c.hidden == fullscreen {
		return
	}
	c.known, c.hidden = true, fullscreen
	c.set(fullscreen)
}

// setEbitenCursorHidden は ebiten のカーソルモードを切り替える。
// CursorModeHidden は表示を消すだけで、位置（Cursor の uniform）は取り続ける。
func setEbitenCursorHidden(hidden bool) {
	if hidden {
		ebiten.SetCursorMode(ebiten.CursorModeHidden)
	} else {
		ebiten.SetCursorMode(ebiten.CursorModeVisible)
	}
}

// ebitenDisplays は displayAPI の本番の実装。
// ebiten.AppendMonitors は RunGame の前ならメインスレッドから呼ぶ（main から呼ぶので満たす）。
type ebitenDisplays struct {
	list []*ebiten.MonitorType
}

func (e *ebitenDisplays) load() []*ebiten.MonitorType {
	if e.list == nil {
		e.list = ebiten.AppendMonitors(nil)
	}

	return e.list
}

func (e *ebitenDisplays) monitors() []monitorInfo {
	ms := e.load()
	infos := make([]monitorInfo, 0, len(ms))
	for _, m := range ms {
		w, h := m.Size()
		infos = append(infos, monitorInfo{Name: m.Name(), W: w, H: h, Scale: m.DeviceScaleFactor()})
	}

	return infos
}

func (e *ebitenDisplays) useMonitor(i int) { ebiten.SetMonitor(e.load()[i]) }

func (e *ebitenDisplays) setFullscreen(on bool) { ebiten.SetFullscreen(on) }
