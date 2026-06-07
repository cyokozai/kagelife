package shadermgr

import (
	"fmt"
	"os"
)


type Shader interface {
	Dispose()
}


type ShaderCompiler func(src []byte) (Shader, error)


type Manager struct {
	shaders  []Shader       // ロード済みシェーダー
	names    []string       // ファイル名（表示用）
	active   int            // 現在アクティブなインデックス
	Compiler ShaderCompiler // コンパイル関数（差し替え可）
}


func New(compiler ShaderCompiler) *Manager {
	return &Manager{Compiler: compiler}
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
	if index >= 0 && index < len(m.shaders) {
		m.active = index
	}
}


func (m *Manager) Active() Shader {
	if len(m.shaders) == 0 {
		return nil
	}

	return m.shaders[m.active]
}


func (m *Manager) ActiveIndex() int {
	return m.active
}


func (m *Manager) Len() int {
	return len(m.shaders)
}


func (m *Manager) Names() []string {
	return append([]string(nil), m.names...) // コピーを返す
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
