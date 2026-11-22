// Package audio は -audio の音声の取り込み（malgo）と軽い解析（FFT・オンセット、純 Go）を受け持つ（ADR-009）。
//
// 解析（Analyzer）はデバイスに触れず、PCM の float32 を受け取って帯域レベルとオンセットを返す。
// 取り込み（Capture）は malgo のコールバックで Analyzer を回し、最新値だけを Latest に置く（ADR-002）。
package audio

import "math"

// 解析の決まり（2026-11-09 のユーザー決定 D4）。
const (
	// SampleRate は取り込みの標本化周波数。
	SampleRate = 48000
	// FFTSize は FFT の点数。HopSize ずつずらす（半分の重なり）。
	FFTSize = 1024
	// HopSize は解析の間隔（標本数）。
	HopSize = FFTSize / 2

	// autoGainWindow は自動ゲインで最大を取る直近の時間（秒）。
	autoGainWindow = 10.0
	// attackTau・releaseTau はならしの時定数（秒）。上がりは速く、下がりはゆっくり。
	attackTau  = 0.010
	releaseTau = 0.300
	// onsetTau はオンセットの値が 1 から 0 へ減る時定数（秒）。
	onsetTau = 0.150

	// gainFloor は自動ゲインの分母の下限。無音や小さな雑音を 1 まで持ち上げないため。
	// 振幅 0.001（-60 dBFS）の正弦波の帯域レベルがおよそこの値になる。
	gainFloor = 1e-3
	// fluxGain はオンセット検出で振幅を対数に圧縮するときの係数（log1p(fluxGain*m)）。
	fluxGain = 1000
	// onsetWindow はオンセットの閾値に使うスペクトルの差分（flux）の平均を取る直近の時間（秒）。
	onsetWindow = 0.5
	// onsetRatio・onsetFloor は閾値 = onsetFloor + onsetRatio × 直近の flux の平均。
	onsetRatio = 1.5
	onsetFloor = 1.0
)

// 帯域の番号。
const (
	bandLow = iota
	bandMid
	bandHigh
	numBands
)

// bandEdges は帯域の下限と上限（Hz）。ビンの中心周波数が [下限, 上限) に入れば、その帯域に数える。
var bandEdges = [numBands][2]float64{
	bandLow:  {20, 150},
	bandMid:  {150, 2000},
	bandHigh: {2000, 16000},
}

// Levels は 1 回の解析の結果。どれも 0〜1。
type Levels struct {
	Bass  float32 // 低 20〜150 Hz
	Mid   float32 // 中 150 Hz〜2 kHz
	High  float32 // 高 2〜16 kHz
	Onset float32 // 立ち上がりで 1 にし、時定数 150 ms で 0 へ減らす
}

func (l Levels) band(b int) float32 {
	switch b {
	case bandLow:
		return l.Bass
	case bandMid:
		return l.Mid
	default:
		return l.High
	}
}

// Analyzer は PCM（モノラル、float32、-1〜1）から帯域レベルとオンセットを求める。
// 時間は標本の数で数えるので、実際の時刻に依らず同じ入力なら同じ結果になる。
// 一つのゴルーチン（取り込みのコールバック）からだけ使うこと。
type Analyzer struct {
	fft    *fft
	window []float64
	buf    []float32 // 直近 FFTSize 標本
	pos    int       // 今の区切りに入った標本の数（0〜HopSize-1）
	re, im []float64

	binBand  []int     // ビンごとの帯域（-1 はどこにも入らない）
	prevLog  []float64 // 前回の log1p(fluxGain*m)
	fluxBins int       // flux を数えるビンの上限（16 kHz まで）

	raw     [numBands]float64 // 直近の帯域レベル（自動ゲインの前）
	hist    [numBands][]float64
	histPos int
	smooth  [numBands]float64

	fluxHist []float64
	fluxPos  int
	fluxSum  float64
	onset    float64

	attack, release, onsetDecay float64
	out                         Levels
}

// NewAnalyzer は標本化周波数 rate の Analyzer を作る。
func NewAnalyzer(rate int) *Analyzer {
	hop := float64(HopSize) / float64(rate)
	a := &Analyzer{
		fft:        newFFT(FFTSize),
		window:     make([]float64, FFTSize),
		buf:        make([]float32, FFTSize),
		re:         make([]float64, FFTSize),
		im:         make([]float64, FFTSize),
		binBand:    make([]int, FFTSize/2+1),
		prevLog:    make([]float64, FFTSize/2+1),
		fluxHist:   make([]float64, max(1, int(math.Round(onsetWindow/hop)))),
		attack:     math.Exp(-hop / attackTau),
		release:    math.Exp(-hop / releaseTau),
		onsetDecay: math.Exp(-hop / onsetTau),
	}
	for i := range a.window {
		a.window[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(FFTSize))
	}
	binHz := float64(rate) / FFTSize
	for k := range a.binBand {
		f := float64(k) * binHz
		a.binBand[k] = -1
		for b, e := range bandEdges {
			if f >= e[0] && f < e[1] {
				a.binBand[k] = b
			}
		}
		if f < bandEdges[bandHigh][1] {
			a.fluxBins = k + 1
		}
	}
	n := max(1, int(math.Ceil(autoGainWindow/hop)))
	for b := range a.hist {
		a.hist[b] = make([]float64, n)
	}

	return a
}

// Write は samples を足し、区切り（HopSize 標本）ごとに解析して、直近の結果を返す。
// 区切りに届かなければ前回の結果をそのまま返す。
func (a *Analyzer) Write(samples []float32) Levels {
	base := FFTSize - HopSize
	for _, s := range samples {
		if math.IsNaN(float64(s)) { // NaN は無音として扱う
			s = 0
		}
		a.buf[base+a.pos] = s
		a.pos++
		if a.pos == HopSize {
			a.analyze()
			copy(a.buf, a.buf[HopSize:])
			a.pos = 0
		}
	}

	return a.out
}

// analyze は直近 FFTSize 標本を解析して out を更新する。
func (a *Analyzer) analyze() {
	for i, s := range a.buf {
		a.re[i] = float64(s) * a.window[i]
		a.im[i] = 0
	}
	a.fft.transform(a.re, a.im)

	// 振幅 A の正弦波がおよそ A になるように縮める（Hann の利得 0.5 と片側の 1/2）。
	const scale = 4.0 / FFTSize
	var energy [numBands]float64
	flux := 0.0
	for k := 1; k < len(a.binBand); k++ {
		m := math.Hypot(a.re[k], a.im[k]) * scale
		if b := a.binBand[k]; b >= 0 {
			energy[b] += m * m
		}
		if k < a.fluxBins {
			lg := math.Log1p(fluxGain * m)
			if d := lg - a.prevLog[k]; d > 0 {
				flux += d
			}
			a.prevLog[k] = lg
		}
	}

	var lv [numBands]float32
	for b := range numBands {
		a.raw[b] = math.Sqrt(energy[b])
		a.hist[b][a.histPos] = a.raw[b]
		ref := gainFloor
		for _, v := range a.hist[b] {
			ref = max(ref, v)
		}
		norm := min(a.raw[b]/ref, 1)
		coef := a.release
		if norm > a.smooth[b] {
			coef = a.attack
		}
		a.smooth[b] = norm + coef*(a.smooth[b]-norm)
		lv[b] = clamp01(a.smooth[b])
	}
	a.histPos = (a.histPos + 1) % len(a.hist[0])

	// オンセット: flux が直近の平均より十分に大きければ立ち上がり。今回の flux は閾値の平均に含めない。
	mean := a.fluxSum / float64(len(a.fluxHist))
	if flux > onsetFloor+onsetRatio*mean {
		a.onset = 1
	} else {
		a.onset *= a.onsetDecay
	}
	a.fluxSum += flux - a.fluxHist[a.fluxPos]
	a.fluxHist[a.fluxPos] = flux
	a.fluxPos = (a.fluxPos + 1) % len(a.fluxHist)
	if a.fluxSum < 0 { // 足し引きの丸めで負にならないように
		a.fluxSum = 0
	}

	a.out = Levels{Bass: lv[bandLow], Mid: lv[bandMid], High: lv[bandHigh], Onset: clamp01(a.onset)}
}

func clamp01(v float64) float32 {
	switch {
	case v < 0 || math.IsNaN(v):
		return 0
	case v > 1:
		return 1
	default:
		return float32(v)
	}
}
