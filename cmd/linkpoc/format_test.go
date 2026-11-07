package main

import (
	"strings"
	"testing"
	"time"

	"github.com/cyokozai/kagelife/internal/carabiner"
)

var now = time.Date(2026, 11, 4, 21, 3, 4, 567_000_000, time.Local)

func synced(beat, bpm float64) carabiner.Estimate {
	e := carabiner.NewEstimator(4)
	e.SetConnected(true)
	e.Update(carabiner.Status{Peers: 1, BPM: bpm, Beat: beat}, now)
	return e.Estimate(now)
}

func TestPhaseBar(t *testing.T) {
	for _, tc := range []struct {
		phase float64
		want  string
	}{
		{0, "[........]"},
		{0.5, "[####....]"},
		{0.99, "[#######.]"},
		{1, "[########]"},
		{-0.1, "[........]"},
	} {
		if got := phaseBar(tc.phase, 8); got != tc.want {
			t.Errorf("phaseBar(%v) = %q, want %q", tc.phase, got, tc.want)
		}
	}
}

func TestCrossedBeat(t *testing.T) {
	for _, tc := range []struct {
		name      string
		prev, cur carabiner.Estimate
		want      bool
	}{
		{"拍の頭をまたぐ", synced(9.95, 120), synced(10.05, 120), true},
		{"同じ拍の中", synced(10.1, 120), synced(10.4, 120), false},
		{"負の拍から 0 へ", synced(-0.05, 120), synced(0.02, 120), true},
		{"前回が status なし", carabiner.Estimate{}, synced(10.05, 120), false},
		{"今回が status なし", synced(9.95, 120), carabiner.Estimate{}, false},
	} {
		if got := crossedBeat(tc.prev, tc.cur); got != tc.want {
			t.Errorf("%s: crossedBeat = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestFormatLineSynced(t *testing.T) {
	e := synced(10.25, 120)
	playing := true
	e.Playing = &playing

	line := formatLine(now, e, false)
	for _, want := range []string{"21:03:04.567", "synced", "peers=1", "bpm=120.00", "beat=10.25", "bar=3.3", "[##......]", "play"} {
		if !strings.Contains(line, want) {
			t.Errorf("formatLine = %q, %q を含まない", line, want)
		}
	}
	if strings.Contains(line, "●") {
		t.Errorf("拍の頭をまたいでいないのに印がある: %q", line)
	}
}

func TestFormatLineMarksBeatHeadWithOffset(t *testing.T) {
	e := synced(10.05, 120) // 拍の頭から 0.05 拍 = 25ms

	line := formatLine(now, e, true)
	if !strings.Contains(line, "●") {
		t.Errorf("印が無い: %q", line)
	}
	if !strings.Contains(line, "+25ms") {
		t.Errorf("拍の頭からの遅れ +25ms が無い: %q", line)
	}
}

func TestFormatLineStates(t *testing.T) {
	alone := carabiner.NewEstimator(4)
	alone.SetConnected(true)
	alone.Update(carabiner.Status{Peers: 0, BPM: 120, Beat: 1}, now)

	waiting := carabiner.NewEstimator(4)
	waiting.SetConnected(true)

	for _, tc := range []struct {
		name string
		e    carabiner.Estimate
		want []string
	}{
		{"未接続", carabiner.Estimate{}, []string{"offline", "beat=---", "play=-"}},
		{"接続済み・status 未着", waiting.Estimate(now), []string{"waiting", "beat=---"}},
		{"peers 0", alone.Estimate(now), []string{"alone", "peers=0", "bpm=120.00"}},
	} {
		line := formatLine(now, tc.e, false)
		for _, w := range tc.want {
			if !strings.Contains(line, w) {
				t.Errorf("%s: formatLine = %q, %q を含まない", tc.name, line, w)
			}
		}
	}
}
