package audio

import (
	"math"
	"math/rand/v2"
	"testing"
)

const testRate = 48000

// sine は振幅 amp・周波数 freq の正弦波を n 標本作る。phase0 は始まりの標本番号（つなげて作るため）。
func sine(freq, amp float64, n, phase0 int) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = float32(amp * math.Sin(2*math.Pi*freq*float64(phase0+i)/testRate))
	}

	return out
}

func silence(n int) []float32 { return make([]float32, n) }

// feed は samples を chunk 標本ずつ a に入れ、最後の値を返す。
func feed(a *Analyzer, samples []float32, chunk int) Levels {
	var last Levels
	for len(samples) > 0 {
		n := min(chunk, len(samples))
		last = a.Write(samples[:n])
		samples = samples[n:]
	}

	return last
}

func secs(s float64) int { return int(s * testRate) }

func splitEvery(s []float32, n int) [][]float32 {
	var out [][]float32
	for len(s) > 0 {
		k := min(n, len(s))
		out = append(out, s[:k])
		s = s[k:]
	}

	return out
}

// FFT は純 Go の基数 2。周波数 k の複素正弦波は k 番目のビンだけに出る。
func TestFFT_SingleBin(t *testing.T) {
	const n = 16
	re := make([]float64, n)
	im := make([]float64, n)
	for i := range n {
		re[i] = math.Cos(2 * math.Pi * 3 * float64(i) / n)
	}
	f := newFFT(n)
	f.transform(re, im)
	for k := range n {
		mag := math.Hypot(re[k], im[k])
		want := 0.0
		if k == 3 || k == n-3 {
			want = n / 2
		}
		if math.Abs(mag-want) > 1e-9 {
			t.Errorf("bin %d: |X| = %v, want %v", k, mag, want)
		}
	}
}

// 帯域: 低 20〜150 Hz・中 150 Hz〜2 kHz・高 2〜16 kHz。正弦波は自動ゲインの前の値でその帯域だけを上げる。
// （自動ゲインは帯域ごとなので、他の帯域へ漏れた小さな値も単独では 1 まで上がる。振り分けはゲインの前で見る）
func TestAnalyzer_BandsFollowSineFrequency(t *testing.T) {
	cases := []struct {
		name string
		freq float64
		hot  int
	}{
		{"low 80Hz", 80, bandLow},
		{"mid 1kHz", 1000, bandMid},
		{"high 6kHz", 6000, bandHigh},
		// 境界際（例 140 Hz）は 1 ビン 46.9 Hz・Hann の主ローブ ±2 ビンのため隣の帯域へ漏れる。分解能の限界なので検査しない
		{"kick 50Hz", 50, bandLow},
		{"high 12kHz", 12000, bandHigh},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := NewAnalyzer(testRate)
			got := feed(a, sine(c.freq, 0.5, secs(1), 0), 480)
			raw := a.raw
			if raw[c.hot] < 0.3 {
				t.Errorf("その帯域の値（ゲイン前）= %v, want >= 0.3 (raw %v)", raw[c.hot], raw)
			}
			for b := range numBands {
				if b != c.hot && raw[b] > 0.1*raw[c.hot] {
					t.Errorf("帯域 %d の値（ゲイン前）= %v, want <= 1/10 of %v", b, raw[b], raw[c.hot])
				}
			}
			if v := got.band(c.hot); v < 0.9 {
				t.Errorf("その帯域の値 = %v, want >= 0.9 (%+v)", v, got)
			}
		})
	}
}

// 帯域の外（20 Hz 未満・16 kHz 以上）の音はどの帯域にも入らない。
func TestAnalyzer_OutOfRangeIgnored(t *testing.T) {
	a := NewAnalyzer(testRate)
	feed(a, sine(19000, 0.5, secs(1), 0), 512)
	for b := range numBands {
		if a.raw[b] > 0.01 {
			t.Errorf("19 kHz: 帯域 %d の値 = %v, want ≈ 0", b, a.raw[b])
		}
	}
}

// 無音なら全部 0（自動ゲインで雑音を 1 まで持ち上げない）。
func TestAnalyzer_SilenceIsZero(t *testing.T) {
	a := NewAnalyzer(testRate)
	got := feed(a, silence(secs(2)), 512)
	if got != (Levels{}) {
		t.Errorf("無音: %+v, want all 0", got)
	}
}

// ±Inf が 1 標本混ざっても解析が壊れない（NaN と同じく無音として扱う）。
// 以前は Inf が FFT で NaN になり、自動ゲインの履歴に残って 3 帯域が永久に 0 になっていた。
func TestAnalyzer_InfIsIgnored(t *testing.T) {
	for _, inf := range []float32{float32(math.Inf(1)), float32(math.Inf(-1))} {
		a := NewAnalyzer(testRate)
		src := sine(80, 0.5, secs(2), 0)
		src[secs(0.5)] = inf
		feed(a, src[:secs(1)], 480)
		got := feed(a, src[secs(1):], 480) // Inf を入れてから 1 秒以上あと
		if got.Bass < 0.5 {
			t.Errorf("%v を混ぜた 1 秒後: Bass = %v, want >= 0.5 (%+v)", inf, got.Bass, got)
		}
	}
}

// 小さな白色雑音（-60 dBFS、σ≈1e-3）だけが続いても、どの帯域も 1 近くまで持ち上げない。
// 雑音の帯域レベルはビン数の平方根に比例するので、下限が帯域で共通だと広い帯域（中・高）が持ち上がる。
func TestAnalyzer_QuietNoiseStaysLow(t *testing.T) {
	a := NewAnalyzer(testRate)
	r := rand.New(rand.NewPCG(5, 6))
	src := make([]float32, secs(12))
	for i := range src {
		src[i] = float32(r.NormFloat64() * 1e-3)
	}
	got := feed(a, src, 480)
	t.Logf("-60 dBFS の雑音 12 秒: %+v（下限 %v）", got, a.floor)
	for b := range numBands {
		if v := got.band(b); v >= 0.3 {
			t.Errorf("-60 dBFS の雑音: 帯域 %d = %v, want < 0.3 (%+v)", b, v, got)
		}
	}
}

// 自動ゲイン: 直近 10 秒の最大で割るので、小さい音だけが続けば 1 近くまで上がる。
// 大きい音の後の小さい音は、大きい音が 10 秒の窓から出るまで小さいまま。
func TestAnalyzer_AutoGain(t *testing.T) {
	a := NewAnalyzer(testRate)
	quiet := feed(a, sine(1000, 0.01, secs(1), 0), 512)
	if quiet.Mid < 0.9 {
		t.Errorf("小さい音だけ: Mid = %v, want >= 0.9", quiet.Mid)
	}

	b := NewAnalyzer(testRate)
	pos := 0
	feed(b, sine(1000, 0.5, secs(1), pos), 512)
	pos += secs(1)
	after := feed(b, sine(1000, 0.05, secs(2), pos), 512)
	pos += secs(2)
	if after.Mid < 0.07 || after.Mid > 0.13 {
		t.Errorf("大きい音の 2 秒後の 1/10 の音: Mid = %v, want ≈ 0.1", after.Mid)
	}
	recovered := feed(b, sine(1000, 0.05, secs(9), pos), 512)
	if recovered.Mid < 0.9 {
		t.Errorf("大きい音から 11 秒後: Mid = %v, want >= 0.9（窓から出た）", recovered.Mid)
	}
}

// ならし: 上がり 10 ms・下がり 300 ms。音が止まって 300 ms 後は約 1/e、50 ms 後はまだ大きい。
func TestAnalyzer_SmoothingAttackRelease(t *testing.T) {
	a := NewAnalyzer(testRate)
	feed(a, silence(secs(0.5)), 512)
	pos := secs(0.5)
	// 上がり: 鳴り始めて 50 ms（時定数の 5 倍）でほぼ上がりきる
	up := feed(a, sine(1000, 0.5, secs(0.05), pos), 512)
	if up.Mid < 0.8 {
		t.Errorf("鳴って 50 ms: Mid = %v, want >= 0.8（上がり 10 ms）", up.Mid)
	}
	pos += secs(0.05)
	feed(a, sine(1000, 0.5, secs(1), pos), 512)

	// 下がり: 止めて 50 ms ではまだ 0.6 以上、300 ms で 1/e 付近、1.5 s でほぼ 0
	d1 := feed(a, silence(secs(0.05)), 512)
	if d1.Mid < 0.6 {
		t.Errorf("止めて 50 ms: Mid = %v, want >= 0.6（下がり 300 ms）", d1.Mid)
	}
	d2 := feed(a, silence(secs(0.25)), 512)
	if d2.Mid < 0.25 || d2.Mid > 0.5 {
		t.Errorf("止めて 300 ms: Mid = %v, want ≈ 1/e", d2.Mid)
	}
	d3 := feed(a, silence(secs(1.2)), 512)
	if d3.Mid > 0.02 {
		t.Errorf("止めて 1.5 s: Mid = %v, want ≈ 0", d3.Mid)
	}
}

// オンセット: 急な立ち上がりで 1 になり、時定数 150 ms で 0 へ減る。
func TestAnalyzer_OnsetEnvelope(t *testing.T) {
	a := NewAnalyzer(testRate)
	if got := feed(a, silence(secs(1)), 512); got.Onset != 0 {
		t.Fatalf("無音: Onset = %v, want 0", got.Onset)
	}
	pos := secs(1)
	hit := feed(a, sine(1000, 0.5, secs(0.03), pos), 512)
	if hit.Onset < 0.8 {
		t.Errorf("立ち上がり直後: Onset = %v, want ≈ 1", hit.Onset)
	}
	pos += secs(0.03)
	later := feed(a, sine(1000, 0.5, secs(0.15), pos), 512)
	if later.Onset < 0.25 || later.Onset > 0.5 {
		t.Errorf("立ち上がりから約 150 ms: Onset = %v, want ≈ 1/e", later.Onset)
	}
	pos += secs(0.15)
	// 同じ音が続く間は、新しいオンセットにならず 0 へ減っていく
	steady := feed(a, sine(1000, 0.5, secs(1), pos), 512)
	if steady.Onset > 0.05 {
		t.Errorf("定常音が 1 秒続いた後: Onset = %v, want ≈ 0", steady.Onset)
	}
}

// 拍のような打撃（短い雑音の塊）を繰り返すと、そのたびにオンセットが 1 近くまで上がる。
func TestAnalyzer_RepeatedHits(t *testing.T) {
	a := NewAnalyzer(testRate)
	r := rand.New(rand.NewPCG(1, 2))
	burst := func() []float32 {
		out := make([]float32, secs(0.05))
		for i := range out {
			out[i] = float32((r.Float64()*2 - 1) * 0.5 * math.Exp(-float64(i)/float64(secs(0.02))))
		}

		return out
	}
	feed(a, silence(secs(0.5)), 512)
	for i := range 4 {
		peak := float32(0)
		for _, chunk := range splitEvery(burst(), HopSize) {
			peak = max(peak, a.Write(chunk).Onset)
		}
		if peak < 0.99 {
			t.Errorf("打撃 %d: Onset の最大 = %v, want 1", i, peak)
		}
		gap := feed(a, silence(secs(0.45)), 512)
		if gap.Onset > 0.1 {
			t.Errorf("打撃 %d の 450 ms 後: Onset = %v, want ≈ 0", i, gap.Onset)
		}
	}
}

// 書き込みの区切り方に結果が左右されない（コールバックの標本数は端末ごとに違う）。
func TestAnalyzer_ChunkingIndependent(t *testing.T) {
	src := sine(300, 0.3, secs(0.7), 0)
	a := feed(NewAnalyzer(testRate), src, 37)
	b := feed(NewAnalyzer(testRate), src, 4096)
	if a != b {
		t.Errorf("区切りで結果が変わった: %+v vs %+v", a, b)
	}
}

// 値はどれも 0〜1 に収まる（大きな音・クリップでも）。
func TestAnalyzer_Clamped(t *testing.T) {
	a := NewAnalyzer(testRate)
	r := rand.New(rand.NewPCG(3, 4))
	for range 200 {
		buf := make([]float32, 480)
		for i := range buf {
			buf[i] = float32(r.Float64()*4 - 2)
		}
		l := a.Write(buf)
		for _, v := range []float32{l.Bass, l.Mid, l.High, l.Onset} {
			if v < 0 || v > 1 || math.IsNaN(float64(v)) {
				t.Fatalf("範囲外: %+v", l)
			}
		}
	}
}
