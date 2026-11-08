package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/cyokozai/kagelife/internal/carabiner"
)

// linkT0 はテストの基準時刻（偽の時刻）。
var linkT0 = time.Date(2026, 11, 21, 20, 0, 0, 0, time.UTC)

// newLinkTestGame は Link を有効にした HUD テスト用の Game と、status を流し込む Estimator を返す。
// Estimator には linkT0 の時点で「128 BPM・拍 10.25・相手 1」の status を入れてある。
func newLinkTestGame(t *testing.T) (*Game, *carabiner.Estimator) {
	t.Helper()
	g := newHUDTestGame(t)
	est := carabiner.NewEstimator(carabiner.DefaultQuantum)
	est.SetConnected(true)
	est.Update(carabiner.Status{Peers: 1, BPM: 128, Beat: 10.25}, linkT0)
	g.link = newLinkClock(est)

	return g, est
}

func near64(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestLinkSync_SyncedUsesLinkBPMAndPhase(t *testing.T) {
	g, _ := newLinkTestGame(t)

	g.stepTempo(linkT0, false)

	if got := g.bpm(); got != 128 {
		t.Fatalf("bpm = %v, want 128（Link の値）", got)
	}
	if got := g.beatPhase(linkT0); !near(got, 0.25) {
		t.Fatalf("phase = %v, want 0.25（Link の拍 10.25 の拍内の位置）", got)
	}
	// 0.5 秒後は 128/60*0.5 = 1.0667 拍進む → 拍内 0.25+0.0667
	at := linkT0.Add(500 * time.Millisecond)
	g.stepTempo(at, false)
	if got, want := g.beatPhase(at), float32(0.25+128.0/60*0.5-1); !near(got, want) {
		t.Fatalf("0.5 秒後の phase = %v, want %v", got, want)
	}
}

func TestLinkSync_AVOffsetEvaluatesLinkAtNowMinusOffset(t *testing.T) {
	g, est := newLinkTestGame(t)
	g.avOffset = 100 * time.Millisecond

	g.stepTempo(linkT0, false)

	want := float32(est.Estimate(linkT0.Add(-100 * time.Millisecond)).Phase) // 0.25 - 0.1*128/60
	if got := g.beatPhase(linkT0); !near(got, want) {
		t.Fatalf("phase = %v, want %v（今 − offset で Link を評価）", got, want)
	}
	if !near(want, float32(0.25-0.1*128/60)) {
		t.Fatalf("前提: want = %v", want)
	}
}

func TestLinkSync_TapDoesNotChangeBPMOrPhaseWhileSynced(t *testing.T) {
	g, _ := newLinkTestGame(t)
	g.stepTempo(linkT0, false)

	// 拍の途中で何度叩いても Link の BPM と位相のまま
	for i := 1; i <= 4; i++ {
		at := linkT0.Add(time.Duration(i) * 300 * time.Millisecond)
		g.stepTempo(at, true)
		if got := g.bpm(); got != 128 {
			t.Fatalf("%d 回目のタップの後の bpm = %v, want 128", i, got)
		}
		want := float32(math.Mod(10.25+at.Sub(linkT0).Seconds()*128/60, 1))
		if got := g.beatPhase(at); !near(got, want) {
			t.Fatalf("%d 回目のタップの後の phase = %v, want %v", i, got, want)
		}
	}
}

func TestLinkSync_SetBPMIsOverriddenOnNextFrame(t *testing.T) {
	g, _ := newLinkTestGame(t)
	g.stepTempo(linkT0, false)

	// 制御口の set_bpm（Engine.SetBPM）は tapper.SetBPM を呼ぶ
	g.tapper.SetBPM(90, linkT0.Add(10*time.Millisecond))

	at := linkT0.Add(16 * time.Millisecond)
	g.stepTempo(at, false)
	if got := g.bpm(); got != 128 {
		t.Fatalf("bpm = %v, want 128（次のフレームで Link の値に戻る）", got)
	}
	// 制御口の state が読む tapper も Link の値になっている
	if got := g.tapper.BPM(); got != 128 {
		t.Fatalf("tapper.BPM() = %v, want 128", got)
	}
}

func TestLinkSync_FallsBackToTapperKeepingBPMAndBeat(t *testing.T) {
	for _, c := range []struct {
		name string
		lose func(est *carabiner.Estimator)
	}{
		{"切断", func(est *carabiner.Estimator) { est.SetConnected(false) }},
		{"相手がいなくなる", func(est *carabiner.Estimator) {
			est.Update(carabiner.Status{Peers: 0, BPM: 128, Beat: 10.25}, linkT0)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			g, est := newLinkTestGame(t)
			g.stepTempo(linkT0, false)

			c.lose(est)
			at := linkT0.Add(time.Second)
			g.stepTempo(at, false)

			if got := g.bpm(); got != 128 {
				t.Fatalf("bpm = %v, want 128（切れる直前の Link の BPM を引き継ぐ）", got)
			}
			if !g.tapper.Measured() {
				t.Fatal("tapper.Measured() = false, want true")
			}
			// 拍が飛ばない: 直前の Link の拍を同じ BPM で外挿した位置
			want := float32(math.Mod(10.25+128.0/60, 1))
			if got := g.beatPhase(at); !near(got, want) {
				t.Fatalf("phase = %v, want %v（Link の拍をそのまま延ばした位置）", got, want)
			}

			// タップに戻っている: 2 回叩けばその間隔の BPM になる
			g.stepTempo(at.Add(100*time.Millisecond), true)
			g.stepTempo(at.Add(600*time.Millisecond), true)
			if got := g.bpm(); got != 120 {
				t.Fatalf("タップ 2 回の後の bpm = %v, want 120", got)
			}
		})
	}
}

func TestLinkSync_NeverSyncedUsesTapper(t *testing.T) {
	g := newHUDTestGame(t)
	est := carabiner.NewEstimator(carabiner.DefaultQuantum) // 接続も status も無い
	g.link = newLinkClock(est)

	g.stepTempo(linkT0, true)
	g.stepTempo(linkT0.Add(400*time.Millisecond), true)

	if got := g.bpm(); got != 150 {
		t.Fatalf("bpm = %v, want 150（タップの値）", got)
	}
	if g.link.synced() {
		t.Fatal("synced() = true, want false")
	}
}

func TestLinkSync_DisabledUsesTapper(t *testing.T) {
	g := newHUDTestGame(t) // link は nil

	g.stepTempo(linkT0, true)
	g.stepTempo(linkT0.Add(500*time.Millisecond), true)

	if got := g.bpm(); got != 120 {
		t.Fatalf("bpm = %v, want 120", got)
	}
	if got := g.beatPhase(linkT0.Add(500 * time.Millisecond)); got != 0 {
		t.Fatalf("phase = %v, want 0（最後のタップが拍頭）", got)
	}
	if got := g.hudState(linkT0).Link; got != "" {
		t.Fatalf("Link = %q, want 空（無効なら出さない）", got)
	}
}

func TestLinkSync_HUDState(t *testing.T) {
	t.Run("同期中は Link の BPM と同期中の表示", func(t *testing.T) {
		g, _ := newLinkTestGame(t)
		g.stepTempo(linkT0, false)
		st := g.hudState(linkT0)
		if st.BPM != 128 || !st.Measured {
			t.Fatalf("BPM = %v, Measured = %v, want 128, true", st.BPM, st.Measured)
		}
		if st.Link != "synced (1 peer)" {
			t.Fatalf("Link = %q", st.Link)
		}
	})

	t.Run("相手がいなければ no peers", func(t *testing.T) {
		g, est := newLinkTestGame(t)
		est.Update(carabiner.Status{Peers: 0, BPM: 128}, linkT0)
		g.stepTempo(linkT0, false)
		if got := g.hudState(linkT0).Link; got != "no peers (tap)" {
			t.Fatalf("Link = %q", got)
		}
	})

	t.Run("つながっていなければ offline", func(t *testing.T) {
		g, est := newLinkTestGame(t)
		est.SetConnected(false)
		g.stepTempo(linkT0, false)
		if got := g.hudState(linkT0).Link; got != "offline (tap)" {
			t.Fatalf("Link = %q", got)
		}
	})
}

func TestHUDText_Link(t *testing.T) {
	got := hudText(hudState{BPM: 128, Measured: true, FadeBeats: 4, Link: "synced (2 peers)"})
	want := "BPM: 128.0  FadeBeats: 4.0  Mix: 0.00  Link: synced (2 peers)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestParseFlags_Link(t *testing.T) {
	opts, err := parseFlags(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.link {
		t.Fatal("link は既定で無効")
	}
	if opts.linkAddr != "127.0.0.1:17000" {
		t.Fatalf("linkAddr = %q, want 127.0.0.1:17000", opts.linkAddr)
	}

	opts, err = parseFlags([]string{"-link", "-link-addr", "127.0.0.1:17001"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.link || opts.linkAddr != "127.0.0.1:17001" {
		t.Fatalf("link = %v, linkAddr = %q", opts.link, opts.linkAddr)
	}
}

// TestStartLink_FakeCarabiner は偽の Carabiner（net.Pipe）を相手に、status を受けると
// BPM と Beat が Link の値に切り替わり、切断するとタップに戻って BPM が保たれることを確かめる。
// コールバックは Client のゴルーチンから来るので、-race で受け渡しを確かめる。
func TestStartLink_FakeCarabiner(t *testing.T) {
	server, client := net.Pipe()
	dialed := false
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		if dialed { // 再接続は失敗させる（切断のまま）
			return nil, errors.New("connection refused")
		}
		dialed = true

		return client, nil
	}

	// 偽の Carabiner: status を受けるたびに 132 BPM の status を返す
	go func() {
		sc := bufio.NewScanner(server)
		beat := 0.0
		for sc.Scan() {
			if strings.TrimSpace(sc.Text()) != "status" {
				continue
			}
			beat += 0.2
			line := fmt.Sprintf("status { :peers 2 :bpm 132.000000 :start 0 :beat %f }\n", beat)
			if _, err := server.Write([]byte(line)); err != nil {
				return
			}
		}
	}()

	est, c := startLink(carabiner.Config{
		Addr: "127.0.0.1:17000",
		Poll: 10 * time.Millisecond,
		Dial: dial,
	})
	defer func() { _ = c.Close() }()

	g := newHUDTestGame(t)
	g.link = newLinkClock(est)

	waitFor(t, "Link に同期する", func() bool {
		g.stepTempo(time.Now(), false)
		return g.link.synced()
	})
	if got := g.bpm(); got != 132 {
		t.Fatalf("bpm = %v, want 132", got)
	}
	now := time.Now()
	if got, want := g.beatPhase(now), float32(est.Estimate(now).Phase); !near(got, want) {
		t.Fatalf("phase = %v, want %v（Link の位相）", got, want)
	}

	_ = server.Close() // Carabiner が落ちる

	waitFor(t, "タップに戻る", func() bool {
		g.stepTempo(time.Now(), false)
		return !g.link.synced()
	})
	if got := g.bpm(); got != 132 {
		t.Fatalf("切断後の bpm = %v, want 132（引き継ぎ）", got)
	}
	if got := g.hudState(time.Now()).Link; got != "offline (tap)" {
		t.Fatalf("Link = %q", got)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%s: 3 秒以内に満たされない", what)
}
