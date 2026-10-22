package control

import (
	"errors"
	"fmt"
	"go/scanner"
	"go/token"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cyokozai/kagelife/internal/shadermgr"
	"github.com/cyokozai/kagelife/internal/tempo"
)

// fakeShader はテスト用のシェーダー。Dispose されたかを記録する。
type fakeShader struct {
	src      string
	disposed atomic.Bool
}

func (f *fakeShader) Dispose() { f.disposed.Store(true) }

// fakeCompiler はソース中の目印で失敗の種類を切り替えるコンパイラ。
//   - "SYNTAX_FAIL": 2 行目と 999 行目の scanner.ErrorList
//   - "SEMANTIC_FAIL": "3:5: unexpected identifier: Nope" と範囲外の 500 行目
//
// それ以外は成功し、作った fakeShader を記録する。
type fakeCompiler struct {
	mu   sync.Mutex
	made []*fakeShader
}

func (c *fakeCompiler) compile(src []byte) (shadermgr.Shader, error) {
	s := string(src)
	switch {
	case strings.Contains(s, "SYNTAX_FAIL"):
		var el scanner.ErrorList
		el.Add(token.Position{Line: 2, Column: 7}, "expected ';', found 'EOF'")
		el.Add(token.Position{Line: 999, Column: 1}, "internal junk")

		return nil, el
	case strings.Contains(s, "SEMANTIC_FAIL"):
		return nil, errors.New("3:5: unexpected identifier: Nope\n500:1: internal junk")
	}

	sh := &fakeShader{src: s}
	c.mu.Lock()
	c.made = append(c.made, sh)
	c.mu.Unlock()

	return sh, nil
}

func (c *fakeCompiler) last() *fakeShader {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.made) == 0 {
		return nil
	}

	return c.made[len(c.made)-1]
}

// newTestEngine は dir を shader_dir とし、names の順に fakeShader を登録した Engine を作る。
func newTestEngine(dir string, names ...string) *Engine {
	sm := shadermgr.New(func([]byte) (shadermgr.Shader, error) { return &fakeShader{}, nil })
	for _, n := range names {
		sm.Install(filepath.Join(dir, n+".kage"), &fakeShader{src: n})
	}

	return &Engine{
		SM:        sm,
		Tapper:    tempo.New(2*time.Second, 8),
		ShaderDir: dir,
		FPS:       func() float64 { return 59.9 },
	}
}

// runLoop は Update の代わりに 1ms ごとに Drain するゴルーチンを起こし、止める関数を返す。
func runLoop(q *Queue, e *Engine) (stop func()) {
	quit := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-quit:
				return
			case <-tick.C:
				q.Drain(e)
			}
		}
	}()

	var once sync.Once

	return func() {
		once.Do(func() {
			close(quit)
			<-done
		})
	}
}

func src(body string) string {
	return fmt.Sprintf("//kage:unit pixels\npackage main\n%s\n", body)
}
