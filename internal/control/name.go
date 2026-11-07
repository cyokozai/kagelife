// Package control は LLM 用 MCP サーバなどの外部プロセスから GUI を操作するための
// HTTP 制御口（制御口 v1）を提供する。
//
// ゲーム状態（シェーダ・テンポ）に触るのはゲームループ（Update）だけにするため、
// HTTP ハンドラは処理を Queue に入れて返信を待ち、Update が Queue.Drain で実行する。
// ebiten には依存しない（CGO 無しでテストできるようにするため）。
package control

import (
	"path/filepath"
	"regexp"
	"strings"
)

// ShaderExt はシェーダファイルの拡張子。
const ShaderExt = ".kage"

// unitPixelsDirective は制御口から書き込むシェーダに必須の指定行。
const unitPixelsDirective = "//kage:unit pixels"

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// ValidName はシェーダ名（拡張子なし）が制御口 v1 の規則に合うかを返す。
func ValidName(name string) bool {
	return namePattern.MatchString(name)
}

// NameFromPath はシェーダファイルのパスからシェーダ名（ファイル名から .kage を除いたもの）を返す。
func NameFromPath(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ShaderExt)
}

// HasUnitPixels はソースに単独の `//kage:unit pixels` 行があるかを返す。
func HasUnitPixels(src string) bool {
	for _, line := range strings.Split(src, "\n") {
		if strings.TrimSpace(line) == unitPixelsDirective {
			return true
		}
	}

	return false
}
