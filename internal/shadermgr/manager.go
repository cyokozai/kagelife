package shadermgr

import (
	"fmt"
	"os"
	"time"
)

var FadePresets = []float32{0.5, 1, 2, 3, 4, 8, 16}

const DefaultFadeBeatsIdx = 4

type ShaderCompiler func(src []byte) (Shader, error)

type Shader interface{ Dispose() }

type Manager struct {
	shaders      []Shader       // ロード済みシェーダー
	names        []string       // ファイル名
	activeAIdx   int            // 現在アクティブなインデックス A
	activeBIdx   int            // 現在アクティブなインデックス B
	fadeBeatsIdx int            // フェードイン/アウトにかかる拍数カウント
	mixRatio     float32        //
	fading       bool           //
	fadeStart    time.Time      //
	fadeOverride float32        // BeginFadeBeats で指定した拍数（0 ならプリセットに従う）
	Compiler     ShaderCompiler // コンパイル関数
}

func New(compiler ShaderCompiler) *Manager {
	return &Manager{
		Compiler:     compiler,
		activeBIdx:   -1,
		fadeBeatsIdx: DefaultFadeBeatsIdx,
	}
}

func (m *Manager) Load(path string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	return m.compile(path, src)
}

func (m *Manager) Reload(path string, src []byte) error {
	newShader, err := m.Compiler(src)
	if err != nil {
		return fmt.Errorf("compile %s: %w", path, err)
	}

	m.Install(path, newShader)

	return nil
}

// Install はコンパイル済みのシェーダーを path の名前で登録する。
// 同名があれば旧シェーダーを Dispose して差し替え、無ければ末尾に追加する。
// 新規追加なら true を返す。
func (m *Manager) Install(path string, shader Shader) bool {
	if i := m.IndexOf(path); i >= 0 {
		if m.shaders[i] != nil {
			m.shaders[i].Dispose() // 旧シェーダーを解放してから差し替え
		}
		m.shaders[i] = shader

		return false
	}

	m.shaders = append(m.shaders, shader) // 新規追加
	m.names = append(m.names, path)

	return true
}

// IndexOf は登録名 path のインデックスを返す。無ければ -1。
func (m *Manager) IndexOf(path string) int {
	for i, name := range m.names {
		if name == path {
			return i
		}
	}

	return -1
}

func (m *Manager) Switch(index int) {
	if index < 0 || index >= len(m.shaders) {
		return
	}
	m.activeAIdx = index
	m.activeBIdx = -1
	m.fading = false
	m.mixRatio = 0
}

func (m *Manager) Active() Shader {
	if len(m.shaders) == 0 {
		return nil
	}

	return m.shaders[m.activeAIdx]
}

func (m *Manager) ActiveIndex() int {
	return m.activeAIdx
}

func (m *Manager) Len() int {
	return len(m.shaders)
}

func (m *Manager) Names() []string {
	return append([]string(nil), m.names...)
}

func (m *Manager) ActiveB() Shader {
	if m.activeBIdx < 0 || m.activeBIdx >= len(m.shaders) {
		return nil
	}

	return m.shaders[m.activeBIdx]
}

func (m *Manager) Fading() bool {
	return m.fading
}

func (m *Manager) MixRatio() float32 {
	return m.mixRatio
}

// BeginFade は現在のプリセット拍数で idx へのクロスフェードを始める。
// フェード中にプリセットを変えると、進行中のフェードにも反映される。
func (m *Manager) BeginFade(idx int) {
	m.beginFade(idx, 0)
}

// BeginFadeBeats は beats 拍で idx へのクロスフェードを始める。プリセットは変えない。
// beats が 0 以下なら何もしない。
func (m *Manager) BeginFadeBeats(idx int, beats float32) {
	if beats <= 0 {
		return
	}
	m.beginFade(idx, beats)
}

func (m *Manager) beginFade(idx int, override float32) {
	if idx < 0 || idx >= len(m.shaders) {
		return
	}
	if idx == m.activeAIdx && !m.fading {
		return
	}
	m.activeBIdx = idx
	m.fadeStart = time.Now()
	m.fading = true
	m.mixRatio = 0
	m.fadeOverride = override
}

// FadeTargetIndex はフェード先のインデックスを返す。フェード中でなければ -1。
func (m *Manager) FadeTargetIndex() int {
	if !m.fading {
		return -1
	}

	return m.activeBIdx
}

// CurrentFadeBeats は進行中（または次に始まる）フェードの拍数を返す。
// BeginFadeBeats で指定した拍数があればそれ、無ければプリセットの値。
func (m *Manager) CurrentFadeBeats() float32 {
	if m.fading && m.fadeOverride > 0 {
		return m.fadeOverride
	}

	return m.FadeBeats()
}

func (m *Manager) Tick(bpm float64) {
	if !m.fading || bpm <= 0 {
		return
	}

	beats := float64(m.CurrentFadeBeats())
	fadeDuration := beats * 60.0 / bpm
	if fadeDuration <= 0 {
		return
	}

	elapsed := time.Since(m.fadeStart).Seconds()
	ratio := elapsed / fadeDuration
	if ratio >= 1.0 {
		m.activeAIdx = m.activeBIdx
		m.activeBIdx = -1
		m.fading = false
		m.mixRatio = 0

		return
	}
	m.mixRatio = float32(ratio)
}

func (m *Manager) FadeBeats() float32 {
	return FadePresets[m.fadeBeatsIdx]
}

func (m *Manager) IncFadeBeats() {
	if m.fadeBeatsIdx < len(FadePresets)-1 {
		m.fadeBeatsIdx++
	}
}

func (m *Manager) DecFadeBeats() {
	if m.fadeBeatsIdx > 0 {
		m.fadeBeatsIdx--
	}
}

func (m *Manager) compile(name string, src []byte) error {
	shader, err := m.Compiler(src)
	if err != nil {
		return fmt.Errorf("compile %s: %w", name, err)
	}

	m.shaders = append(m.shaders, shader)
	m.names = append(m.names, name)

	return nil
}
