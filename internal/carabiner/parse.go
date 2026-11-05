// Package carabiner は Carabiner（Ableton Link を TCP のテキストで中継する別プロセス）から
// テンポと拍を受け取る。kagelife 本体は Link の SDK を持たず、127.0.0.1:17000 の
// Carabiner と行区切りのテキストでやり取りするだけにする。
//
// Link の時刻（:start や beat-at-time など）は Link の単調時計のマイクロ秒で Go の時計と
// 基準が違うので使わない。status の応答を受け取ったローカルの時刻と :beat・:bpm を組にして
// Estimator で外挿する。
package carabiner

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ErrMalformed は応答の形が壊れているときに返す（errors.Is で判定する）。
var ErrMalformed = errors.New("carabiner: malformed message")

// Status は status 応答の中身。
type Status struct {
	Peers   int     // Link セッションの相手の数（自分を含まない）
	BPM     float64 // テンポ
	Start   int64   // Link 時計での起点（マイクロ秒）。PoC では使わない
	Beat    float64 // 応答を作った時点の拍の位置
	Playing *bool   // Start/Stop Sync が有効なときだけ値がある。無ければ nil
}

// ResponseError は Carabiner が返したエラー応答（bad-input / unsupported）。
type ResponseError struct {
	Kind   string // "bad-input" または "unsupported"
	Detail string // 後ろに続く文字列（無ければ空）
}

func (e *ResponseError) Error() string {
	if e.Detail == "" {
		return "carabiner: " + e.Kind
	}
	return "carabiner: " + e.Kind + " " + e.Detail
}

// UnknownMessageError は status でもエラーでもない応答（version など）を受けたときに返す。
// 呼び出し側は読み飛ばしてよい。
type UnknownMessageError struct {
	Type string // 先頭の語
}

func (e *UnknownMessageError) Error() string {
	return "carabiner: unhandled message type " + strconv.Quote(e.Type)
}

func malformed(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrMalformed}, args...)...)
}

// ParseStatus は応答 1 行を解析する。status なら Status を返し、
// エラー応答は *ResponseError、それ以外の種類は *UnknownMessageError、
// 形が壊れていれば ErrMalformed を包んだエラーを返す。
func ParseStatus(line string) (Status, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return Status{}, malformed("empty line")
	}
	typ, rest, _ := strings.Cut(line, " ")
	rest = strings.TrimSpace(rest)
	switch typ {
	case "status":
		return parseStatusBody(rest)
	case "bad-input", "unsupported":
		return Status{}, &ResponseError{Kind: typ, Detail: rest}
	default:
		// "status{" のように空白なしで括弧が続く形も status として扱う。
		if strings.HasPrefix(typ, "status{") {
			return parseStatusBody(strings.TrimPrefix(line, "status"))
		}
		return Status{}, &UnknownMessageError{Type: typ}
	}
}

func parseStatusBody(body string) (Status, error) {
	toks, err := tokenize(body)
	if err != nil {
		return Status{}, err
	}
	if len(toks) == 0 || toks[0] != "{" {
		return Status{}, malformed("status body must start with '{'")
	}
	end, err := skipValue(toks, 0)
	if err != nil {
		return Status{}, err
	}
	if end != len(toks) {
		return Status{}, malformed("trailing data after status map")
	}
	toks = toks[1 : end-1] // 外側の { } を外す

	var (
		st                      Status
		hasPeers, hasBPM, hasBt bool
	)
	for i := 0; i < len(toks); {
		key := toks[i]
		if !strings.HasPrefix(key, ":") || len(key) < 2 {
			return Status{}, malformed("expected keyword, got %q", key)
		}
		if i+1 >= len(toks) {
			return Status{}, malformed("missing value for %s", key)
		}
		next, err := skipValue(toks, i+1)
		if err != nil {
			return Status{}, err
		}
		val := toks[i+1]
		single := next == i+2
		switch key {
		case ":peers":
			n, err := strconv.Atoi(val)
			if !single || err != nil || n < 0 {
				return Status{}, malformed(":peers %q is not a non-negative integer", val)
			}
			st.Peers, hasPeers = n, true
		case ":bpm":
			f, err := parseFinite(val, single)
			if err != nil || f <= 0 {
				return Status{}, malformed(":bpm %q is not a positive number", val)
			}
			st.BPM, hasBPM = f, true
		case ":beat":
			f, err := parseFinite(val, single)
			if err != nil {
				return Status{}, malformed(":beat %q is not a number", val)
			}
			st.Beat, hasBt = f, true
		case ":start":
			n, err := strconv.ParseInt(val, 10, 64)
			if !single || err != nil {
				return Status{}, malformed(":start %q is not an integer", val)
			}
			st.Start = n
		case ":playing":
			if !single || (val != "true" && val != "false") {
				return Status{}, malformed(":playing %q is not a boolean", val)
			}
			b := val == "true"
			st.Playing = &b
		}
		i = next
	}
	switch {
	case !hasPeers:
		return Status{}, malformed("missing :peers")
	case !hasBPM:
		return Status{}, malformed("missing :bpm")
	case !hasBt:
		return Status{}, malformed("missing :beat")
	}
	return st, nil
}

func parseFinite(s string, single bool) (float64, error) {
	if !single {
		return 0, errors.New("not a scalar")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, errors.New("not finite")
	}
	return f, nil
}

// tokenize は edn 風の文字列を、括弧・文字列・それ以外の語に切る。
// 空白とカンマは区切りとして捨てる。
func tokenize(s string) ([]string, error) {
	var toks []string
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == ',':
			i++
		case strings.IndexByte("{}[]()", c) >= 0:
			toks = append(toks, string(c))
			i++
		case c == '"':
			j := i + 1
			for ; j < len(s); j++ {
				if s[j] == '\\' {
					j++
					continue
				}
				if s[j] == '"' {
					break
				}
			}
			if j >= len(s) {
				return nil, malformed("unterminated string")
			}
			toks = append(toks, s[i:j+1])
			i = j + 1
		default:
			j := i
			for j < len(s) && strings.IndexByte(" \t\r\n,{}[]()\"", s[j]) < 0 {
				j++
			}
			toks = append(toks, s[i:j])
			i = j
		}
	}
	return toks, nil
}

// skipValue は toks[i] から始まる値 1 つを読み飛ばし、その次の添字を返す。
func skipValue(toks []string, i int) (int, error) {
	if i >= len(toks) {
		return 0, malformed("unexpected end")
	}
	open := toks[i]
	var closer string
	switch open {
	case "{":
		closer = "}"
	case "[":
		closer = "]"
	case "(":
		closer = ")"
	case "}", "]", ")":
		return 0, malformed("unexpected %q", open)
	default:
		return i + 1, nil
	}
	j := i + 1
	for {
		if j >= len(toks) {
			return 0, malformed("missing %q", closer)
		}
		if toks[j] == closer {
			return j + 1, nil
		}
		next, err := skipValue(toks, j)
		if err != nil {
			return 0, err
		}
		j = next
	}
}
