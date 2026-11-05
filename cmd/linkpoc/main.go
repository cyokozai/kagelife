// Command linkpoc は Carabiner 経由で Ableton Link のテンポと拍を受け取り、
// 10Hz で 1 行ずつ表示する PoC。rekordbox などの拍の点滅と目で比べるために使う。
//
// Carabiner（https://github.com/Deep-Symmetry/carabiner）は別プロセスで、
// 利用者が自分で起動する。kagelife は Link の SDK を持たない。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cyokozai/kagelife/internal/carabiner"
)

const displayInterval = 100 * time.Millisecond // 10Hz

func main() {
	addr := flag.String("addr", carabiner.DefaultAddr, "Carabiner の待ち受けアドレス")
	quantum := flag.Int("quantum", 4, "小節の長さ（拍）")
	poll := flag.Duration("poll", carabiner.DefaultPoll, "status を問い合わせる間隔")
	startStop := flag.Bool("startstop", true, "Start/Stop Sync を有効にする（enable-start-stop-sync を送る）")
	flag.Parse()

	if *quantum <= 0 {
		fmt.Fprintln(os.Stderr, "-quantum は 1 以上を指定してください")
		os.Exit(2)
	}
	if *poll <= 0 {
		fmt.Fprintln(os.Stderr, "-poll は正の間隔を指定してください")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	run(ctx, os.Stdout, *addr, float64(*quantum), *poll, *startStop)
}

// run は ctx が終わるまで表示を続ける。
func run(ctx context.Context, out io.Writer, addr string, quantum float64, poll time.Duration, startStop bool) {
	est := carabiner.NewEstimator(quantum)
	notes := make(chan string, 16)
	note := func(s string) {
		select {
		case notes <- s:
		default: // 表示が詰まっていれば捨てる
		}
	}

	// 接続できない状態が続く間、案内は 1 回だけ出す。
	offlineNoticed := false
	client := carabiner.Start(carabiner.Config{
		Addr:          addr,
		Poll:          poll,
		StartStopSync: startStop,
		OnConnect: func() {
			est.SetConnected(true)
			offlineNoticed = false
			note(fmt.Sprintf("# Carabiner (%s) に接続しました", addr))
		},
		OnDisconnect: func(err error) {
			est.SetConnected(false)
			if offlineNoticed {
				return
			}
			offlineNoticed = true
			note(fmt.Sprintf("# Carabiner (%s) に接続できません: %v — 別のターミナルで Carabiner を起動してください（例: ./Carabiner）。再接続を待ちます", addr, err))
		},
		OnStatus: func(st carabiner.Status, at time.Time) { est.Update(st, at) },
		OnMessageError: func(err error) {
			var re *carabiner.ResponseError
			if errors.As(err, &re) && re.Kind == "unsupported" {
				note(fmt.Sprintf("# Carabiner が対応していないコマンドがあります: %v（Carabiner の版を確認してください）", err))
				return
			}
			note(fmt.Sprintf("# 応答を解釈できません: %v", err))
		},
	})
	defer func() { _ = client.Close() }()

	_, _ = fmt.Fprintf(out, "# linkpoc: addr=%s quantum=%g poll=%s startstop=%v（Ctrl+C で終了）\n", addr, quantum, poll, startStop)

	ticker := time.NewTicker(displayInterval)
	defer ticker.Stop()
	var prev carabiner.Estimate
	for {
		select {
		case <-ctx.Done():
			_, _ = fmt.Fprintln(out, "# 終了します")
			return
		case s := <-notes:
			_, _ = fmt.Fprintln(out, s)
		case <-ticker.C:
			now := time.Now()
			cur := est.Estimate(now)
			_, _ = fmt.Fprintln(out, formatLine(now, cur, crossedBeat(prev, cur)))
			prev = cur
		}
	}
}
