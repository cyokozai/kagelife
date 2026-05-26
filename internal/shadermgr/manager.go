// Package shadermgr は Kage シェーダーの管理ロジックを提供する。
//
// 【パッケージ分割の理由】
// Ebitengine は import するだけで init() が走り X11/GLFW を要求する。
// テスト可能なコアロジックを ebiten 非依存のパッケージに分離することで、
// ヘッドレスコンテナ上で `go test ./...` が通る設計にする。
//
// 依存関係:
//   main パッケージ → ebiten（本番の ShaderCompiler を提供）
//   shadermgr パッケージ → ebiten に依存しない（テスト可能）
package shadermgr

import (
	"fmt"
	"os"
)


type Shader interface {
	Dispose()
}

// ShaderCompiler はシェーダーをコンパイルする関数型。
// 本番では ebiten.NewShader をラップした関数、テストではモックを渡す。
type ShaderCompiler func(src []byte) (Shader, error)

// Manager は複数の Kage シェーダーを管理する。
type Manager struct {
	shaders  []Shader       // ロード済みシェーダー
	names    []string       // ファイル名（表示用）
	active   int            // 現在アクティブなインデックス
	Compiler ShaderCompiler // コンパイル関数（差し替え可）
}

// New は Manager を生成する。
func New(compiler ShaderCompiler) *Manager {
	return &Manager{Compiler: compiler}
}

// Load はファイルパスから Kage シェーダーを読み込んでコンパイルする。
func (m *Manager) Load(path string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	return m.compile(path, src)
}

// Reload は既存のシェーダーを再コンパイルして差し替える（ホットリロード用）。
// コンパイル成功時のみ差し替え、失敗時は旧シェーダーを維持する。
func (m *Manager) Reload(path string, src []byte) error {
	newShader, err := m.Compiler(src)
	if err != nil {
		// コンパイルエラー → 旧シェーダーをそのまま維持
		return fmt.Errorf("compile %s: %w", path, err)
	}

	for i, name := range m.names {
		if name == path {
			// 旧シェーダーを解放してから差し替え
			if m.shaders[i] != nil {
				m.shaders[i].Dispose()
			}
			m.shaders[i] = newShader
			return nil
		}
	}

	// 新規追加
	m.shaders = append(m.shaders, newShader)
	m.names = append(m.names, path)
	return nil
}

// Switch はアクティブなシェーダーのインデックスを変更する。
// 範囲外のインデックスは無視する。
func (m *Manager) Switch(index int) {
	if index >= 0 && index < len(m.shaders) {
		m.active = index
	}
}

// Active は現在アクティブなシェーダーを返す。0 個の場合は nil。
func (m *Manager) Active() Shader {
	if len(m.shaders) == 0 {
		return nil
	}
	return m.shaders[m.active]
}

// ActiveIndex は現在のアクティブインデックスを返す。
func (m *Manager) ActiveIndex() int {
	return m.active
}

// Len はロード済みシェーダーの数を返す。
func (m *Manager) Len() int {
	return len(m.shaders)
}

// Names はロード済みシェーダーのファイル名一覧を返す。
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
