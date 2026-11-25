package control

import (
	"slices"
	"testing"
)

// features の audio は -audio で音が有効なときだけ出す（ADR-009、FR-185）。
func TestEngineState_AudioFeatureOnlyWhenEnabled(t *testing.T) {
	e := newTestEngine(t.TempDir())
	if got := e.State().Features; slices.Contains(got, "audio") {
		t.Errorf("-audio 無し: Features = %v, want no audio", got)
	}
	e.Audio = true
	got := e.State().Features
	if !slices.Contains(got, "audio") {
		t.Errorf("-audio 有り: Features = %v, want audio", got)
	}
	if !slices.IsSorted(got) {
		t.Errorf("Features = %v, want sorted", got)
	}
}
