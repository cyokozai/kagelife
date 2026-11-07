package main

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/cyokozai/kagelife/internal/uniform"
)

// -render-scale の範囲。
const (
	minRenderScale = 0.25
	maxRenderScale = 1.0
	// frameStatsWindow はフレーム時間の最大値を見る直近の時間。
	frameStatsWindow = 5 * time.Second
)

// parseRenderScale は -render-scale の値（minRenderScale〜maxRenderScale）を解釈する。
func parseRenderScale(s string) (float64, error) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) {
		return 0, fmt.Errorf("-render-scale: 数で指定する: %q", s)
	}
	if v < minRenderScale || v > maxRenderScale {
		return 0, fmt.Errorf("-render-scale: %.2f〜%.1f の範囲で指定する: %v", minRenderScale, maxRenderScale, v)
	}

	return v, nil
}

// scaledSize は画面 w x h を s 倍した描画先の大きさを返す（四捨五入、1 未満にはしない）。
func scaledSize(w, h int, s float64) (int, int) {
	sw := max(1, int(math.Round(float64(w)*s)))
	sh := max(1, int(math.Round(float64(h)*s)))

	return sw, sh
}

// frameParams は 1 フレームの uniform を組み立てるための値。
type frameParams struct {
	ScreenW, ScreenH int     // 画面の大きさ
	Scale            float64 // -render-scale
	CursorX, CursorY int     // 画面の座標のカーソル
	Elapsed          time.Duration
	Beat             float32
	Frame            int
	Random           float32
}

// shaderInput はシェーダーに渡す uniform の値を組み立てる。
// Resolution は描画先（画面 × Scale）の大きさにし、Cursor も同じ倍率で縮める。
func shaderInput(p frameParams) uniform.Input {
	w, h := scaledSize(p.ScreenW, p.ScreenH, p.Scale)

	return uniform.Input{
		Time:    float32(p.Elapsed.Seconds()),
		Width:   w,
		Height:  h,
		Beat:    p.Beat,
		CursorX: int(math.Round(float64(p.CursorX) * p.Scale)),
		CursorY: int(math.Round(float64(p.CursorY) * p.Scale)),
		Frame:   p.Frame,
		Random:  p.Random,
	}
}

// frameSample は 1 フレームの終わりの時刻と、前のフレームからの間隔。
type frameSample struct {
	end time.Time
	dt  time.Duration
}

// frameStats は直近 window のフレーム時間の最大値を求める（負荷試験で HUD に出す）。
// ゲームループのゴルーチンからだけ使う。
type frameStats struct {
	window  time.Duration
	last    time.Time
	hasLast bool
	samples []frameSample
}

func newFrameStats(window time.Duration) *frameStats {
	return &frameStats{window: window}
}

// observe は時刻 now に 1 フレームを描いたことを記録する。
func (f *frameStats) observe(now time.Time) {
	if f.hasLast {
		f.samples = append(f.samples, frameSample{end: now, dt: now.Sub(f.last)})
	}
	f.last, f.hasLast = now, true
	f.prune(now)
}

// prune は窓から外れた古い記録を捨てる。
func (f *frameStats) prune(now time.Time) {
	cutoff := now.Add(-f.window)
	i := 0
	for i < len(f.samples) && !f.samples[i].end.After(cutoff) {
		i++
	}
	if i > 0 {
		f.samples = append(f.samples[:0], f.samples[i:]...)
	}
}

// max は now から見て直近 window に終わったフレームの間隔の最大値を返す（無ければ 0）。
func (f *frameStats) max(now time.Time) time.Duration {
	cutoff := now.Add(-f.window)
	var m time.Duration
	for _, s := range f.samples {
		if s.end.After(cutoff) && s.dt > m {
			m = s.dt
		}
	}

	return m
}

// perfState は HUD に出す負荷の値。
type perfState struct {
	FPS         float64
	FrameMax    time.Duration
	RenderScale float64
}

// perfLine は HUD の負荷の行を組み立てる。
func perfLine(p perfState) string {
	line := fmt.Sprintf("FPS: %.1f  FrameMax(5s): %.1fms", p.FPS, float64(p.FrameMax)/float64(time.Millisecond))
	if p.RenderScale < 1 {
		line += fmt.Sprintf("  RenderScale: %.2f", p.RenderScale)
	}

	return line
}
