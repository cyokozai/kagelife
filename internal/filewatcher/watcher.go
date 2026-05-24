package filewatcher

import (
	"log"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)


type Watcher struct {
	Events <-chan string  // 内部でイベンkkトを送るためのチャネル
	raw chan string 	  // fsnotify の生イベントを受け取る
	quit chan struct{}  // Close() で送る停止シグナル
	once sync.Once 		  // Close() の二重呼び出し防止
	delay time.Duration // デバウンス間隔
}


func New(dir string) (*Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	err = watcher.Add(dir)
	if err != nil {
		log.Println(err)
		watcher.Close()

		return nil, err
	}

	events := make(chan string, 8)
	raw    := make(chan string, 8)
	quit 	 := make(chan struct{})

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					log.Println("watcher.Events is not ok")

					return
				}
				if strings.HasSuffix(event.Name, ".kage") &&  (event.Has(fsnotify.Write) || event.Has(fsnotify.Create)) {
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
	
	w := &Watcher{
		Events: events,
		raw: raw,
		quit: quit,
		delay: 100 * time.Millisecond,
	}
	go w.debounceLoop()

	return w, nil
}


func newForTest(delay time.Duration) *Watcher {
	events := make(chan string, 8)
	w := &Watcher{
		Events: events,
		raw: make(chan string, 8),
		quit: make(chan struct{}),
		delay: delay,
	}
	go w.debounceLoop()

	return w
}


func (w *Watcher) Close() {
	w.once.Do(func() {
		close(w.quit)
		w.Close()
	})
}


func (w *Watcher) debounceLoop() {
	timers := map[string]*time.Timer{}
	for {
		select {
		case path := <-w.raw:
			if t, ok := timers[path]; ok {
				t.Stop()
			}
			timers[path] = time.AfterFunc(w.delay, func() {
				select { 
				case w.Events <- path:

				default:
				}
			})

		case <- w.quit:
			for _, t := range timers {
				t.Stop()
			}

			return
		}
	}
}
