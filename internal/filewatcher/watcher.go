package filewatcher

import (
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	// defaultDelay はデバウンス間隔。PRD の「保存〜反映 < 200ms」に対し 100ms で確定している。
	defaultDelay = 100 * time.Millisecond
	// chanBuffer は Events / Removed / 内部の raw のバッファ長。
	chanBuffer = 8
)

// Watcher はディレクトリ内の .kage ファイルの変更を監視し、デバウンスしてから通知する。
//
// 保存が落ち着いた時点でファイルが存在すれば Events に、存在しなければ Removed に
// パスを送る。Close() の後は Events と Removed の両方が close される。
type Watcher struct {
	Events  <-chan string // 外部公開: 変更・新規作成されたファイルのパス（受信専用）
	Removed <-chan string // 外部公開: 削除・移動で無くなったファイルのパス（受信専用。満杯なら捨てる）

	events  chan string       // Events の送信側
	removed chan string       // Removed の送信側
	raw     chan string       // 取捨を済ませた fsnotify イベントのパス
	quit    chan struct{}     // Close() で close する停止シグナル
	once    sync.Once         // Close() の二重呼び出し防止
	wg      sync.WaitGroup    // 読み取り・デバウンス・タイマーの goroutine を待ち合わせる
	delay   time.Duration     // デバウンス間隔
	exists  func(string) bool // 発火時の存在確認（テストで差し替える）
	pending atomic.Int64      // デバウンス待ちのパス数（テストでの map リーク検査用）
	watcher *fsnotify.Watcher // nil のときはテスト用（fsnotify を使わない）
}

// New は dir を監視する Watcher を返す。失敗したときはエラーを返すだけでログには出さない。
func New(dir string) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := fw.Add(dir); err != nil {
		return nil, errors.Join(err, fw.Close())
	}

	w := newWatcher(defaultDelay, fileExists)
	w.watcher = fw
	w.wg.Add(1)
	go w.readLoop()
	w.start()

	return w, nil
}

// newForTest は fsnotify を使わずデバウンスループだけを動かすテスト用コンストラクタ。
// 発火時のファイルは常に存在するものとして扱う。
func newForTest(delay time.Duration) *Watcher {
	return newForTestWithStat(delay, func(string) bool { return true })
}

// newForTestWithStat は存在確認を差し替えられるテスト用コンストラクタ。
func newForTestWithStat(delay time.Duration, exists func(string) bool) *Watcher {
	w := newWatcher(delay, exists)
	w.start()

	return w
}

func newWatcher(delay time.Duration, exists func(string) bool) *Watcher {
	events := make(chan string, chanBuffer)
	removed := make(chan string, chanBuffer)

	return &Watcher{
		Events:  events,
		Removed: removed,
		events:  events,
		removed: removed,
		raw:     make(chan string, chanBuffer),
		quit:    make(chan struct{}),
		delay:   delay,
		exists:  exists,
	}
}

func (w *Watcher) start() {
	w.wg.Add(1)
	go w.debounceLoop()
}

// Close は監視を止め、すべての goroutine の終了を待ってから戻る。
// Events と Removed は送信側（debounceLoop）が止まる時点で close される。
// 何度呼んでもよい。
func (w *Watcher) Close() {
	w.once.Do(func() {
		close(w.quit)
		if w.watcher != nil {
			if err := w.watcher.Close(); err != nil {
				log.Println(err)
			}
		}
	})
	w.wg.Wait()
}

// pendingCount はデバウンス待ちのパス数を返す（テスト用）。
func (w *Watcher) pendingCount() int {
	return int(w.pending.Load())
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	// 権限エラーなど「在るが読めない」場合は存在扱いにし、読み込み側でエラーを出させる
	return !errors.Is(err, fs.ErrNotExist)
}

// shouldHandle は fsnotify イベントをデバウンスに回すかを決める。
//
//   - 対象は拡張子 .kage のファイルだけ
//   - 名前が "." で始まるもの（Emacs のロック .#foo.kage、macOS の ._foo.kage など）と
//     "#" で始まるもの（Emacs の自動保存）は一時ファイルとして無視する
//   - Write / Create に加え、別名で書いてから rename する保存方式に追従するため
//     Rename / Remove も扱う（存在するかどうかは発火時に os.Stat で判定する）
func shouldHandle(ev fsnotify.Event) bool {
	base := filepath.Base(ev.Name)
	if strings.HasPrefix(base, ".") || strings.HasPrefix(base, "#") {
		return false
	}
	if filepath.Ext(base) != ".kage" {
		return false
	}

	return ev.Has(fsnotify.Write) || ev.Has(fsnotify.Create) ||
		ev.Has(fsnotify.Rename) || ev.Has(fsnotify.Remove)
}

// readLoop は fsnotify のイベントを取捨して raw に流す。
func (w *Watcher) readLoop() {
	defer w.wg.Done()
	for {
		select {
		case ev, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			if !shouldHandle(ev) {
				continue
			}
			log.Printf("event: %s", ev)
			select {
			case w.raw <- ev.Name:
			case <-w.quit:
				return
			}
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			log.Println(err)
		case <-w.quit:
			return
		}
	}
}

// debounceLoop はパスごとにタイマーを持ち、delay の間に新しいイベントが来なければ通知する。
//
// タイマーの同一性は *time.Timer ではなく世代番号で判定する。AfterFunc のコールバックは
// 別 goroutine で走るため、戻り値の *time.Timer をコールバック内で読むとデータ競合になる。
// 世代番号はループ内で採番し、クロージャには値で渡す。番号はパスをまたいで単調増加させ、
// map のエントリを消して作り直しても古いコールバックの番号と衝突しないようにしている。
//
// Events / Removed に送るのはこの goroutine だけなので、ここを抜けるときに close する。
func (w *Watcher) debounceLoop() {
	type pendingTimer struct {
		timer *time.Timer
		gen   uint64
	}
	type firedMsg struct {
		path string
		gen  uint64
	}

	timers := map[string]pendingTimer{}
	fired := make(chan firedMsg)
	var gen uint64

	defer w.wg.Done()
	defer close(w.removed)
	defer close(w.events)
	defer func() {
		for _, p := range timers {
			if p.timer.Stop() {
				w.wg.Done() // コールバックは走らないので、代わりに数を戻す
			}
		}
		w.pending.Store(0)
	}()

	for {
		select {
		case path := <-w.raw:
			if p, ok := timers[path]; ok {
				if p.timer.Stop() {
					w.wg.Done()
				}
				// Stop に間に合わず走り出したコールバックは、世代番号の不一致で捨てられる
			} else {
				w.pending.Add(1)
			}
			gen++
			g := gen
			w.wg.Add(1)
			t := time.AfterFunc(w.delay, func() {
				defer w.wg.Done()
				select {
				case fired <- firedMsg{path: path, gen: g}:
				case <-w.quit:
				}
			})
			timers[path] = pendingTimer{timer: t, gen: g}

		case msg := <-fired:
			p, ok := timers[msg.path]
			if !ok || p.gen != msg.gen {
				continue // 置き換え済みの古いタイマー
			}
			delete(timers, msg.path)
			w.pending.Add(-1)
			if !w.deliver(msg.path) {
				return
			}

		case <-w.quit:
			return
		}
	}
}

// deliver は発火したパスを存在の有無で振り分けて送る。Close された場合は false を返す。
func (w *Watcher) deliver(path string) bool {
	if w.exists(path) {
		select {
		case w.events <- path:
			return true
		case <-w.quit:
			return false
		}
	}

	// Removed は受け手がいなくても止まらないよう、満杯なら捨てる
	select {
	case w.removed <- path:
	default:
		log.Printf("filewatcher: Removed is full, dropped %s", path)
	}

	return true
}
