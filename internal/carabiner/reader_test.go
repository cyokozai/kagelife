package carabiner

import (
	"errors"
	"io"
	"testing"
)

func readAll(t *testing.T, lr *LineReader) []string {
	t.Helper()
	var got []string
	for {
		line, err := lr.ReadLine()
		if errors.Is(err, io.EOF) {
			return got
		}
		if err != nil {
			t.Fatalf("ReadLine: %v", err)
		}
		got = append(got, line)
	}
}

func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestLineReaderTwoLinesInOnePacket(t *testing.T) {
	pr, pw := io.Pipe()
	go func() {
		_, _ = pw.Write([]byte("status { :peers 0 :bpm 120 :start 1 :beat 2 }\nstatus { :peers 1 :bpm 121 :start 1 :beat 3 }\n"))
		_ = pw.Close()
	}()

	got := readAll(t, NewLineReader(pr))
	want := []string{
		"status { :peers 0 :bpm 120 :start 1 :beat 2 }",
		"status { :peers 1 :bpm 121 :start 1 :beat 3 }",
	}
	if !equalLines(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}
}

func TestLineReaderSplitInsideLine(t *testing.T) {
	pr, pw := io.Pipe()
	go func() {
		for _, chunk := range []string{"stat", "us { :peers 0 :bp", "m 120 :start 1 :beat 2 }\r", "\nbad-in", "put x\n"} {
			_, _ = pw.Write([]byte(chunk))
		}
		_ = pw.Close()
	}()

	got := readAll(t, NewLineReader(pr))
	want := []string{"status { :peers 0 :bpm 120 :start 1 :beat 2 }", "bad-input x"}
	if !equalLines(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}
}

func TestLineReaderSkipsEmptyLinesAndDropsUnterminatedTail(t *testing.T) {
	pr, pw := io.Pipe()
	go func() {
		_, _ = pw.Write([]byte("\n\nstatus { :peers 0 :bpm 120 :start 1 :beat 2 }\nstatus { :pe"))
		_ = pw.Close()
	}()

	got := readAll(t, NewLineReader(pr))
	want := []string{"status { :peers 0 :bpm 120 :start 1 :beat 2 }"}
	if !equalLines(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}
}
