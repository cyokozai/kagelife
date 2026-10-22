package control

import (
	"errors"
	"path/filepath"
	"time"

	"github.com/cyokozai/kagelife/internal/shadermgr"
	"github.com/cyokozai/kagelife/internal/tempo"
)

// ErrNotFound は指定の名前のシェーダが読み込まれていないこと。
// ファイルはあってもコンパイルに失敗している（shadermgr の nil のスロット）場合も含む。
var ErrNotFound = errors.New("shader not found")

// LastError は直近のシェーダのコンパイル失敗。
type LastError struct {
	Shader  string `json:"shader"`
	Message string `json:"message"`
}

// FadeState は GET /v1/state の fade。
type FadeState struct {
	Fading bool    `json:"fading"`
	Target string  `json:"target"`
	Mix    float64 `json:"mix"`
	Beats  float64 `json:"beats"`
}

// State は GET /v1/state の応答。
// Shaders はファイル名順で、コンパイルに失敗したファイルも含む。
// BPM は常に tempo.MinBPM..tempo.MaxBPM で、タップ前は tempo.DefaultBPM。
// BPMMeasured は BPM がタップか set_bpm で設定された値なら true、既定値のままなら false。
type State struct {
	Active      string     `json:"active"`
	Shaders     []string   `json:"shaders"`
	BPM         float64    `json:"bpm"`
	BPMMeasured bool       `json:"bpm_measured"`
	Fade        FadeState  `json:"fade"`
	FPS         float64    `json:"fps"`
	Resolution  [2]int     `json:"resolution"`
	LastError   *LastError `json:"last_error"`
}

// Engine はゲームループが持つ VJ 状態（シェーダとテンポ）を名前で操作する。
// どのメソッドもゲームループ（Update）のゴルーチンからだけ呼ぶこと。
type Engine struct {
	SM        *shadermgr.Manager
	Tapper    *tempo.Tapper
	ShaderDir string // 絶対パス。Manager にはこの下の <name>.kage のパスで登録する
	// Now は現在時刻（nil なら time.Now）。
	Now func() time.Time
	// FPS は実測 FPS（nil なら 0）。
	FPS func() float64
	// Resolution は直近の Draw の画面サイズ。
	Resolution [2]int

	lastErr *LastError
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}

	return time.Now()
}

// PathOf はシェーダ名に対応するファイルのパスを返す。
func (e *Engine) PathOf(name string) string {
	return filepath.Join(e.ShaderDir, name+ShaderExt)
}

func (e *Engine) nameAt(i int) string {
	names := e.SM.Names()
	if i < 0 || i >= len(names) {
		return ""
	}

	return NameFromPath(names[i])
}

// State は現在の状態を返す。
func (e *Engine) State() State {
	paths := e.SM.Names()
	names := make([]string, 0, len(paths))
	for _, p := range paths {
		names = append(names, NameFromPath(p))
	}

	st := State{
		Shaders:     names,
		BPM:         e.Tapper.BPM(),
		BPMMeasured: e.Tapper.Measured(),
		Resolution:  e.Resolution,
		Fade: FadeState{
			Fading: e.SM.Fading(),
			Target: e.nameAt(e.SM.FadeTargetIndex()),
			Mix:    float64(e.SM.MixRatio()),
			Beats:  float64(e.SM.CurrentFadeBeats()),
		},
	}
	if e.SM.Len() > 0 {
		st.Active = e.nameAt(e.SM.ActiveIndex())
	}
	if e.FPS != nil {
		st.FPS = e.FPS()
	}
	if e.lastErr != nil {
		le := *e.lastErr
		st.LastError = &le
	}

	return st
}

// loadedIndex は name のスロット位置を返す。無い、またはコンパイルに失敗したスロットなら ErrNotFound。
func (e *Engine) loadedIndex(name string) (int, error) {
	i := e.SM.IndexOf(e.PathOf(name))
	if !e.SM.Usable(i) {
		return -1, ErrNotFound
	}

	return i, nil
}

// Switch は name に即時に切り替える。
func (e *Engine) Switch(name string) error {
	i, err := e.loadedIndex(name)
	if err != nil {
		return err
	}
	e.SM.Switch(i)

	return nil
}

// Crossfade は name へのクロスフェードを始め、使う拍数を返す。
// beats が 0 以下なら現在のプリセット拍数を使う。BPM は常に 40〜300 なので、BPM を理由には失敗しない。
// フェード中の再指示は shadermgr の規則に従う。同じ先への再指示は無視されるので、
// そのときは進行中のフェードの拍数を返す。
func (e *Engine) Crossfade(name string, beats float64) (float64, error) {
	i, err := e.loadedIndex(name)
	if err != nil {
		return 0, err
	}

	if beats > 0 {
		e.SM.BeginFadeBeats(i, float32(beats))
	} else {
		e.SM.BeginFade(i)
		beats = float64(e.SM.FadeBeats())
	}
	if e.SM.FadeTargetIndex() == i {
		return float64(e.SM.CurrentFadeBeats()), nil
	}

	return beats, nil
}

// SetBPM は BPM を直接設定し、今を拍頭にする。範囲（40〜300）の検査は呼び出し側（postBPM）で行う。
func (e *Engine) SetBPM(bpm float64) {
	e.Tapper.SetBPM(bpm, e.now())
}

// Install はコンパイル済みのシェーダを name で登録（既存なら差し替え）し、last_error を消す。
// 新規追加なら true。
func (e *Engine) Install(name string, sh shadermgr.Shader) bool {
	created := e.SM.Install(e.PathOf(name), sh)
	e.lastErr = nil

	return created
}

// ReloadFile はファイル監視で変更を検知した path を src で再コンパイルし、
// 失敗なら last_error に記録、成功なら消す。
func (e *Engine) ReloadFile(path string, src []byte) error {
	if err := e.SM.Reload(path, src); err != nil {
		e.RecordError(NameFromPath(path), err.Error())

		return err
	}
	e.lastErr = nil

	return nil
}

// RemoveFile はファイル監視で削除を検知した path のシェーダを外し、
// last_error がそのファイルのものなら消す（無くなったファイルのエラーを出し続けないため）。
func (e *Engine) RemoveFile(path string) {
	e.SM.Remove(path)
	if e.lastErr != nil && e.lastErr.Shader == NameFromPath(path) {
		e.lastErr = nil
	}
}

// RecordError は last_error を記録する。
func (e *Engine) RecordError(shader, message string) {
	e.lastErr = &LastError{Shader: shader, Message: message}
}

// LastError は直近のコンパイル失敗（無ければ nil）を返す。
func (e *Engine) LastError() *LastError {
	return e.lastErr
}
