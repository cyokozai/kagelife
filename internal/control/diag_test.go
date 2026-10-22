package control

import (
	"errors"
	"fmt"
	"go/scanner"
	"go/token"
	"strings"
	"testing"
)

const fiveLines = "//kage:unit pixels\npackage main\n\nfunc Fragment() vec4 {\n}"

func syntaxErr(lines ...int) error {
	var el scanner.ErrorList
	for _, l := range lines {
		el.Add(token.Position{Line: l, Column: 3}, fmt.Sprintf("syntax at %d", l))
	}

	return el
}

func TestDiagnose_ScannerErrorListKeepsUserLinesOnly(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", syntaxErr(2, 5, 6, 120))

	got := Diagnose([]byte(fiveLines), err)

	want := []Diagnostic{{Line: 2, Col: 3, Message: "syntax at 2"}, {Line: 5, Col: 3, Message: "syntax at 5"}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("Diagnose = %+v, want %+v", got, want)
	}
}

func TestDiagnose_SemanticErrorString(t *testing.T) {
	err := errors.New("4:14: unexpected identifier: Nope\n300:1: internal thing\n5:2: another")

	got := Diagnose([]byte(fiveLines), err)

	want := []Diagnostic{
		{Line: 4, Col: 14, Message: "unexpected identifier: Nope"},
		{Line: 5, Col: 2, Message: "another"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("Diagnose = %+v, want %+v", got, want)
	}
}

func TestDiagnose_UnparsableLinesBecomeLineZero(t *testing.T) {
	err := errors.New("shader: something odd happened")

	got := Diagnose([]byte(fiveLines), err)

	want := []Diagnostic{{Line: 0, Col: 0, Message: "shader: something odd happened"}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("Diagnose = %+v, want %+v", got, want)
	}
}

func TestDiagnose_AtMostTen(t *testing.T) {
	var lines []string
	for i := 1; i <= 15; i++ {
		lines = append(lines, "x")
	}
	src := strings.Join(lines, "\n")
	var msgs []string
	for i := 1; i <= 15; i++ {
		msgs = append(msgs, fmt.Sprintf("%d:1: e%d", i, i))
	}

	got := Diagnose([]byte(src), errors.New(strings.Join(msgs, "\n")))

	if len(got) != 10 {
		t.Fatalf("len = %d, want 10", len(got))
	}
	if got[9].Line != 10 {
		t.Errorf("10 件目の行 = %d, want 10（先頭から順に残す）", got[9].Line)
	}
}

func TestDiagnose_AllOutOfRangeFallsBackToFirstError(t *testing.T) {
	err := syntaxErr(200, 201)

	got := Diagnose([]byte(fiveLines), err)

	if len(got) != 1 || got[0].Line != 0 || !strings.Contains(got[0].Message, "syntax at 200") {
		t.Errorf("Diagnose = %+v, want 行 0 の 1 件（先頭の誤り）", got)
	}
}

func TestSummarize(t *testing.T) {
	one := Summarize([]Diagnostic{{Line: 5, Col: 14, Message: "unexpected identifier: Nope"}})
	if !strings.Contains(one, "5:14: unexpected identifier: Nope") {
		t.Errorf("Summarize(1 件) = %q", one)
	}

	three := Summarize([]Diagnostic{{Line: 1, Col: 1, Message: "a"}, {Line: 2, Col: 1, Message: "b"}, {Line: 3, Col: 1, Message: "c"}})
	if !strings.Contains(three, "1:1: a") || !strings.Contains(three, "2 more") {
		t.Errorf("Summarize(3 件) = %q", three)
	}

	zero := Summarize([]Diagnostic{{Message: "odd"}})
	if !strings.Contains(zero, "odd") || strings.Contains(zero, "0:0") {
		t.Errorf("Summarize(行 0) = %q", zero)
	}
}
