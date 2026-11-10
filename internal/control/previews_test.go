package control

import (
	"bytes"
	"encoding/base64"
	"image/jpeg"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// fakeClock は差し替え用の時計。Advance で進める。
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 11, 21, 20, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// cellColor はテスト用のアトラスで、キーの位置 i（0〜8）のセルを塗る色。
func cellColor(i int) [3]byte {
	return [3]byte{byte(20 + i*25), byte(230 - i*25), 128}
}

// testAtlas はセルごとに cellColor で塗った PreviewAtlasSize 四方の RGBA（乗算済み、不透明）を返す。
func testAtlas() []byte {
	pix := make([]byte, PreviewAtlasSize*PreviewAtlasSize*4)
	for y := range PreviewAtlasSize {
		for x := range PreviewAtlasSize {
			c := cellColor((y/PreviewSize)*PreviewCols + x/PreviewSize)
			o := (y*PreviewAtlasSize + x) * 4
			pix[o], pix[o+1], pix[o+2], pix[o+3] = c[0], c[1], c[2], 255
		}
	}

	return pix
}

// waitFrame は prev のフレーム番号が want 以上になるまで待つ（符号化はゲームループの外で走る）。
func waitFrame(t *testing.T, p *Previews, want int) PreviewFrame {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		f := p.latest
		p.mu.Unlock()
		if f.Frame >= want && !p.Busy() {
			return f
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("frame が %d にならない", want)

	return PreviewFrame{}
}

func newPreviewsHarness(t *testing.T, loop bool, slots ...PreviewSlot) *harness {
	t.Helper()
	h := newHarness(t, loop, "a")
	h.eng.PreviewSlots = func() []PreviewSlot { return append([]PreviewSlot(nil), slots...) }

	return h
}

// keysOf は応答の keys を取り出す。
func keysOf(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	raw, ok := body["keys"].([]any)
	if !ok {
		t.Fatalf("keys が配列でない: %v", body)
	}
	out := make([]map[string]any, len(raw))
	for i, k := range raw {
		out[i] = k.(map[string]any)
	}

	return out
}

// 止まっている状態からの最初の要求は、割り当てを jpeg null で即答し、描き始める。
func TestPreviews_FirstRequestIsNullAndStartsRendering(t *testing.T) {
	h := newPreviewsHarness(t, true,
		PreviewSlot{Key: "1", Name: "01_uv"},
		PreviewSlot{Key: "3", Name: "bad", CompileError: true},
	)
	if h.prev.Wanted() {
		t.Fatal("要求の前から描こうとしている")
	}

	st, body := h.do("GET", "/v1/previews", "")
	if st != 200 {
		t.Fatalf("status = %d, body = %v", st, body)
	}
	if body["size"] != 72.0 || body["fps"] != 10.0 || body["frame"] != 0.0 {
		t.Errorf("size/fps/frame = %v %v %v, want 72 10 0", body["size"], body["fps"], body["frame"])
	}
	keys := keysOf(t, body)
	if len(keys) != 2 {
		t.Fatalf("keys = %v, want 2", keys)
	}
	want := []map[string]any{
		{"key": "1", "name": "01_uv", "jpeg": nil, "compile_error": false},
		{"key": "3", "name": "bad", "jpeg": nil, "compile_error": true},
	}
	for i := range want {
		for f, v := range want[i] {
			got, ok := keys[i][f]
			if !ok || got != v {
				t.Errorf("keys[%d].%s = %v (present=%v), want %v", i, f, got, ok, v)
			}
		}
	}
	if !h.prev.Wanted() {
		t.Error("要求の後に描こうとしない")
	}
}

// 割り当てが無ければ keys は空の配列（null にしない）。PreviewSlots が未設定でも同じ。
func TestPreviews_NoSlotsIsEmptyArray(t *testing.T) {
	h := newHarness(t, true, "a")

	_, body := h.do("GET", "/v1/previews", "")
	if got := jsonBody(body["keys"]); got != "[]" {
		t.Errorf("keys = %s, want []", got)
	}
}

// 直近 2 秒以内に要求が無ければ描かない（時計は差し替える）。
func TestPreviews_StopsAfterTwoSecondsWithoutRequests(t *testing.T) {
	h := newPreviewsHarness(t, true, PreviewSlot{Key: "1", Name: "a"})

	h.do("GET", "/v1/previews", "")
	h.clk.Advance(PreviewIdle)
	if !h.prev.Wanted() {
		t.Error("ちょうど 2 秒後は描く")
	}
	h.clk.Advance(time.Millisecond)
	if h.prev.Wanted() {
		t.Error("2 秒を過ぎても描こうとしている")
	}
	if h.prev.Due() {
		t.Error("止まっているのに Due が true")
	}

	// 要求が来れば再開する
	h.do("GET", "/v1/previews", "")
	if !h.prev.Wanted() {
		t.Error("要求の後に再開しない")
	}
}

// Due は最大 10 fps（前回から 100ms 以上）で、符号化中は false。
func TestPreviews_DueRateLimit(t *testing.T) {
	h := newPreviewsHarness(t, true, PreviewSlot{Key: "1", Name: "a"})
	h.do("GET", "/v1/previews", "")

	if !h.prev.Due() {
		t.Fatal("要求直後の 1 回目が Due でない")
	}
	if h.prev.Due() {
		t.Error("同じ時刻に 2 回 Due")
	}
	h.clk.Advance(99 * time.Millisecond)
	if h.prev.Due() {
		t.Error("99ms で Due")
	}
	h.clk.Advance(time.Millisecond)

	// 符号化中は Due にしない（アトラスの画素を符号化が読んでいるため）
	h.prev.busy.Store(true)
	if h.prev.Due() {
		t.Error("符号化中に Due")
	}
	h.prev.busy.Store(false)
	if !h.prev.Due() {
		t.Error("100ms 後に Due でない")
	}
}

// Submit はアトラスのセルを 72x72 の JPEG にしてキーごとに返し、frame を 1 増やす。
// セルの位置はキーの数字で決まる（1 が左上、3 が右上、5 が中央、9 が右下）。
func TestPreviews_SubmitEncodesCells(t *testing.T) {
	h := newPreviewsHarness(t, true)
	slots := []PreviewSlot{
		{Key: "1", Name: "a"}, {Key: "3", Name: "c"}, {Key: "5", Name: "e"},
		{Key: "7", Name: "bad", CompileError: true}, {Key: "9", Name: "i"},
	}
	h.do("GET", "/v1/previews", "")
	if !h.prev.Due() {
		t.Fatal("Due でない")
	}
	h.prev.Submit(testAtlas(), slots)
	waitFrame(t, h.prev, 1)

	_, body := h.do("GET", "/v1/previews", "")
	if body["frame"] != 1.0 {
		t.Errorf("frame = %v, want 1", body["frame"])
	}
	keys := keysOf(t, body)
	if len(keys) != len(slots) {
		t.Fatalf("keys = %d, want %d", len(keys), len(slots))
	}
	for i, s := range slots {
		k := keys[i]
		if k["key"] != s.Key || k["name"] != s.Name || k["compile_error"] != s.CompileError {
			t.Errorf("keys[%d] = %v, want %+v", i, k, s)
		}
		if s.CompileError {
			if k["jpeg"] != nil {
				t.Errorf("key %s: コンパイルに失敗したスロットの jpeg = %v, want null", s.Key, k["jpeg"])
			}

			continue
		}
		b64, _ := k["jpeg"].(string)
		raw, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			t.Fatalf("key %s: base64: %v", s.Key, err)
		}
		img, err := jpeg.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("key %s: jpeg: %v", s.Key, err)
		}
		if b := img.Bounds(); b.Dx() != PreviewSize || b.Dy() != PreviewSize {
			t.Errorf("key %s: size = %v, want 72x72", s.Key, b)
		}
		r, g, bl, _ := img.At(36, 36).RGBA()
		want := cellColor(int(s.Key[0] - '1'))
		got := [3]int{int(r >> 8), int(g >> 8), int(bl >> 8)}
		for c := range 3 {
			if d := got[c] - int(want[c]); d < -12 || d > 12 {
				t.Errorf("key %s: 中央の色 = %v, want %v 付近（セルの位置がずれている）", s.Key, got, want)

				break
			}
		}
	}
}

// 止まった後の最初の要求は、前に描いた絵を返さず jpeg null で即答する。frame は戻さない。
func TestPreviews_StaleFrameIsNotReturnedAfterStop(t *testing.T) {
	h := newPreviewsHarness(t, true, PreviewSlot{Key: "1", Name: "a"})
	h.do("GET", "/v1/previews", "")
	h.prev.Due()
	h.prev.Submit(testAtlas(), []PreviewSlot{{Key: "1", Name: "a"}})
	waitFrame(t, h.prev, 1)

	h.clk.Advance(PreviewIdle + time.Second)
	_, body := h.do("GET", "/v1/previews", "")
	keys := keysOf(t, body)
	if len(keys) != 1 || keys[0]["jpeg"] != nil {
		t.Errorf("keys = %v, want 1 件で jpeg null", keys)
	}
	if body["frame"] != 1.0 {
		t.Errorf("frame = %v, want 1（戻さない）", body["frame"])
	}
}

// -previews=false（ServerConfig.Previews が nil、Engine.NoPreviews）なら、GET /v1/previews は 404 で、
// features に previews を出さない。keys は止めない。
func TestPreviews_Disabled(t *testing.T) {
	dir := t.TempDir()
	eng := newTestEngine(dir, "a")
	eng.NoPreviews = true
	fk := &fakeKeys{assigned: map[string]bool{"1": true}}
	eng.PressKey = fk.press
	q := NewQueue(16)
	h := &harness{t: t, dir: dir, eng: eng, q: q, comp: &fakeCompiler{}}
	h.srv = httptest.NewServer(NewServer(ServerConfig{Token: testToken, ShaderDir: dir, Compile: h.comp.compile, Queue: q, Timeout: 100 * time.Millisecond}))
	h.stop = runLoop(q, eng)
	t.Cleanup(func() {
		h.srv.Close()
		h.stop()
	})

	st, body := h.do("GET", "/v1/previews", "")
	assertError(t, st, body, 404, "not_found")

	_, s := h.do("GET", "/v1/state", "")
	if got, want := jsonBody(s["features"]), `["keys","llm_slots"]`; got != want {
		t.Errorf("features = %s, want %s", got, want)
	}
	if st, _ := h.do("POST", "/v1/keys", `{"key":"1"}`); st != 200 {
		t.Errorf("keys: status = %d, want 200", st)
	}
}

// 認証・メソッド・未知のクエリ・ループの停止。
func TestPreviews_AuthMethodQueryAndTimeout(t *testing.T) {
	h := newPreviewsHarness(t, true, PreviewSlot{Key: "1", Name: "a"})

	st, body := h.doAuth("GET", "/v1/previews", "", "")
	assertError(t, st, body, 401, "unauthorized")
	if h.prev.Wanted() {
		t.Error("認証に失敗した要求で描き始めた")
	}
	st, body = h.do("POST", "/v1/previews", "")
	assertError(t, st, body, 405, "method_not_allowed")

	st, _ = h.do("GET", "/v1/previews?size=144&x=1", "")
	if st != 200 {
		t.Errorf("未知のクエリ: status = %d, want 200", st)
	}

	stopped := newPreviewsHarness(t, false, PreviewSlot{Key: "1", Name: "a"})
	st, body = stopped.do("GET", "/v1/previews", "")
	assertError(t, st, body, 503, "loop_timeout")
}
