package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
)

// unitDirective は全シェーダに必須のディレクティブ。
// Ebitengine v2.10.4 は texels 既定だとコンパイルエラーの行番号がずれるため、
// 手書き・MCP 経由の書き込みを問わず pixels を明示させる。
const unitDirective = "//kage:unit pixels"

// unitDirectiveMaxLine はディレクティブを探す先頭からの行数。
const unitDirectiveMaxLine = 5

func shaderFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("shaders", "*.kage"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("shaders/*.kage が 1 件も見つからない")
	}
	return files
}

// TestShadersCompile は同梱シェーダがすべて Kage としてコンパイルできることを確かめる。
// NewShader は RunGame 前・DISPLAY 無しでも呼べる。
func TestShadersCompile(t *testing.T) {
	for _, path := range shaderFiles(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			s, err := ebiten.NewShader(src)
			if err != nil {
				t.Fatalf("NewShader: %v", err)
			}
			s.Deallocate()
		})
	}
}

// TestShadersDeclarePixelUnit は全シェーダの先頭付近に //kage:unit pixels があることを確かめる。
func TestShadersDeclarePixelUnit(t *testing.T) {
	for _, path := range shaderFiles(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			sc := bufio.NewScanner(bytes.NewReader(src))
			for i := 0; i < unitDirectiveMaxLine && sc.Scan(); i++ {
				if strings.TrimSpace(sc.Text()) == unitDirective {
					return
				}
			}
			t.Fatalf("先頭 %d 行以内に %q が無い", unitDirectiveMaxLine, unitDirective)
		})
	}
}
