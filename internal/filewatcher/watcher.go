// Package filewatcher は shaders/ ディレクトリを監視し、
// .kage ファイルの変更をデバウンスして通知する。
package filewatcher

import (
	"log"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/text/cases"
)

// Watcher は shaders/ ディレクトリのファイル変更を監視する。
//
// 使い方:
//
//	w, err := New("shaders/")
//	defer w.Close()
//	for path := range w.Events {
//	    // path が変更されたファイルのパス
//	}
type Watcher struct {
	Events <-chan string // デバウンスされたイベントを送るチャネル
	
	raw chan string 	  // fsnotify の生イベントを受け取る
	quit chan struct{}  // Close() で送る停止シグナル
	once sync.Once 		  // Close() の二重呼び出し防止
	delay time.Duration // デバウンス間隔
}

// New は dir を監視する Watcher を生成して起動する。
//
// TODO: 以下を実装してください
//  1. fsnotify.NewWatcher() で監視を開始
//  2. dir を watcher.Add() で登録
//  3. raw / quit / events チャネルを初期化
//  4. goroutine を2つ起動:
//     - fsnotify イベントを raw に転送する goroutine（.kage ファイルのみ）
//     - debounceLoop goroutine
//  5. &Watcher{Events: events, ...} を返す
func New(dir string) (*Watcher, error) {
	watcher, _ := fsnotify.NewWatcher()
	err := watcher.Add(dir)
	if err != nil {
		log.Println(err)

		return nil, err
	}

	raw    := make(chan string, 100)
	quit 	 := make(chan struct{})
	events := make(chan string, 100)

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					log.Println("watcher.Events is not ok")

					return 
				}
				if event.Name == dir {
					log.Printf("event: %s", event.Name)
				}
				if strings.HasSuffix(event.Name, ".kage") {
					log.Printf("event: %s", event.Name)
					raw <- event.Name
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					log.Println("watcher.Errors is not ok")
					
					return
				}
				log.Println(err)
			}
		}
	}()

	return &Watcher{Events: events, raw: raw, quit: quit, delay: 500 * time.Millisecond}, nil
} 
//
// TODO: 以下を実装してください
//  1. raw / quit / events チャネルを初期化
//  2. debounceLoop goroutine を起動
//  3. &Watcher{Events: events, raw: raw, quit: quit, delay: delay} を返す
//
// 注意: このファイルに実装しておくことでテストから参照できる（同パッケージのため）。
func newForTest(delay time.Duration) *Watcher {
	panic("not implemented")
}

// Close は監視を停止し、内部リソースを解放する。
 //
// TODO: 以下を実装してください
//  1. once.Do() の中で quit を close()
//  2. fsnotify の watcher も Close()（New() で生成した場合）
//
// ポイント: sync.Once を使うことで Close() の二重呼び出しでパニックしない。
func (w *Watcher) Close() {
	panic("not implemented")
}

// debounceLoop は raw チャネルからイベントを受け取り、
// delay 間隔でデバウンスして events チャネルに送出する。
//
// デバウンスアルゴリズム（ファイルごとにタイマーを管理する）:
//
//	timers := map[string]*time.Timer{}
//	for {
//	    select {
//	    case path := <-raw:
//	        if t, ok := timers[path]; ok {
//	            t.Stop() // 既存タイマーをリセット
//	        }
//	        timers[path] = time.AfterFunc(delay, func() {
//	            events <- path // delay 後に送出
//	        })
//	    case <-quit:
//	        // 全タイマーを Stop して終了
//	        return
//	    }
//	}
//
// TODO: 上記アルゴリズムを実装してください。
// ヒント: time.AfterFunc のコールバックはgoroutineで実行されるため、
//
//	events チャネルがバッファ付きか、または select で quit も待つこと。
func (w *Watcher) debounceLoop() {
	events := make(chan string, 100) // バッファ付きチャネル
	times := map[string]*time.Timer{}
	for {
		select {
		case path := <- w.raw:
			if t, ok := times[path]; ok {
				t.Stop()
			}
			times[path] = time.AfterFunc(w.delay, func() {
				events <- path
			})
		case <- w.quit:
			return 
		}
	}
}

// ── 以下は実装時に参考にする型（使い方を示すためのコメント） ──────────

// closeOnce は Close() の二重呼び出しを防ぐための sync.Once の使用例。
// （フィールド名は自由に変えて構わない）
var _ sync.Once
