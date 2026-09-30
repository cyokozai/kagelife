package control

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type harness struct {
	t    *testing.T
	dir  string
	eng  *Engine
	q    *Queue
	comp *fakeCompiler
	srv  *httptest.Server
	stop func()
}

// newHarness は shader_dir に names のファイルを置き、同じ名前を Engine に登録した制御口を立てる。
// loop=false ならゲームループを回さない（loop_timeout の検証用）。
func newHarness(t *testing.T, loop bool, names ...string) *harness {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n+".kage"), []byte(src("// "+n)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	h := &harness{t: t, dir: dir, eng: newTestEngine(dir, names...), q: NewQueue(16), comp: &fakeCompiler{}, stop: func() {}}
	h.srv = httptest.NewServer(NewServer(ServerConfig{
		Token:     testToken,
		ShaderDir: dir,
		Compile:   h.comp.compile,
		Queue:     h.q,
		Timeout:   100 * time.Millisecond,
	}))
	if loop {
		h.stop = runLoop(h.q, h.eng)
	}
	t.Cleanup(func() {
		h.srv.Close()
		h.stop()
	})

	return h
}

// do は認証付きでリクエストし、ステータスと JSON 本文を返す。
func (h *harness) do(method, path, body string) (int, map[string]any) {
	h.t.Helper()

	return h.doAuth(method, path, body, "Bearer "+testToken)
}

func (h *harness) doAuth(method, path, body, auth string) (int, map[string]any) {
	h.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, h.srv.URL+path, rd)
	if err != nil {
		h.t.Fatal(err)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		h.t.Errorf("%s %s: Content-Type = %q, want application/json", method, path, ct)
	}
	var m map[string]any
	if err := json.NewDecoder(res.Body).Decode(&m); err != nil {
		h.t.Fatalf("%s %s: JSON でない応答: %v", method, path, err)
	}

	return res.StatusCode, m
}

// inspect はゲームループ上で fn を実行する（Engine を別ゴルーチンから直接触らない）。
func (h *harness) inspect(fn func(e *Engine)) {
	h.t.Helper()
	if _, err := call(h.q, time.Second, func(e *Engine) struct{} { fn(e); return struct{}{} }); err != nil {
		h.t.Fatal(err)
	}
}

func jsonBody(v any) string {
	b, _ := json.Marshal(v)

	return string(b)
}

func assertError(t *testing.T, status int, body map[string]any, wantStatus int, wantCode string) {
	t.Helper()
	if status != wantStatus || body["error"] != wantCode {
		t.Errorf("status=%d body=%v, want %d %s", status, body, wantStatus, wantCode)
	}
	if msg, _ := body["message"].(string); msg == "" {
		t.Errorf("message が空: %v", body)
	}
}

// ── 認証・ルーティング ───────────────────────────────────────────

func TestAuth(t *testing.T) {
	h := newHarness(t, true, "a")

	for _, auth := range []string{"", "Bearer wrong", "bearer " + testToken, testToken, "Bearer " + testToken + "x"} {
		st, body := h.doAuth("GET", "/v1/state", "", auth)
		assertError(t, st, body, 401, "unauthorized")
	}

	// 未定義パスでも認証が先
	st, body := h.doAuth("GET", "/nope", "", "")
	assertError(t, st, body, 401, "unauthorized")
}

func TestNotFoundAndMethodNotAllowed(t *testing.T) {
	h := newHarness(t, true, "a")

	for _, p := range []string{"/", "/v1", "/v1/nope", "/v1/capture", "/v1/uniforms", "/v1/shaders"} {
		st, body := h.do("GET", p, "")
		assertError(t, st, body, 404, "not_found")
	}

	cases := [][2]string{
		{"POST", "/v1/state"},
		{"DELETE", "/v1/shaders/a"},
		{"GET", "/v1/switch"},
		{"GET", "/v1/crossfade"},
		{"PUT", "/v1/bpm"},
	}
	for _, c := range cases {
		st, body := h.do(c[0], c[1], "")
		assertError(t, st, body, 405, "method_not_allowed")
	}
}

// ── GET /v1/state ───────────────────────────────────────────────

func TestGetState(t *testing.T) {
	h := newHarness(t, true, "01_uv", "02_time_sin")
	h.inspect(func(e *Engine) {
		e.Resolution = [2]int{1280, 720}
		e.RecordError("02_time_sin", "boom")
	})

	st, body := h.do("GET", "/v1/state", "")

	if st != 200 {
		t.Fatalf("status = %d, body = %v", st, body)
	}
	if body["active"] != "01_uv" || body["bpm"] != 0.0 || body["fps"] != 59.9 {
		t.Errorf("body = %v", body)
	}
	if got := jsonBody(body["shaders"]); got != `["01_uv","02_time_sin"]` {
		t.Errorf("shaders = %s", got)
	}
	if got := jsonBody(body["resolution"]); got != `[1280,720]` {
		t.Errorf("resolution = %s", got)
	}
	if got := jsonBody(body["fade"]); got != `{"beats":4,"fading":false,"mix":0,"target":""}` {
		t.Errorf("fade = %s", got)
	}
	if got := jsonBody(body["last_error"]); got != `{"message":"boom","shader":"02_time_sin"}` {
		t.Errorf("last_error = %s", got)
	}
}

func TestGetState_NullLastError(t *testing.T) {
	h := newHarness(t, true)

	_, body := h.do("GET", "/v1/state", "")

	if v, ok := body["last_error"]; !ok || v != nil {
		t.Errorf("last_error = %v (存在=%v), want null", v, ok)
	}
	if body["active"] != "" || jsonBody(body["shaders"]) != "[]" {
		t.Errorf("body = %v", body)
	}
}

func TestLoopTimeout(t *testing.T) {
	h := newHarness(t, false, "a")

	requests := [][3]string{
		{"GET", "/v1/state", ""},
		{"POST", "/v1/switch", `{"name":"a"}`},
		{"POST", "/v1/crossfade", `{"name":"a"}`},
		{"POST", "/v1/bpm", `{"bpm":120}`},
	}
	for _, r := range requests {
		st, body := h.do(r[0], r[1], r[2])
		assertError(t, st, body, 503, "loop_timeout")
	}
}

// ── GET /v1/shaders/{name} ──────────────────────────────────────

func TestGetShader(t *testing.T) {
	h := newHarness(t, true, "a")

	st, body := h.do("GET", "/v1/shaders/a", "")
	if st != 200 || body["name"] != "a" || body["source"] != src("// a") {
		t.Errorf("status=%d body=%v", st, body)
	}

	st, body = h.do("GET", "/v1/shaders/missing", "")
	assertError(t, st, body, 404, "not_found")

	st, body = h.do("GET", "/v1/shaders/Bad.Name", "")
	assertError(t, st, body, 400, "invalid_name")
}

// ── PUT /v1/shaders/{name} ──────────────────────────────────────

func TestPutShader_InvalidName(t *testing.T) {
	h := newHarness(t, true)

	for _, n := range []string{"UPPER", "_x", "a.kage", "a%20b", strings.Repeat("a", 65)} {
		st, body := h.do("PUT", "/v1/shaders/"+n, jsonBody(map[string]string{"source": src("")}))
		assertError(t, st, body, 400, "invalid_name")
	}
}

func TestPutShader_TooLarge(t *testing.T) {
	h := newHarness(t, true)
	big := src(strings.Repeat("/", 64*1024))

	st, body := h.do("PUT", "/v1/shaders/big", jsonBody(map[string]string{"source": big}))
	assertError(t, st, body, 413, "too_large")

	// ちょうど 64KiB は通る
	exact := src("")
	exact += strings.Repeat("/", 64*1024-len(exact))
	st, body = h.do("PUT", "/v1/shaders/exact", jsonBody(map[string]string{"source": exact}))
	if st != 200 {
		t.Errorf("64KiB ちょうど: status=%d body=%v", st, body)
	}

	// JSON 全体が極端に大きい場合も 413
	huge := `{"source":"` + strings.Repeat("a", 2<<20) + `"}`
	st, body = h.do("PUT", "/v1/shaders/huge", huge)
	assertError(t, st, body, 413, "too_large")

	if _, err := os.Stat(filepath.Join(h.dir, "big.kage")); !os.IsNotExist(err) {
		t.Error("413 なのにファイルが書かれた")
	}
}

func TestPutShader_InvalidRequest(t *testing.T) {
	h := newHarness(t, true)

	for _, b := range []string{"", "not json", `{"source":1}`, `[]`} {
		st, body := h.do("PUT", "/v1/shaders/x", b)
		assertError(t, st, body, 400, "invalid_request")
	}
}

func TestPutShader_UnitPixelsRequired(t *testing.T) {
	h := newHarness(t, true)

	st, body := h.do("PUT", "/v1/shaders/x", jsonBody(map[string]string{"source": "package main\n"}))

	assertError(t, st, body, 422, "unit_pixels_required")
	if h.comp.last() != nil {
		t.Error("unit 検査に落ちたのにコンパイルされた")
	}
}

func TestPutShader_CompileErrorSyntax(t *testing.T) {
	h := newHarness(t, true, "a")
	before, _ := os.ReadFile(filepath.Join(h.dir, "a.kage"))

	st, body := h.do("PUT", "/v1/shaders/a", jsonBody(map[string]string{"source": src("SYNTAX_FAIL")}))

	assertError(t, st, body, 422, "compile_error")
	if got := jsonBody(body["diagnostics"]); got != `[{"col":7,"line":2,"message":"expected ';', found 'EOF'"}]` {
		t.Errorf("diagnostics = %s", got)
	}
	if !strings.Contains(body["message"].(string), "2:7") {
		t.Errorf("message = %q", body["message"])
	}
	after, _ := os.ReadFile(filepath.Join(h.dir, "a.kage"))
	if string(before) != string(after) {
		t.Error("コンパイル失敗なのにファイルが変わった")
	}
}

func TestPutShader_CompileErrorSemanticRecordsLastError(t *testing.T) {
	h := newHarness(t, true)

	st, body := h.do("PUT", "/v1/shaders/newone", jsonBody(map[string]string{"source": src("SEMANTIC_FAIL")}))

	assertError(t, st, body, 422, "compile_error")
	if got := jsonBody(body["diagnostics"]); got != `[{"col":5,"line":3,"message":"unexpected identifier: Nope"}]` {
		t.Errorf("diagnostics = %s", got)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "newone.kage")); !os.IsNotExist(err) {
		t.Error("コンパイル失敗なのにファイルが作られた")
	}

	// last_error にも残る（投函は非同期なので少し待つ）
	deadline := time.Now().Add(time.Second)
	for {
		_, s := h.do("GET", "/v1/state", "")
		if le, ok := s["last_error"].(map[string]any); ok && le["shader"] == "newone" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("last_error が記録されない: %v", s["last_error"])
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestPutShader_CreatesNew(t *testing.T) {
	h := newHarness(t, true, "a")
	source := src("// new")

	st, body := h.do("PUT", "/v1/shaders/fresh", jsonBody(map[string]string{"source": source}))

	if st != 200 || body["name"] != "fresh" || body["created"] != true {
		t.Fatalf("status=%d body=%v", st, body)
	}
	got, err := os.ReadFile(filepath.Join(h.dir, "fresh.kage"))
	if err != nil || string(got) != source {
		t.Errorf("ファイル = %q, %v", got, err)
	}
	entries, _ := os.ReadDir(h.dir)
	if len(entries) != 2 {
		t.Errorf("一時ファイルが残っている: %v", entries)
	}
	var names []string
	h.inspect(func(e *Engine) { names = e.State().Shaders })
	if jsonBody(names) != `["a","fresh"]` {
		t.Errorf("shaders = %v", names)
	}
}

func TestPutShader_ReplacesExisting(t *testing.T) {
	h := newHarness(t, true, "a", "b")
	var old *fakeShader
	h.inspect(func(e *Engine) { old = e.SM.Active().(*fakeShader) })
	source := src("// replaced")

	st, body := h.do("PUT", "/v1/shaders/a", jsonBody(map[string]string{"source": source}))

	if st != 200 || body["created"] != false {
		t.Fatalf("status=%d body=%v", st, body)
	}
	got, _ := os.ReadFile(filepath.Join(h.dir, "a.kage"))
	if string(got) != source {
		t.Errorf("ファイル = %q", got)
	}
	var active *fakeShader
	var n int
	h.inspect(func(e *Engine) { active = e.SM.Active().(*fakeShader); n = e.SM.Len() })
	if active != h.comp.last() || n != 2 {
		t.Errorf("差し替わっていない: active=%p last=%p len=%d", active, h.comp.last(), n)
	}
	if !old.disposed.Load() {
		t.Error("旧シェーダーが Dispose されていない")
	}
}

func TestPutShader_LoopTimeoutStillWritesFile(t *testing.T) {
	h := newHarness(t, false)

	st, body := h.do("PUT", "/v1/shaders/x", jsonBody(map[string]string{"source": src("")}))

	assertError(t, st, body, 503, "loop_timeout")
	if _, err := os.Stat(filepath.Join(h.dir, "x.kage")); err != nil {
		t.Errorf("ファイルが書かれていない（fsnotify 経由の反映に委ねる）: %v", err)
	}
}

// ── POST /v1/switch ─────────────────────────────────────────────

func TestSwitch(t *testing.T) {
	h := newHarness(t, true, "a", "b")

	st, body := h.do("POST", "/v1/switch", `{"name":"b"}`)
	if st != 200 || body["active"] != "b" {
		t.Errorf("status=%d body=%v", st, body)
	}

	st, body = h.do("POST", "/v1/switch", `{"name":"zzz"}`)
	assertError(t, st, body, 404, "not_found")

	st, body = h.do("POST", "/v1/switch", `{"name":"../a"}`)
	assertError(t, st, body, 400, "invalid_name")

	st, body = h.do("POST", "/v1/switch", `nope`)
	assertError(t, st, body, 400, "invalid_request")
}

// ── POST /v1/crossfade ──────────────────────────────────────────

func TestCrossfade(t *testing.T) {
	h := newHarness(t, true, "a", "b")

	st, body := h.do("POST", "/v1/crossfade", `{"name":"b"}`)
	assertError(t, st, body, 409, "bpm_not_set")

	h.do("POST", "/v1/bpm", `{"bpm":120}`)

	st, body = h.do("POST", "/v1/crossfade", `{"name":"zzz","beats":4}`)
	assertError(t, st, body, 404, "not_found")

	for _, b := range []string{"0", "-1", "64.5", "1000"} {
		st, body = h.do("POST", "/v1/crossfade", `{"name":"b","beats":`+b+`}`)
		assertError(t, st, body, 400, "invalid_beats")
	}

	st, body = h.do("POST", "/v1/crossfade", `{"name":"b"}`)
	if st != 200 || body["target"] != "b" || body["beats"] != 4.0 {
		t.Errorf("省略時: status=%d body=%v", st, body)
	}

	st, body = h.do("POST", "/v1/crossfade", `{"name":"a","beats":64}`)
	if st != 200 || body["target"] != "a" || body["beats"] != 64.0 {
		t.Errorf("64 拍: status=%d body=%v", st, body)
	}
	_, s := h.do("GET", "/v1/state", "")
	if fade := s["fade"].(map[string]any); fade["fading"] != true || fade["target"] != "a" || fade["beats"] != 64.0 {
		t.Errorf("fade = %v", fade)
	}

	st, body = h.do("POST", "/v1/crossfade", `{"name":"B"}`)
	assertError(t, st, body, 400, "invalid_name")
}

// ── POST /v1/bpm ────────────────────────────────────────────────

func TestBPM(t *testing.T) {
	h := newHarness(t, true)

	st, body := h.do("POST", "/v1/bpm", `{"bpm":128}`)
	if st != 200 || body["bpm"] != 128.0 {
		t.Errorf("status=%d body=%v", st, body)
	}
	_, s := h.do("GET", "/v1/state", "")
	if s["bpm"] != 128.0 {
		t.Errorf("state.bpm = %v", s["bpm"])
	}

	for _, b := range []string{"20", "300"} {
		st, _ := h.do("POST", "/v1/bpm", `{"bpm":`+b+`}`)
		if st != 200 {
			t.Errorf("bpm=%s: status=%d", b, st)
		}
	}
	for _, b := range []string{`{"bpm":19.9}`, `{"bpm":300.1}`, `{"bpm":0}`} {
		st, body := h.do("POST", "/v1/bpm", b)
		assertError(t, st, body, 400, "invalid_bpm")
	}
}

// ── 補足（契約 v1 の確定事項） ─────────────────────────────────

// 補足 1: 必須項目の欠落は 400 invalid_request。
func TestMissingRequiredFields(t *testing.T) {
	h := newHarness(t, true, "a")

	cases := [][3]string{
		{"PUT", "/v1/shaders/x", `{}`},
		{"PUT", "/v1/shaders/x", `{"source":null}`},
		{"POST", "/v1/switch", `{}`},
		{"POST", "/v1/crossfade", `{"beats":4}`},
		{"POST", "/v1/bpm", `{}`},
		{"POST", "/v1/bpm", `null`},
	}
	for _, c := range cases {
		st, body := h.do(c[0], c[1], c[2])
		assertError(t, st, body, 400, "invalid_request")
	}
}

// 補足 3: / や .. を含む名前（エスケープされたものも）はルーティングで 404 にせず 400 invalid_name。
func TestNameWithSlashOrDots(t *testing.T) {
	h := newHarness(t, true, "a")

	for _, p := range []string{"a/b", "a%2Fb", "..%2Fx", "..", "%2e%2e", "../x", "%2e%2e%2fa", ""} {
		for _, m := range []string{"GET", "PUT"} {
			st, body := h.do(m, "/v1/shaders/"+p, jsonBody(map[string]string{"source": src("")}))
			assertError(t, st, body, 400, "invalid_name")
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(h.dir), "x.kage")); !os.IsNotExist(err) {
		t.Error("shader_dir の外に書き込まれた")
	}
}

// 補足 2: crossfade の判定順は beats → 名前の存在 → BPM。
func TestCrossfadeCheckOrder(t *testing.T) {
	h := newHarness(t, true, "a", "b")

	st, body := h.do("POST", "/v1/crossfade", `{"name":"zzz","beats":0}`)
	assertError(t, st, body, 400, "invalid_beats")

	st, body = h.do("POST", "/v1/crossfade", `{"name":"zzz"}`) // BPM 未設定でも 404 が先
	assertError(t, st, body, 404, "not_found")
}

// 補足 5: PUT は成功してもアクティブにしない。
func TestPutShader_DoesNotActivate(t *testing.T) {
	h := newHarness(t, true, "a", "b")
	h.do("POST", "/v1/switch", `{"name":"b"}`)

	for _, n := range []string{"fresh", "a"} {
		st, _ := h.do("PUT", "/v1/shaders/"+n, jsonBody(map[string]string{"source": src("// " + n)}))
		if st != 200 {
			t.Fatalf("PUT %s: status = %d", n, st)
		}
		_, s := h.do("GET", "/v1/state", "")
		if s["active"] != "b" {
			t.Errorf("PUT %s 後の active = %v, want b", n, s["active"])
		}
	}
}

// 補足 4: last_error は失敗のたびに上書きし、どのシェーダでも次に成功したら null。
func TestLastErrorOverwriteAndClear(t *testing.T) {
	h := newHarness(t, true, "a")
	waitShader := func(want any) {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for {
			_, s := h.do("GET", "/v1/state", "")
			var got any
			if le, ok := s["last_error"].(map[string]any); ok {
				got = le["shader"]
			}
			if got == want {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("last_error.shader = %v, want %v", got, want)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	h.do("PUT", "/v1/shaders/x", jsonBody(map[string]string{"source": src("SEMANTIC_FAIL")}))
	waitShader("x")
	h.do("PUT", "/v1/shaders/y", jsonBody(map[string]string{"source": src("SYNTAX_FAIL")}))
	waitShader("y")
	h.do("PUT", "/v1/shaders/z", jsonBody(map[string]string{"source": src("// ok")}))
	waitShader(nil)
}
