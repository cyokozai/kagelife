package control

import (
	"errors"
	"fmt"
	"go/scanner"
	"regexp"
	"strconv"
	"strings"
)

// maxDiagnostics は compile_error で返す診断の上限件数。
const maxDiagnostics = 10

// Diagnostic はコンパイル失敗の 1 件分。Line/Col が 0 なら位置不明。
type Diagnostic struct {
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Message string `json:"message"`
}

// String は "行:列: メッセージ"（位置不明ならメッセージのみ）を返す。
func (d Diagnostic) String() string {
	if d.Line <= 0 {
		return d.Message
	}

	return fmt.Sprintf("%d:%d: %s", d.Line, d.Col, d.Message)
}

var semanticLine = regexp.MustCompile(`^(\d+):(\d+): (.*)$`)

// Diagnose はシェーダのコンパイルエラーを行・列付きの診断に分解する。
//
// Ebitengine は利用者のソースに内部コードを連結して解析するため、ソースの行数を超える
// 位置の誤りは捨てる。構文エラーは scanner.ErrorList、意味エラーは改行区切りの
// "行:列: メッセージ" 文字列として受け取る。どちらにも当たらない行は位置 0 で残す。
// 残るものが 1 件も無ければ、先頭の誤りを位置 0 で 1 件だけ返す。最大 10 件。
func Diagnose(src []byte, err error) []Diagnostic {
	if err == nil {
		return nil
	}
	lines := strings.Count(string(src), "\n") + 1
	inRange := func(l int) bool { return l >= 1 && l <= lines }

	var out []Diagnostic
	var first string

	var el scanner.ErrorList
	if errors.As(err, &el) {
		for _, e := range el {
			if first == "" {
				first = e.Error()
			}
			if inRange(e.Pos.Line) {
				out = append(out, Diagnostic{Line: e.Pos.Line, Col: e.Pos.Column, Message: e.Msg})
			}
		}
	} else {
		for _, l := range strings.Split(err.Error(), "\n") {
			l = strings.TrimSpace(l)
			if l == "" {
				continue
			}
			if first == "" {
				first = l
			}
			m := semanticLine.FindStringSubmatch(l)
			if m == nil {
				out = append(out, Diagnostic{Message: l})

				continue
			}
			line, _ := strconv.Atoi(m[1])
			col, _ := strconv.Atoi(m[2])
			if inRange(line) {
				out = append(out, Diagnostic{Line: line, Col: col, Message: m[3]})
			}
		}
	}

	if len(out) == 0 {
		if first == "" {
			first = err.Error()
		}
		out = []Diagnostic{{Message: first}}
	}
	if len(out) > maxDiagnostics {
		out = out[:maxDiagnostics]
	}

	return out
}

// Summarize は compile_error の message（先頭の診断と残り件数）を作る。
func Summarize(diags []Diagnostic) string {
	if len(diags) == 0 {
		return "shader compilation failed"
	}
	msg := "shader compilation failed: " + diags[0].String()
	if n := len(diags) - 1; n > 0 {
		msg += fmt.Sprintf(" (and %d more)", n)
	}

	return msg
}
