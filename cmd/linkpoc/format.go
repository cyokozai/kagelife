package main

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/cyokozai/kagelife/internal/carabiner"
)

const phaseBarWidth = 8

// phaseBar は拍内の位相 [0, 1] を width 文字のバーにする。
func phaseBar(phase float64, width int) string {
	n := int(math.Floor(phase * float64(width)))
	n = max(0, min(width, n))
	return "[" + strings.Repeat("#", n) + strings.Repeat(".", width-n) + "]"
}

// crossedBeat は prev から cur までの間に拍の頭（整数の beat）をまたいだかを返す。
func crossedBeat(prev, cur carabiner.Estimate) bool {
	if !prev.HasStatus || !cur.HasStatus {
		return false
	}
	return math.Floor(cur.Beat) > math.Floor(prev.Beat)
}

// stateLabel は接続と同期の状態を短い語にする。
func stateLabel(e carabiner.Estimate) string {
	switch {
	case !e.Connected:
		return "offline"
	case !e.HasStatus:
		return "waiting"
	case !e.Synced:
		return "alone"
	default:
		return "synced"
	}
}

func playLabel(p *bool) string {
	switch {
	case p == nil:
		return "-"
	case *p:
		return "on"
	default:
		return "off"
	}
}

// formatLine は表示 1 行を作る。mark が true なら拍の頭をまたいだ印と、
// 拍の頭からの経過（表示の遅れ）を付ける。
func formatLine(now time.Time, e carabiner.Estimate, mark bool) string {
	ts := now.Format("15:04:05.000")
	if !e.HasStatus {
		return fmt.Sprintf("%s %-7s peers=- bpm=------ beat=---       bar=---    %s play=%s",
			ts, stateLabel(e), phaseBar(0, phaseBarWidth), playLabel(e.Playing))
	}
	line := fmt.Sprintf("%s %-7s peers=%d bpm=%6.2f beat=%-9s bar=%-6s %s play=%-3s",
		ts, stateLabel(e), e.Peers, e.BPM,
		fmt.Sprintf("%.2f", e.Beat),
		fmt.Sprintf("%d.%d", e.Bar, e.BeatInBar),
		phaseBar(e.Phase, phaseBarWidth), playLabel(e.Playing))
	if mark {
		late := e.Phase * 60 / e.BPM * 1000
		line += fmt.Sprintf(" ● +%dms", int(math.Round(late)))
	}
	return line
}
