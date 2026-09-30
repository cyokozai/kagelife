package control

import (
	"errors"
	"path/filepath"
	"time"

	"github.com/cyokozai/kagelife/internal/shadermgr"
	"github.com/cyokozai/kagelife/internal/tempo"
)

var (
	// ErrNotFound は指定の名前のシェーダが読み込まれていないこと。
	ErrNotFound = errors.New("shader not found")
	// ErrBPMNotSet は BPM が未設定（0）でクロスフェードできないこと。
	ErrBPMNotSet = errors.New("bpm is not set")
)

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
type State struct {
	Active     string     `json:"active"`
	Shaders    []string   `json:"shaders"`
	BPM        float64    `json:"bpm"`
	Fade       FadeState  `json:"fade"`
	FPS        float64    `json:"fps"`
	Resolution [2]int     `json:"resolution"`
	LastError  *LastError `json:"last_error"`
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
		Shaders:    names,
		BPM:        e.Tapper.BPM(),
		Resolution: e.Resolution,
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

// Switch は name に即時に切り替える。
func (e *Engine) Switch(name string) error {
	i := e.SM.IndexOf(e.PathOf(name))
	if i < 0 {
		return ErrNotFound
	}
	e.SM.Switch(i)

	return nil
}

// Crossfade は name へのクロスフェードを始め、使う拍数を返す。
// beats が 0 以下なら現在のプリセット拍数を使う。
func (e *Engine) Crossfade(name string, beats float64) (float64, error) {
	i := e.SM.IndexOf(e.PathOf(name))
	if i < 0 {
		return 0, ErrNotFound
	}
	if e.Tapper.BPM() <= 0 {
		return 0, ErrBPMNotSet
	}

	if beats > 0 {
		e.SM.BeginFadeBeats(i, float32(beats))

		return beats, nil
	}
	e.SM.BeginFade(i)

	return float64(e.SM.FadeBeats()), nil
}

// SetBPM は BPM を直接設定し、今を拍頭にする。
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

// RecordError は last_error を記録する。
func (e *Engine) RecordError(shader, message string) {
	e.lastErr = &LastError{Shader: shader, Message: message}
}

// LastError は直近のコンパイル失敗（無ければ nil）を返す。
func (e *Engine) LastError() *LastError {
	return e.lastErr
}
