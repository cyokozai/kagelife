package control

import (
	"bytes"
	"image"
	"image/jpeg"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// GET /v1/previews（feature previews）。Stream Deck のキーの液晶に、数字キー 1〜9 に
// 割り当たったシェーダの小さな動くデモを出すためのもの。
//
// 描くのはゲームループ（main の Draw）で、9 個を 1 枚のアトラスに描き、GPU から 1 回だけ読み出して
// Submit に渡す。JPEG の符号化は別ゴルーチンで行い、Update・Draw を待たせない。
// 直近 PreviewIdle 以内に要求が来ていなければ Due が false を返し、描かない（GPU も CPU も追加で使わない）。

const (
	// PreviewSize は 1 キー分のプレビューの一辺（Stream Deck MK.2 のキー画像）。
	PreviewSize = 72
	// PreviewFPS はプレビューを描く上限の頻度。
	PreviewFPS = 10
	// PreviewIdle は最後の要求からプレビューを描き続ける時間。これを過ぎたら描かない。
	PreviewIdle = 2 * time.Second
	// PreviewCols はアトラスの 1 行のセルの数（3x3 でキー 1〜9）。
	PreviewCols = 3
	// PreviewAtlasSize はアトラスの一辺（216）。
	PreviewAtlasSize = PreviewSize * PreviewCols
	// previewQuality は JPEG の品質。
	previewQuality = 80
	// previewInterval はプレビューを描く最小の間隔（1 / PreviewFPS）。
	previewInterval = time.Second / PreviewFPS
)

// PreviewSlot はキー 1 つに割り当たっているスロット。
type PreviewSlot struct {
	Key          string // "1"〜"9"
	Name         string // シェーダ名（拡張子なし）
	CompileError bool   // コンパイルに失敗したスロット（shadermgr の nil）なら true
}

// PreviewKey は GET /v1/previews の keys の 1 件。JPEG が nil なら null になる。
type PreviewKey struct {
	Key          string `json:"key"`
	Name         string `json:"name"`
	JPEG         []byte `json:"jpeg"` // encoding/json が base64 にする
	CompileError bool   `json:"compile_error"`
}

// PreviewFrame は GET /v1/previews の応答。
type PreviewFrame struct {
	Size  int          `json:"size"`
	FPS   int          `json:"fps"`
	Frame int          `json:"frame"`
	Keys  []PreviewKey `json:"keys"`
}

// Previews はプレビューの最新のフレームと、描くかどうかの判断を持つ。
// HTTP のハンドラ・ゲームループ・符号化のゴルーチンから呼ばれるので、中は mu で守る。
type Previews struct {
	// Now は現在時刻（nil なら time.Now）。テストで差し替える。
	Now func() time.Time

	mu         sync.Mutex
	lastReq    time.Time // 最後の要求の時刻
	hasReq     bool      // 要求が 1 回でも来たか
	lastRender time.Time // 最後に Due が true を返した時刻
	latest     PreviewFrame

	busy atomic.Bool // 符号化中（アトラスの画素を符号化が読んでいる）
}

// NewPreviews は描いていない状態の Previews を作る。
func NewPreviews() *Previews {
	return &Previews{latest: PreviewFrame{Size: PreviewSize, FPS: PreviewFPS, Keys: []PreviewKey{}}}
}

func (p *Previews) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}

	return time.Now()
}

// wantedAt は now の時点で描くべきか（直近 PreviewIdle 以内に要求が来ているか）を返す。mu を持って呼ぶ。
func (p *Previews) wantedAt(now time.Time) bool {
	return p.hasReq && now.Sub(p.lastReq) <= PreviewIdle
}

// Wanted は今プレビューを描くべきかを返す。
func (p *Previews) Wanted() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.wantedAt(p.now())
}

// Busy は符号化中かを返す。符号化中は Submit に渡した画素を書き換えてはいけない。
func (p *Previews) Busy() bool {
	return p.busy.Load()
}

// Due はゲームループが今プレビューを描くべきかを返し、true なら描いた時刻として記録する。
// 要求が直近 PreviewIdle 以内に来ていて、符号化中でなく、前回から 1/PreviewFPS 以上経っていれば true。
// true を返したら、呼び出し側は描いて Submit を呼ぶ。
func (p *Previews) Due() bool {
	if p.busy.Load() {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	if !p.wantedAt(now) || now.Sub(p.lastRender) < previewInterval {
		return false
	}
	p.lastRender = now

	return true
}

// Submit は読み出したアトラスの画素（PreviewAtlasSize 四方の乗算済み RGBA）を、別ゴルーチンで
// キーごとの JPEG にし、最新のフレームとして frame を 1 増やして置く。待たずに戻る。
// slots はキーの順。キー n（1〜9）はアトラスの (n-1)%3 列・(n-1)/3 行のセル。
// 符号化が終わる（Busy が false になる）まで pix を書き換えないこと。
func (p *Previews) Submit(pix []byte, slots []PreviewSlot) {
	p.busy.Store(true)
	go func() {
		defer p.busy.Store(false)
		keys := encodePreviews(pix, slots)
		p.mu.Lock()
		p.latest = PreviewFrame{Size: PreviewSize, FPS: PreviewFPS, Frame: p.latest.Frame + 1, Keys: keys}
		p.mu.Unlock()
	}()
}

// encodePreviews は slots ごとにアトラスのセルを JPEG にする。
// コンパイルに失敗したスロット・キーが 1〜9 でないもの・画素が足りないものは JPEG を nil にする。
func encodePreviews(pix []byte, slots []PreviewSlot) []PreviewKey {
	const stride = PreviewAtlasSize * 4
	keys := make([]PreviewKey, 0, len(slots))
	var buf bytes.Buffer
	for _, s := range slots {
		k := PreviewKey{Key: s.Key, Name: s.Name, CompileError: s.CompileError}
		if cell, ok := previewCell(s.Key); ok && !s.CompileError && len(pix) >= PreviewAtlasSize*stride {
			col, row := cell%PreviewCols, cell/PreviewCols
			off := row*PreviewSize*stride + col*PreviewSize*4
			img := &image.RGBA{Pix: pix[off:], Stride: stride, Rect: image.Rect(0, 0, PreviewSize, PreviewSize)}
			buf.Reset()
			if jpeg.Encode(&buf, img, &jpeg.Options{Quality: previewQuality}) == nil {
				k.JPEG = append([]byte(nil), buf.Bytes()...)
			}
		}
		keys = append(keys, k)
	}

	return keys
}

// previewCell はキー "1"〜"9" のアトラスのセルの位置（0〜8）を返す。
func previewCell(key string) (int, bool) {
	if len(key) != 1 || key[0] < '1' || key[0] > '9' {
		return 0, false
	}

	return int(key[0] - '1'), true
}

// request は要求を記録し、最新のフレームを返す。
// 要求の前に止まっていた（直近 PreviewIdle 以内に要求が無かった）なら stale を true にする。
// このとき latest の絵は古いので、呼び出し側は今の割り当てを pending で置き直す。
func (p *Previews) request() (f PreviewFrame, stale bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	stale = !p.wantedAt(now)
	p.lastReq, p.hasReq = now, true
	f = p.latest
	f.Keys = append([]PreviewKey{}, p.latest.Keys...)

	return f, stale
}

// pending は slots を jpeg 無しで並べたフレーム（番号は frame）を返す。
// 最新のフレームがまだ frame のままなら、それも置き換える（古い絵を返さないため）。
func (p *Previews) pending(frame int, slots []PreviewSlot) PreviewFrame {
	keys := make([]PreviewKey, 0, len(slots))
	for _, s := range slots {
		keys = append(keys, PreviewKey{Key: s.Key, Name: s.Name, CompileError: s.CompileError})
	}
	f := PreviewFrame{Size: PreviewSize, FPS: PreviewFPS, Frame: frame, Keys: keys}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.latest.Frame == frame {
		p.latest = f
		f.Keys = append([]PreviewKey{}, keys...)
	}

	return f
}

// previewSlots は Engine.PreviewSlots の結果を返す。未設定なら空。
func (e *Engine) previewSlots() []PreviewSlot {
	if e.PreviewSlots == nil {
		return nil
	}

	return e.PreviewSlots()
}

// getPreviews は最新の完成したプレビューを返す。要求本文もクエリも見ない。
// 止まっていた後の最初の要求は、今の割り当てをゲームループから読み、jpeg null で即答する。
func (s *Server) getPreviews(w http.ResponseWriter, _ *http.Request) {
	f, stale := s.cfg.Previews.request()
	if stale {
		slots, err := call(s.cfg.Queue, s.cfg.Timeout, (*Engine).previewSlots)
		if err != nil {
			writeLoopError(w)

			return
		}
		f = s.cfg.Previews.pending(f.Frame, slots)
	}
	writeJSON(w, http.StatusOK, f)
}
