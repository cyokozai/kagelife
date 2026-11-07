package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/cyokozai/kagelife/internal/tempo"
)

// maxAVOffsetMS は -av-offset の上限（ミリ秒）。PRD Q-018 の仮の上限に合わせる。
const maxAVOffsetMS = 1000

// parseAVOffset は -av-offset の値（ミリ秒の整数、0〜maxAVOffsetMS）を解釈する。
func parseAVOffset(s string) (time.Duration, error) {
	ms, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("-av-offset: ミリ秒の整数で指定する: %q", s)
	}
	if ms < 0 || ms > maxAVOffsetMS {
		return 0, fmt.Errorf("-av-offset: 0〜%d の範囲で指定する: %d", maxAVOffsetMS, ms)
	}

	return time.Duration(ms) * time.Millisecond, nil
}

// visualPhase は映像に使う拍の位相を返す（PRD FR-156 の最小版）。
// 会場の音は返しより offset だけ遅れて届くので、拍時計を「now − offset」で評価する。
// タップや set_bpm の時刻（入力）はずらさない。
func visualPhase(tp *tempo.Tapper, now time.Time, offset time.Duration) float32 {
	return tp.Phase(now.Add(-offset))
}
