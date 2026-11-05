package carabiner

import (
	"bufio"
	"io"
	"strings"
)

// maxLineBytes は 1 行の上限。status 応答は 100 バイト前後なので十分に大きく取る。
const maxLineBytes = 64 * 1024

// LineReader は TCP の読み取りを改行で区切る。1 回の読み取りに複数行が入っていても、
// 行の途中で読み取りが切れていても、1 行ずつ返す。
type LineReader struct {
	sc *bufio.Scanner
}

// NewLineReader は r を読む LineReader を作る。
func NewLineReader(r io.Reader) *LineReader {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1024), maxLineBytes)
	sc.Split(scanTerminatedLines)
	return &LineReader{sc: sc}
}

// ReadLine は次の空でない行を、末尾の改行（\r\n も）を除いて返す。
// 入力が終われば io.EOF を返す。改行で終わっていない最後の断片は捨てる。
func (lr *LineReader) ReadLine() (string, error) {
	for lr.sc.Scan() {
		line := strings.TrimRight(lr.sc.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		return line, nil
	}
	if err := lr.sc.Err(); err != nil {
		return "", err
	}
	return "", io.EOF
}

// scanTerminatedLines は bufio.ScanLines と違い、入力の終わりにある改行なしの断片を返さない。
// 接続が行の途中で切れたときに、壊れた行を解析へ回さないため。
func scanTerminatedLines(data []byte, atEOF bool) (int, []byte, error) {
	for i, b := range data {
		if b == '\n' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF {
		return len(data), nil, nil
	}
	return 0, nil, nil
}
