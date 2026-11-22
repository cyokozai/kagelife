package audio

import "math"

// fft は大きさ n（2 のべき）の基数 2 の FFT。回転因子とビット反転の表を先に作って使い回す。
// 外部の FFT ライブラリは使わない（ADR-009: 解析は純 Go）。
type fft struct {
	n      int
	cos    []float64
	sin    []float64
	bitrev []int
}

func newFFT(n int) *fft {
	if n < 2 || n&(n-1) != 0 {
		panic("audio: FFT size must be a power of two")
	}
	f := &fft{n: n, cos: make([]float64, n/2), sin: make([]float64, n/2), bitrev: make([]int, n)}
	for i := range n / 2 {
		a := -2 * math.Pi * float64(i) / float64(n)
		f.cos[i], f.sin[i] = math.Cos(a), math.Sin(a)
	}
	bits := 0
	for 1<<bits < n {
		bits++
	}
	for i := range n {
		r := 0
		for b := range bits {
			if i&(1<<b) != 0 {
				r |= 1 << (bits - 1 - b)
			}
		}
		f.bitrev[i] = r
	}

	return f
}

// transform は re・im（長さ n）をその場で順方向に変換する。
func (f *fft) transform(re, im []float64) {
	n := f.n
	for i, j := range f.bitrev {
		if i < j {
			re[i], re[j] = re[j], re[i]
			im[i], im[j] = im[j], im[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		half := size / 2
		step := n / size
		for start := 0; start < n; start += size {
			for k := range half {
				wr, wi := f.cos[k*step], f.sin[k*step]
				a, b := start+k, start+k+half
				tr := re[b]*wr - im[b]*wi
				ti := re[b]*wi + im[b]*wr
				re[b], im[b] = re[a]-tr, im[a]-ti
				re[a], im[a] = re[a]+tr, im[a]+ti
			}
		}
	}
}
