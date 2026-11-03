package carabiner

import (
	"math"
	"sync"
	"time"
)

// DefaultQuantum は小節の長さ（拍）の既定値。
const DefaultQuantum = 4.0

// Estimate はある時刻での拍の推定値。
type Estimate struct {
	Connected bool // Carabiner と TCP がつながっている
	HasStatus bool // 今の接続で status を 1 度以上受けた
	Synced    bool // Connected && HasStatus && Peers > 0（Link の相手と拍を共有している）

	Peers   int
	BPM     float64
	Playing *bool // Start/Stop Sync が無効なら nil

	Beat      float64 // 外挿した拍の位置
	BarPhase  float64 // 小節内の位置 [0, quantum)
	Bar       int     // 小節番号（1 始まり。Beat が負なら 0 以下）
	BeatInBar int     // 小節内の拍番号（1 始まり）
	Phase     float64 // 拍内の位相 [0, 1)
}

// Estimator は最新の (ローカル単調時刻, beat, bpm) の組から現在の拍を外挿する。
// beat(now) = beat0 + (now - t0) 秒 × bpm / 60。
// 時刻は引数で受けるので、テストでは偽の時刻を渡せる。並行に呼んでよい。
type Estimator struct {
	quantum float64

	mu        sync.Mutex
	connected bool
	has       bool
	at        time.Time
	st        Status
}

// NewEstimator は小節の長さ quantum（拍）を指定して作る。0 以下なら DefaultQuantum を使う。
func NewEstimator(quantum float64) *Estimator {
	if !(quantum > 0) {
		quantum = DefaultQuantum
	}
	return &Estimator{quantum: quantum}
}

// Quantum は小節の長さ（拍）を返す。
func (e *Estimator) Quantum() float64 { return e.quantum }

// SetConnected は接続状態を記録する。状態が変わったら前の接続の status を捨てる。
func (e *Estimator) SetConnected(c bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.connected != c {
		e.has = false
		e.st = Status{}
	}
	e.connected = c
}

// Update は status を受け取ったローカル時刻 at と組にして記録する。
// at は time.Now() 由来（単調時計の読みを含む）の値を渡す。
func (e *Estimator) Update(st Status, at time.Time) {
	if st.Playing != nil {
		p := *st.Playing
		st.Playing = &p
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.st = st
	e.at = at
	e.has = true
}

// Estimate は now での推定値を返す。status をまだ受けていなければ拍の値は 0 のまま。
func (e *Estimator) Estimate(now time.Time) Estimate {
	e.mu.Lock()
	connected, has, at, st := e.connected, e.has, e.at, e.st
	e.mu.Unlock()

	est := Estimate{Connected: connected, HasStatus: has}
	if !has {
		return est
	}
	est.Peers = st.Peers
	est.BPM = st.BPM
	if st.Playing != nil {
		p := *st.Playing
		est.Playing = &p
	}
	est.Synced = connected && st.Peers > 0

	beat := st.Beat + now.Sub(at).Seconds()*st.BPM/60
	est.Beat = beat
	est.BarPhase = floorMod(beat, e.quantum)
	est.Bar = int(math.Floor(beat/e.quantum)) + 1
	est.BeatInBar = int(math.Floor(est.BarPhase)) + 1
	est.Phase = floorMod(beat, 1)
	return est
}

// floorMod は x mod m を [0, m) で返す。
func floorMod(x, m float64) float64 {
	r := math.Mod(x, m)
	if r < 0 {
		r += m
	}
	if r >= m { // 丸めで m ちょうどになる場合
		r = 0
	}
	return r
}
