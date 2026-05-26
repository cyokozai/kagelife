package filewatcher

import (
	"log"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)


type Watcher struct {
	Events  <-chan string      // 外部公開: 受信専用チャネル
	events  chan string        // 内部送信用チャネル
	raw     chan string        // fsnotify の生イベントを受け取る
	quit    chan struct{}      // Close() で送る停止シグナル
	once    sync.Once         // Close() の二重呼び出し防止
	delay   time.Duration     // デバウンス間隔
	watcher *fsnotify.Watcher // goroutine リーク防止のために保持
}

func New(dir string) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if err = fw.Add(dir); err != nil {
		log.Println(err)
		fw.Close()
		return nil, err
	}

	events := make(chan string, 8)
	raw := make(chan string, 8)
	quit := make(chan struct{})

	go func() {
		for {
			select {
			case event, ok := <-fw.Events:
				if !ok {
					return
				}
				if strings.HasSuffix(event.Name, ".kage") && (event.Has(fsnotify.Write) || event.Has(fsnotify.Create)) {
					log.Printf("event: %s", event.Name)
					select {
					case raw <- event.Name:
					case <-quit:
						return
					}
				}
			case err, ok := <-fw.Errors:
				if !ok {
					return
				}
				log.Println(err)
			}
		}
	}()

	w := &Watcher{
		Events:  events,
		events:  events,
		raw:     raw,
		quit:    quit,
		delay:   100 * time.Millisecond,
		watcher: fw,
	}
	go w.debounceLoop()

	return w, nil
}

func newForTest(delay time.Duration) *Watcher {
	events := make(chan string, 8)
	w := &Watcher{
		Events: events,
		events: events,
		raw:    make(chan string, 8),
		quit:   make(chan struct{}),
		delay:  delay,
	}
	go w.debounceLoop()

	return w
}

func (w *Watcher) Close() {
	w.once.Do(func() {
		close(w.quit)
		if w.watcher != nil {
			w.watcher.Close()
		}
	})
}

func (w *Watcher) debounceLoop() {
	type firedMsg struct {
		path  string
		timer *time.Timer
	}
	timers := map[string]*time.Timer{}
	fired := make(chan firedMsg, 16)

	for {
		select {
		case path := <-w.raw:
			if t, ok := timers[path]; ok {
				t.Stop()
			}
			p := path
			var t *time.Timer
			t = time.AfterFunc(w.delay, func() {
				select {
				case w.events <- p:
				case <-w.quit:
				}
				select {
				case fired <- firedMsg{path: p, timer: t}:
				default:
				}
			})
			timers[path] = t

		case msg := <-fired:
			if timers[msg.path] == msg.timer {
				delete(timers, msg.path)
			}

		case <-w.quit:
			for _, t := range timers {
				t.Stop()
			}
			return
		}
	}
}
