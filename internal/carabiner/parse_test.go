package carabiner

import (
	"errors"
	"testing"
)

func TestParseStatusReadmeExample(t *testing.T) {
	st, err := ParseStatus("status { :peers 0 :bpm 120.000000 :start 73743731220 :beat 597.737570 }")
	if err != nil {
		t.Fatalf("ParseStatus: %v", err)
	}
	if st.Peers != 0 {
		t.Errorf("Peers = %d, want 0", st.Peers)
	}
	if st.BPM != 120 {
		t.Errorf("BPM = %v, want 120", st.BPM)
	}
	if st.Start != 73743731220 {
		t.Errorf("Start = %d, want 73743731220", st.Start)
	}
	if st.Beat != 597.737570 {
		t.Errorf("Beat = %v, want 597.737570", st.Beat)
	}
	if st.Playing != nil {
		t.Errorf("Playing = %v, want nil（:playing が無い）", *st.Playing)
	}
}

func TestParseStatusWithPlaying(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"status { :peers 1 :bpm 128.000000 :start 1 :beat 2.5 :playing true }", true},
		{"status { :peers 1 :bpm 128.000000 :start 1 :beat 2.5 :playing false }", false},
	} {
		st, err := ParseStatus(tc.in)
		if err != nil {
			t.Fatalf("ParseStatus(%q): %v", tc.in, err)
		}
		if st.Playing == nil {
			t.Fatalf("ParseStatus(%q): Playing = nil, want %v", tc.in, tc.want)
		}
		if *st.Playing != tc.want {
			t.Errorf("ParseStatus(%q): Playing = %v, want %v", tc.in, *st.Playing, tc.want)
		}
		if st.Peers != 1 || st.BPM != 128 || st.Beat != 2.5 {
			t.Errorf("ParseStatus(%q) = %+v", tc.in, st)
		}
	}
}

func TestParseStatusKeyOrderAndUnknownKeys(t *testing.T) {
	in := `status {:beat 3.25, :future-key "a b } c" :nested {:x [1 2 {:y 3}]} :bpm 99.5 :playing true :start -5 :peers 2 :flag nil}`
	st, err := ParseStatus(in)
	if err != nil {
		t.Fatalf("ParseStatus: %v", err)
	}
	if st.Peers != 2 || st.BPM != 99.5 || st.Start != -5 || st.Beat != 3.25 {
		t.Errorf("ParseStatus = %+v", st)
	}
	if st.Playing == nil || !*st.Playing {
		t.Errorf("Playing = %v, want true", st.Playing)
	}
}

func TestParseStatusTrailingNewline(t *testing.T) {
	st, err := ParseStatus("status { :peers 3 :bpm 140 :start 0 :beat -1.5 }\r\n")
	if err != nil {
		t.Fatalf("ParseStatus: %v", err)
	}
	if st.Peers != 3 || st.BPM != 140 || st.Beat != -1.5 {
		t.Errorf("ParseStatus = %+v", st)
	}
}

func TestParseStatusErrorResponses(t *testing.T) {
	for _, tc := range []struct {
		in, kind, detail string
	}{
		{"bad-input status-please", "bad-input", "status-please"},
		{"unsupported enable-start-stop-sync", "unsupported", "enable-start-stop-sync"},
		{"bad-input", "bad-input", ""},
	} {
		_, err := ParseStatus(tc.in)
		var re *ResponseError
		if !errors.As(err, &re) {
			t.Fatalf("ParseStatus(%q) err = %v, want *ResponseError", tc.in, err)
		}
		if re.Kind != tc.kind || re.Detail != tc.detail {
			t.Errorf("ParseStatus(%q) = {Kind:%q Detail:%q}, want {%q %q}", tc.in, re.Kind, re.Detail, tc.kind, tc.detail)
		}
	}
}

func TestParseStatusOtherMessage(t *testing.T) {
	_, err := ParseStatus("version \"1.2.0\"")
	var um *UnknownMessageError
	if !errors.As(err, &um) {
		t.Fatalf("err = %v, want *UnknownMessageError", err)
	}
	if um.Type != "version" {
		t.Errorf("Type = %q, want version", um.Type)
	}
}

func TestParseStatusMalformed(t *testing.T) {
	for _, in := range []string{
		"",
		"status",
		"status { :peers 0 :bpm 120 :start 1 :beat 2",     // 閉じ括弧なし
		"status :peers 0 :bpm 120 :start 1 :beat 2 }",     // 開き括弧なし
		"status { :peers 0 :bpm abc :start 1 :beat 2 }",   // 数値でない
		"status { :peers 0.5 :bpm 120 :start 1 :beat 2 }", // peers が整数でない
		"status { :peers 0 :start 1 :beat 2 }",            // bpm が無い
		"status { :peers 0 :bpm 120 :start 1 }",           // beat が無い
		"status { :bpm 120 :start 1 :beat 2 }",            // peers が無い
		"status { :peers 0 :bpm 120 :start 1 :beat 2 :playing maybe }",
		"status { :peers 0 :bpm 120 :start 1 :beat }",        // 値が無い
		"status { peers 0 :bpm 120 :start 1 :beat 2 }",       // キーでない
		"status { :peers 0 :bpm 120 :start 1 :beat 2 } junk", // 後ろにごみ
		"status { :peers 0 :bpm NaN :start 1 :beat 2 }",      // 有限でない
		"status { :peers 0 :bpm -10 :start 1 :beat 2 }",      // bpm が正でない
	} {
		_, err := ParseStatus(in)
		if !errors.Is(err, ErrMalformed) {
			t.Errorf("ParseStatus(%q) err = %v, want ErrMalformed", in, err)
		}
	}
}
