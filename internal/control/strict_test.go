package control

import (
	"sort"
	"strings"
	"testing"
)

// 未知のフィールドは 400 invalid_request で拒否し、message にフィールド名を含める。
func TestUnknownFieldsRejected(t *testing.T) {
	h := newHarness(t, true, "a")

	cases := []struct {
		method, path, body, field string
	}{
		{"PUT", "/v1/shaders/x", jsonBody(map[string]string{"source": src("// ok"), "sorce": "typo"}), "sorce"},
		{"POST", "/v1/switch", `{"name":"a","quantize":"bar"}`, "quantize"},
		{"POST", "/v1/crossfade", `{"name":"a","beats":4,"curve":"ease"}`, "curve"},
		{"POST", "/v1/bpm", `{"bpm":128,"tap":true}`, "tap"},
	}
	for _, c := range cases {
		st, body := h.do(c.method, c.path, c.body)
		assertError(t, st, body, 400, "invalid_request")
		if msg, _ := body["message"].(string); !strings.Contains(msg, c.field) {
			t.Errorf("%s %s: message = %q, want it to contain %q", c.method, c.path, msg, c.field)
		}
	}

	// 拒否された要求は状態を変えない
	_, s := h.do("GET", "/v1/state", "")
	if s["bpm"] != 120.0 || s["active"] != "a" {
		t.Errorf("state = %v, want unchanged", s)
	}
}

// 本文が大きすぎるときは未知のフィールドより 413 too_large を優先する。
func TestUnknownFieldsTooLargeStillWins(t *testing.T) {
	h := newHarness(t, true, "a")

	big := `{"unknown":"` + strings.Repeat("x", maxBodyBytes+1) + `"}`
	st, body := h.do("POST", "/v1/switch", big)
	assertError(t, st, body, 413, "too_large")
}

// state の features は常に配列で、このビルドが持つ任意機能を並べる。
// llm_slots は -llm-slots を指定していなくても載る（設定済みかは state の llm_slots で見る）。
func TestGetState_FeaturesIsArray(t *testing.T) {
	h := newHarness(t, true)

	_, body := h.do("GET", "/v1/state", "")

	v, ok := body["features"]
	if !ok {
		t.Fatalf("features が無い: %v", body)
	}
	if got := jsonBody(v); got != `["llm_slots"]` {
		t.Errorf("features = %s, want [\"llm_slots\"]", got)
	}
}

// Engine.State の Features はソート済みで、定義元のスライスと共有しない。
func TestEngineState_FeaturesSortedCopy(t *testing.T) {
	e := newTestEngine(t.TempDir())

	got := e.State().Features
	if got == nil {
		t.Fatal("Features が nil")
	}
	if !sort.StringsAreSorted(got) {
		t.Errorf("Features = %v, want sorted", got)
	}
	if len(got) != len(features) {
		t.Errorf("len(Features) = %d, want %d", len(got), len(features))
	}
	if len(got) > 0 {
		got[0] = "mutated"
		if features[0] == "mutated" {
			t.Error("Features が定義元のスライスを共有している")
		}
	}
}
