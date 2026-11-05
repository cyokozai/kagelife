package carabiner

import (
	"math"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func status(peers int, bpm, beat float64) Status {
	return Status{Peers: peers, BPM: bpm, Beat: beat}
}

func TestEstimatorAdvancesOneBeatPerHalfSecondAt120(t *testing.T) {
	e := NewEstimator(4)
	e.SetConnected(true)
	e.Update(status(1, 120, 10), t0)

	got := e.Estimate(t0.Add(500 * time.Millisecond))
	if !near(got.Beat, 11) {
		t.Errorf("0.5 秒後の Beat = %v, want 11", got.Beat)
	}
	if !got.Synced {
		t.Error("Synced = false, want true")
	}
	if got.BPM != 120 || got.Peers != 1 {
		t.Errorf("BPM, Peers = %v, %d", got.BPM, got.Peers)
	}
}

func TestEstimatorBarPositionAndPhase(t *testing.T) {
	e := NewEstimator(4)
	e.SetConnected(true)
	e.Update(status(1, 120, 9.0), t0)

	// 9.0 + 0.625 秒 × 2 拍/秒 = 10.25 拍。4 拍の小節で 3 小節目の 3 拍目、位相 0.25。
	got := e.Estimate(t0.Add(625 * time.Millisecond))
	if !near(got.Beat, 10.25) {
		t.Fatalf("Beat = %v, want 10.25", got.Beat)
	}
	if !near(got.BarPhase, 2.25) {
		t.Errorf("BarPhase = %v, want 2.25", got.BarPhase)
	}
	if got.Bar != 3 || got.BeatInBar != 3 {
		t.Errorf("Bar.BeatInBar = %d.%d, want 3.3", got.Bar, got.BeatInBar)
	}
	if !near(got.Phase, 0.25) {
		t.Errorf("Phase = %v, want 0.25", got.Phase)
	}
}

func TestEstimatorNegativeBeatWrapsIntoBar(t *testing.T) {
	e := NewEstimator(4)
	e.SetConnected(true)
	e.Update(status(1, 120, -1.25), t0)

	got := e.Estimate(t0)
	// -1.25 は 1 つ前の小節（0 小節目）の 3.75 拍。位相は 0.75。
	if !near(got.BarPhase, 2.75) || got.Bar != 0 || got.BeatInBar != 3 || !near(got.Phase, 0.75) {
		t.Errorf("Estimate = %+v", got)
	}
}

func TestEstimatorExtrapolatesWithNewBPMAfterChange(t *testing.T) {
	e := NewEstimator(4)
	e.SetConnected(true)
	e.Update(status(1, 120, 0), t0)
	// 1 秒後に bpm が 60 に変わり、その時点の beat が 2 と報告された。
	t1 := t0.Add(time.Second)
	e.Update(status(1, 60, 2), t1)

	got := e.Estimate(t1.Add(1500 * time.Millisecond))
	if !near(got.Beat, 3.5) {
		t.Errorf("bpm 60 で 1.5 秒後の Beat = %v, want 3.5", got.Beat)
	}
	if got.BPM != 60 {
		t.Errorf("BPM = %v, want 60", got.BPM)
	}
}

func TestEstimatorQuantumIsConfigurable(t *testing.T) {
	e := NewEstimator(3)
	e.SetConnected(true)
	e.Update(status(1, 120, 7.5), t0)

	got := e.Estimate(t0)
	if !near(got.BarPhase, 1.5) || got.Bar != 3 || got.BeatInBar != 2 {
		t.Errorf("quantum 3: Estimate = %+v", got)
	}
}

func TestEstimatorNotSyncedWithoutPeersOrConnection(t *testing.T) {
	e := NewEstimator(4)

	if got := e.Estimate(t0); got.Synced || got.Connected || got.HasStatus {
		t.Errorf("初期状態: %+v, want 同期していない", got)
	}

	e.SetConnected(true)
	e.Update(status(0, 120, 1), t0)
	if got := e.Estimate(t0); got.Synced || !got.HasStatus || !got.Connected {
		t.Errorf("peers 0: %+v, want Synced=false HasStatus=true Connected=true", got)
	}

	e.Update(status(2, 120, 1), t0)
	if got := e.Estimate(t0); !got.Synced {
		t.Errorf("peers 2: %+v, want Synced=true", got)
	}

	e.SetConnected(false)
	if got := e.Estimate(t0); got.Synced || got.Connected {
		t.Errorf("切断後: %+v, want Synced=false Connected=false", got)
	}
}

func TestEstimatorReconnectNeedsFreshStatus(t *testing.T) {
	e := NewEstimator(4)
	e.SetConnected(true)
	e.Update(status(1, 120, 1), t0)
	e.SetConnected(false)
	e.SetConnected(true)

	if got := e.Estimate(t0); got.Synced || got.HasStatus {
		t.Errorf("再接続直後（status 未着）: %+v, want HasStatus=false", got)
	}
}

func TestEstimatorKeepsPlaying(t *testing.T) {
	e := NewEstimator(4)
	e.SetConnected(true)
	playing := true
	st := status(1, 120, 0)
	st.Playing = &playing
	e.Update(st, t0)

	got := e.Estimate(t0)
	if got.Playing == nil || !*got.Playing {
		t.Errorf("Playing = %v, want true", got.Playing)
	}
	playing = false // 呼び出し側の変数を書き換えても推定器の値は変わらない
	if got := e.Estimate(t0); got.Playing == nil || !*got.Playing {
		t.Errorf("Playing がコピーされていない: %v", got.Playing)
	}
}
