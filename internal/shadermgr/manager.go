package shadermgr

import (
	"fmt"
	"os"
	"time"
)


var FadePresets = []float32{ 0.5, 1, 2, 3, 4, 8, 16 }


const DefaultFadeBeatsIdx = 4


type ShaderCompiler func(src []byte) (Shader, error)


type Shader interface { Dispose() }


type Manager struct {
	shaders 		 []Shader       // ロード済みシェーダー
	names   		 []string       // ファイル名
	activeAIdx 	 int            // 現在アクティブなインデックス A
	activeBIdx 	 int						// 現在アクティブなインデックス B
	fadeBeatsIdx int 						// フェードイン/アウトにかかる拍数カウント
	mixRatio		 float32				// 
	fading 			 bool						// 
	fadeStart 	 time.Time			// 
	Compiler 		 ShaderCompiler // コンパイル関数
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

	for i, name := range m.names {
		if name == path {
			if m.shaders[i] != nil {
				m.shaders[i].Dispose() // 旧シェーダーを解放してから差し替え
			}
			m.shaders[i] = newShader

			return nil
		}
	}

	m.shaders = append(m.shaders, newShader) // 新規追加
	m.names = append(m.names, path)

	return nil
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


func (m *Manager) BeginFade(idx int) {
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
}


func (m *Manager) Tick(bpm float64) {
  if !m.fading || bpm <= 0 {
    return
  }
  
  beats := float64(FadePresets[m.fadeBeatsIdx])
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
