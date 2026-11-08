package main

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/cyokozai/kagelife/internal/carabiner"
	"github.com/cyokozai/kagelife/internal/tempo"
)

// linkSource は Link の拍の推定を返す（*carabiner.Estimator）。並行に呼んでよいこと。
type linkSource interface {
	Estimate(now time.Time) carabiner.Estimate
}

// linkClock は -link のときの拍時計。毎フレーム step で Link の推定を読み、
// 同期中（Synced）は BPM と拍を Link に合わせる。ゲームループのゴルーチンからだけ使う。
// Carabiner のコールバックとの受け渡しは Estimator の中で排他される。
type linkClock struct {
	src      linkSource
	last     carabiner.Estimate // 直近の step で読んだ推定
	isSynced bool               // 直近の step で同期していたら true
}

func newLinkClock(src linkSource) *linkClock {
	return &linkClock{src: src}
}

// step は now の推定を読む。同期中なら tapper を Link の BPM と拍頭に合わせ続ける。
// こうしておくと、制御口の state も Link の BPM を返し、同期が切れたときは
// tapper が切れる直前の Link の BPM と拍頭をそのまま引き継ぐ（拍が飛ばない）。
// 同期中に set_bpm やタップで tapper が変わっても、ここで Link の値に戻す。
func (l *linkClock) step(tp *tempo.Tapper, now time.Time) bool {
	e := l.src.Estimate(now)
	was := l.isSynced
	l.last = e
	l.isSynced = e.Synced && e.BPM > 0

	switch {
	case l.isSynced:
		// 拍頭 = 今 − 拍内の位相 × 1 拍の長さ
		head := now.Add(-time.Duration(e.Phase * 60 / e.BPM * float64(time.Second)))
		tp.SetBPM(e.BPM, head)
		if !was {
			log.Printf("link: synced (%.1f BPM, %s); Space and set_bpm follow Link while synced", e.BPM, peersText(e.Peers))
		}
	case was:
		log.Printf("link: lost sync, falling back to tap tempo at %.1f BPM", tp.BPM())
	}

	return l.isSynced
}

// synced は直近の step で同期していたら true。l が nil（-link 無し）なら false。
func (l *linkClock) synced() bool {
	return l != nil && l.isSynced
}

// hudText は HUD の状態行に出す Link の状態。
func (l *linkClock) hudText() string {
	e := l.last
	switch {
	case l.isSynced:
		return "synced (" + peersText(e.Peers) + ")"
	case !e.Connected:
		return "offline (tap)"
	case !e.HasStatus:
		return "waiting (tap)"
	default:
		return "no peers (tap)"
	}
}

func peersText(n int) string {
	if n == 1 {
		return "1 peer"
	}

	return fmt.Sprintf("%d peers", n)
}

// stepTempo は 1 フレーム分の拍時計を進める。tapped は今のフレームで Space が押されたか。
// Link に同期中はタップを無視する（BPM も拍頭も変えない）。
func (g *Game) stepTempo(now time.Time, tapped bool) {
	synced := false
	if g.link != nil {
		synced = g.link.step(g.tapper, now)
	}
	if tapped && !synced {
		g.tapper.Tap(now)
	}
}

// bpm はクロスフェードと HUD に使う BPM。同期中は Link の値、それ以外は tapper の値。
func (g *Game) bpm() float64 {
	if g.link.synced() {
		return g.link.last.BPM
	}

	return g.tapper.BPM()
}

// beatPhase は映像に使う拍の位相（uniform の Beat）。
// 同期中は Link の拍を「今 − offset」で評価した拍内の位相、それ以外は tapper の位相（visualPhase）。
func (g *Game) beatPhase(now time.Time) float32 {
	if g.link.synced() {
		if e := g.link.src.Estimate(now.Add(-g.avOffset)); e.Synced && e.BPM > 0 {
			return float32(e.Phase)
		}
	}

	return visualPhase(g.tapper, now, g.avOffset)
}

// startLink は Carabiner への接続を背景で始め、status を受ける Estimator と Client を返す。
// cfg の Addr・Poll・Dial などはそのまま使い、コールバックはここで埋める（cmd/linkpoc と同じつなぎ方）。
// コールバックは Client のゴルーチンから順に呼ばれる。止めるには Client.Close を呼ぶ。
func startLink(cfg carabiner.Config) (*carabiner.Estimator, *carabiner.Client) {
	est := carabiner.NewEstimator(carabiner.DefaultQuantum)
	addr := cfg.Addr
	if addr == "" {
		addr = carabiner.DefaultAddr
	}

	// つながらない状態が続くあいだ、案内は 1 回だけ出す（コールバックは同じゴルーチンから来る）。
	offlineNoticed := false
	cfg.OnConnect = func() {
		est.SetConnected(true)
		offlineNoticed = false
		log.Printf("link: connected to Carabiner (%s)", addr)
	}
	cfg.OnDisconnect = func(err error) {
		est.SetConnected(false)
		if offlineNoticed {
			return
		}
		offlineNoticed = true
		log.Printf("link: cannot reach Carabiner (%s): %v; start Carabiner on this machine. Using tap tempo until it connects", addr, err)
	}
	cfg.OnStatus = est.Update
	cfg.OnMessageError = func(err error) {
		var re *carabiner.ResponseError
		if errors.As(err, &re) && re.Kind == "unsupported" {
			log.Printf("link: Carabiner does not support a command: %v (check the Carabiner version)", err)

			return
		}
		log.Printf("link: cannot parse a Carabiner response: %v", err)
	}

	return est, carabiner.Start(cfg)
}
