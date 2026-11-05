package control

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cyokozai/kagelife/internal/shadermgr"
)

// ── -llm-slots の検査 ──────────────────────────────────────────

func TestParseLLMSlots(t *testing.T) {
	ok := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"llm_a", []string{"llm_a"}},
		{"llm_a,llm_b", []string{"llm_a", "llm_b"}},
		{"llm_b,llm_a", []string{"llm_b", "llm_a"}}, // 並びはフラグの順のまま
		{" llm_a , llm_b ", []string{"llm_a", "llm_b"}},
	}
	for _, c := range ok {
		got, err := ParseLLMSlots(c.in)
		if err != nil {
			t.Errorf("ParseLLMSlots(%q) = %v", c.in, err)

			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseLLMSlots(%q) = %#v, want %#v", c.in, got, c.want)
		}
	}

	bad := []struct {
		in, wantInMsg string
	}{
		{"llm_a,llm_a", "llm_a"},        // 重複
		{"llm_a , llm_a", "llm_a"},      // 空白を除いてから重複を見る
		{"llm_a,llm_b,llm_c", "3"},      // 3 個以上
		{"llm_a,llm_b,llm_a", "3"},      // 個数を重複より先に見る
		{"llm_a,llm_b,", "3"},           // 末尾のカンマも 1 個に数える
		{"LLM_A", "LLM_A"},              // 規則違反（大文字）
		{"llm_a,LLM_B", "LLM_B"},        // 2 個目の規則違反
		{"llm_a.kage", "llm_a.kage"},    // 拡張子付き
		{"_llm", "_llm"},                // 先頭が記号
		{"llm a", "llm a"},              // 空白入り
		{"llm_a,../x", "../x"},          // パス
		{strings.Repeat("a", 65), "aa"}, // 65 文字
		{"llm_a,", `""`},                // 空の名前
		{",", `""`},                     // 空だけ
	}
	for _, c := range bad {
		got, err := ParseLLMSlots(c.in)
		if err == nil {
			t.Errorf("ParseLLMSlots(%q) = %#v, want error", c.in, got)

			continue
		}
		if !strings.Contains(err.Error(), c.wantInMsg) {
			t.Errorf("ParseLLMSlots(%q) error = %q, want it to contain %q", c.in, err, c.wantInMsg)
		}
		if !strings.Contains(err.Error(), "-llm-slots") {
			t.Errorf("ParseLLMSlots(%q) error = %q, want it to name the flag", c.in, err)
		}
	}
}

// ── state の llm_slots ─────────────────────────────────────────

func TestGetState_LLMSlotsEmptyArrayWithoutFlag(t *testing.T) {
	h := newHarness(t, true, "a")

	_, body := h.do("GET", "/v1/state", "")

	v, ok := body["llm_slots"]
	if !ok {
		t.Fatalf("llm_slots が無い: %v", body)
	}
	if got := jsonBody(v); got != "[]" {
		t.Errorf("llm_slots = %s, want []", got)
	}
}

func TestGetState_LLMSlotsInFlagOrder(t *testing.T) {
	h := newHarnessLLM(t, true, []string{"llm_b", "llm_a"}, "a")

	_, body := h.do("GET", "/v1/state", "")

	if got := jsonBody(body["llm_slots"]); got != `["llm_b","llm_a"]` {
		t.Errorf("llm_slots = %s, want [\"llm_b\",\"llm_a\"]", got)
	}
}

// Engine.State の LLMSlots は Engine の設定と共有しない。
func TestEngineState_LLMSlotsCopy(t *testing.T) {
	e := newTestEngine(t.TempDir())
	e.LLMSlots = []string{"llm_a", "llm_b"}

	got := e.State().LLMSlots
	got[0] = "mutated"
	if e.LLMSlots[0] != "llm_a" {
		t.Error("State().LLMSlots が Engine.LLMSlots を共有している")
	}
}

// ── 「映っている」の判定 ───────────────────────────────────────

func TestEngineLive(t *testing.T) {
	t.Run("active と同じ名前は映っている", func(t *testing.T) {
		e := newTestEngine(t.TempDir(), "a", "llm_a", "llm_b")
		_ = e.Switch("llm_a")
		if !e.Live("llm_a") || e.Live("llm_b") {
			t.Errorf("Live(llm_a)=%v Live(llm_b)=%v, want true/false", e.Live("llm_a"), e.Live("llm_b"))
		}
	})

	t.Run("フェード中の target は映っている", func(t *testing.T) {
		e := newTestEngine(t.TempDir(), "a", "llm_a", "llm_b")
		if _, err := e.Crossfade("llm_b", 4); err != nil {
			t.Fatal(err)
		}
		if !e.Live("llm_b") || !e.Live("a") || e.Live("llm_a") {
			t.Errorf("Live(llm_b)=%v Live(a)=%v Live(llm_a)=%v", e.Live("llm_b"), e.Live("a"), e.Live("llm_a"))
		}
	})

	t.Run("フェードを終えた後の名残は映っていない", func(t *testing.T) {
		e := newTestEngine(t.TempDir(), "a", "llm_a", "llm_b")
		if _, err := e.Crossfade("llm_b", 4); err != nil {
			t.Fatal(err)
		}
		_ = e.Switch("a") // フェードを打ち切る
		if e.Live("llm_b") {
			t.Error("Live(llm_b) = true after the fade ended")
		}
	})

	t.Run("シェーダが無ければ何も映っていない", func(t *testing.T) {
		e := newTestEngine(t.TempDir())
		if e.Live("llm_a") {
			t.Error("Live(llm_a) = true on an empty engine")
		}
	})

	t.Run("コンパイルに失敗した active は映っていない", func(t *testing.T) {
		dir := t.TempDir()
		e := newTestEngine(dir)
		e.SM = shadermgr.New(func([]byte) (shadermgr.Shader, error) { return nil, errors.New("boom") })
		path := filepath.Join(dir, "llm_a.kage")
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		_ = e.SM.Load(path) // nil のスロットが active になる
		if e.Live("llm_a") {
			t.Error("Live(llm_a) = true for a slot that draws nothing")
		}
	})

	t.Run("読み込まれていない名前は映っていない", func(t *testing.T) {
		e := newTestEngine(t.TempDir(), "a")
		if e.Live("llm_a") {
			t.Error("Live(llm_a) = true for a missing shader")
		}
	})
}

// ── PUT の 403 / 409 ───────────────────────────────────────────

var llmPair = []string{"llm_a", "llm_b"}

func putBody(name string) string {
	return jsonBody(map[string]string{"source": src("// " + name)})
}

func TestPutShader_LLMSlots_ForbiddenName(t *testing.T) {
	h := newHarnessLLM(t, true, llmPair, "a")

	st, body := h.do("PUT", "/v1/shaders/x", putBody("x"))
	assertError(t, st, body, 403, "forbidden_name")
	msg, _ := body["message"].(string)
	for _, want := range []string{"llm_a", "llm_b"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message = %q, want it to list %q", msg, want)
		}
	}
	// 既存の予約名以外も上書きさせない
	st, body = h.do("PUT", "/v1/shaders/a", putBody("a"))
	assertError(t, st, body, 403, "forbidden_name")

	if _, err := os.Stat(filepath.Join(h.dir, "x.kage")); !os.IsNotExist(err) {
		t.Errorf("x.kage が書かれた: %v", err)
	}
}

func TestPutShader_LLMSlots_ReservedNotLiveIsWritten(t *testing.T) {
	h := newHarnessLLM(t, true, llmPair, "a")

	st, body := h.do("PUT", "/v1/shaders/llm_a", putBody("llm_a"))
	if st != 200 || body["name"] != "llm_a" || body["created"] != true {
		t.Fatalf("status=%d body=%v", st, body)
	}
	// 書き込んでも表示は変えない（ADR-006 補足 14）
	if _, s := h.do("GET", "/v1/state", ""); s["active"] != "a" {
		t.Errorf("active = %v, want a", s["active"])
	}
}

func TestPutShader_LLMSlots_ActiveIsLive(t *testing.T) {
	h := newHarnessLLM(t, true, llmPair, "a", "llm_a", "llm_b")
	h.inspect(func(e *Engine) { _ = e.Switch("llm_a") })

	st, body := h.do("PUT", "/v1/shaders/llm_a", putBody("llm_a"))
	assertError(t, st, body, 409, "slot_live")
	if msg, _ := body["message"].(string); !strings.Contains(msg, "llm_b") {
		t.Errorf("message = %q, want it to point to the other slot llm_b", msg)
	}

	// もう一方には書ける
	if st, body := h.do("PUT", "/v1/shaders/llm_b", putBody("llm_b")); st != 200 {
		t.Errorf("PUT llm_b: status=%d body=%v", st, body)
	}
}

func TestPutShader_LLMSlots_FadeTargetIsLive(t *testing.T) {
	h := newHarnessLLM(t, true, llmPair, "a", "llm_a", "llm_b")
	h.inspect(func(e *Engine) {
		if _, err := e.Crossfade("llm_b", 4); err != nil {
			t.Error(err)
		}
	})

	st, body := h.do("PUT", "/v1/shaders/llm_b", putBody("llm_b"))
	assertError(t, st, body, 409, "slot_live")
	if msg, _ := body["message"].(string); !strings.Contains(msg, "llm_a") {
		t.Errorf("message = %q, want it to point to the other slot llm_a", msg)
	}
	if st, _ := h.do("PUT", "/v1/shaders/llm_a", putBody("llm_a")); st != 200 {
		t.Errorf("PUT llm_a: status=%d, want 200", st)
	}
}

// 予約名が 1 個のときの 409 の message でも、書けない理由が分かる。
func TestPutShader_LLMSlots_SingleSlotLive(t *testing.T) {
	h := newHarnessLLM(t, true, []string{"llm_a"}, "llm_a")

	st, body := h.do("PUT", "/v1/shaders/llm_a", putBody("llm_a"))
	assertError(t, st, body, 409, "slot_live")
}

// 表示中のシェーダが無いとき PUT で表示される振る舞い（補足 14）は予約名でも同じ。
func TestPutShader_LLMSlots_IntoEmptyBecomesActive(t *testing.T) {
	h := newHarnessLLM(t, true, llmPair)

	if st, body := h.do("PUT", "/v1/shaders/llm_a", putBody("llm_a")); st != 200 {
		t.Fatalf("status=%d body=%v", st, body)
	}
	if _, s := h.do("GET", "/v1/state", ""); s["active"] != "llm_a" {
		t.Errorf("active = %v, want llm_a", s["active"])
	}
}

// 判定の順: 名前（400）→ 予約名以外（403）→ 映っている（409）→ 本文（400 / 413 / 422）。
func TestPutShader_LLMSlots_CheckOrder(t *testing.T) {
	h := newHarnessLLM(t, true, llmPair, "a", "llm_a", "llm_b")
	h.inspect(func(e *Engine) { _ = e.Switch("llm_a") })

	bad := "{not json"
	noPixels := jsonBody(map[string]string{"source": "package main\n"})
	big := `{"source":"` + strings.Repeat("x", maxBodyBytes+1) + `"}`

	cases := []struct {
		label, name, body string
		status            int
		code              string
	}{
		{"名前の規則違反は 403 より先", "Bad", bad, 400, "invalid_name"},
		{"予約名以外は本文より先に 403", "x", bad, 403, "forbidden_name"},
		{"予約名以外は大きすぎる本文より先に 403", "x", big, 403, "forbidden_name"},
		{"映っている予約名は本文より先に 409", "llm_a", bad, 409, "slot_live"},
		{"映っている予約名は 422 より先に 409", "llm_a", noPixels, 409, "slot_live"},
		{"映っている予約名は 413 より先に 409", "llm_a", big, 409, "slot_live"},
		{"映っていない予約名は本文を見る（400）", "llm_b", bad, 400, "invalid_request"},
		{"映っていない予約名は本文を見る（422）", "llm_b", noPixels, 422, "unit_pixels_required"},
		{"映っていない予約名は本文を見る（413）", "llm_b", big, 413, "too_large"},
	}
	for _, c := range cases {
		st, body := h.do("PUT", "/v1/shaders/"+c.name, c.body)
		if st != c.status || body["error"] != c.code {
			t.Errorf("%s: status=%d body=%v, want %d %s", c.label, st, body, c.status, c.code)
		}
	}
}

// 403 はゲームループの状態を読まないので、ループが止まっていても返せる。
// 409 の判定はループで行うので、止まっていれば loop_timeout。
func TestPutShader_LLMSlots_LoopStalled(t *testing.T) {
	h := newHarnessLLM(t, false, llmPair, "a")

	st, body := h.do("PUT", "/v1/shaders/x", putBody("x"))
	assertError(t, st, body, 403, "forbidden_name")

	st, body = h.do("PUT", "/v1/shaders/llm_a", putBody("llm_a"))
	assertError(t, st, body, 503, "loop_timeout")
	if _, err := os.Stat(filepath.Join(h.dir, "llm_a.kage")); !os.IsNotExist(err) {
		t.Errorf("判定できないまま llm_a.kage が書かれた: %v", err)
	}
}

// フラグ有りでも switch / crossfade / GET は制限しない。
func TestLLMSlots_OtherEndpointsUnrestricted(t *testing.T) {
	h := newHarnessLLM(t, true, llmPair, "a", "b", "llm_a")

	if st, body := h.do("POST", "/v1/switch", `{"name":"b"}`); st != 200 {
		t.Errorf("switch b: status=%d body=%v", st, body)
	}
	if st, body := h.do("POST", "/v1/crossfade", `{"name":"a"}`); st != 200 {
		t.Errorf("crossfade a: status=%d body=%v", st, body)
	}
	if st, body := h.do("POST", "/v1/switch", `{"name":"llm_a"}`); st != 200 {
		t.Errorf("switch llm_a: status=%d body=%v", st, body)
	}
	if st, body := h.do("GET", "/v1/shaders/a", ""); st != 200 {
		t.Errorf("GET a: status=%d body=%v", st, body)
	}
}

// フラグ無しなら、映っている名前にも予約名以外にも今までどおり書ける。
func TestPutShader_NoLLMSlots_Unrestricted(t *testing.T) {
	h := newHarness(t, true, "a", "llm_a")
	h.inspect(func(e *Engine) { _ = e.Switch("llm_a") })

	for _, n := range []string{"a", "llm_a", "x"} {
		if st, body := h.do("PUT", "/v1/shaders/"+n, putBody(n)); st != 200 {
			t.Errorf("PUT %s: status=%d body=%v", n, st, body)
		}
	}
}
