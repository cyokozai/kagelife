package control

import (
	"strings"
	"sync"
	"testing"
)

// keyPress は fakeKeys が受け取った 1 回の押下。
type keyPress struct {
	key   string
	shift bool
}

// fakeKeys は Engine.PressKey の代わり。assigned に無いキーは割り当て無しとして false を返す。
type fakeKeys struct {
	mu       sync.Mutex
	assigned map[string]bool
	got      []keyPress
}

func (f *fakeKeys) press(key string, shift bool) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.assigned[key] {
		return false
	}
	f.got = append(f.got, keyPress{key, shift})

	return true
}

func (f *fakeKeys) presses() []keyPress {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]keyPress(nil), f.got...)
}

func newKeysHarness(t *testing.T, loop bool, assigned ...string) (*harness, *fakeKeys) {
	t.Helper()
	h := newHarness(t, loop, "a")
	fk := &fakeKeys{assigned: map[string]bool{}}
	for _, k := range assigned {
		fk.assigned[k] = true
	}
	h.eng.PressKey = fk.press

	return h, fk
}

// 割り当てのあるキーはゲームループの上で押した扱いになり、200 {"key":...} を返す。
// shift は省略すると false。
func TestKeys_PressesAssignedKey(t *testing.T) {
	h, fk := newKeysHarness(t, true, "1", "9", "0", "[", "]")

	cases := []struct {
		body  string
		want  keyPress
		reply string
	}{
		{`{"key":"1"}`, keyPress{"1", false}, "1"},
		{`{"key":"9","shift":true}`, keyPress{"9", true}, "9"},
		{`{"key":"0","shift":false}`, keyPress{"0", false}, "0"},
		{`{"key":"["}`, keyPress{"[", false}, "["},
		{`{"key":"]","shift":true}`, keyPress{"]", true}, "]"},
	}
	for _, c := range cases {
		st, body := h.do("POST", "/v1/keys", c.body)
		if st != 200 || body["key"] != c.reply {
			t.Errorf("%s: status=%d body=%v, want 200 key=%s", c.body, st, body, c.reply)
		}
	}

	got := fk.presses()
	if len(got) != len(cases) {
		t.Fatalf("presses = %v, want %d", got, len(cases))
	}
	for i, c := range cases {
		if got[i] != c.want {
			t.Errorf("press %d = %v, want %v", i, got[i], c.want)
		}
	}
}

// 割り当てが無い数字は 404 not_found。
func TestKeys_UnassignedIsNotFound(t *testing.T) {
	h, fk := newKeysHarness(t, true, "1")

	for _, k := range []string{"2", "0"} {
		st, body := h.do("POST", "/v1/keys", `{"key":"`+k+`"}`)
		assertError(t, st, body, 404, "not_found")
	}
	if got := fk.presses(); len(got) != 0 {
		t.Errorf("presses = %v, want none", got)
	}
}

// PressKey が未設定（キー操作を持たない Engine）なら、どのキーも 404 not_found。
func TestKeys_NoHookIsNotFound(t *testing.T) {
	h := newHarness(t, true, "a")

	st, body := h.do("POST", "/v1/keys", `{"key":"1"}`)
	assertError(t, st, body, 404, "not_found")
}

// 0〜9・[・] 以外のキーは 400 invalid_key。ゲームループには渡さない。
func TestKeys_InvalidKey(t *testing.T) {
	h, fk := newKeysHarness(t, true, "1")

	for _, body := range []string{
		`{"key":""}`, `{"key":"10"}`, `{"key":"a"}`, `{"key":"H"}`, `{"key":" "}`,
		`{"key":"１"}`, `{"key":"-1"}`, `{"key":"{"}`,
	} {
		st, res := h.do("POST", "/v1/keys", body)
		assertError(t, st, res, 400, "invalid_key")
	}
	if got := fk.presses(); len(got) != 0 {
		t.Errorf("presses = %v, want none", got)
	}
}

// 本文の検査: key の欠落・不正な JSON・型違い・未知のフィールドは 400 invalid_request。
func TestKeys_InvalidRequest(t *testing.T) {
	h, fk := newKeysHarness(t, true, "1")

	cases := []struct{ body, msgContains string }{
		{`{}`, "key"},
		{`{"shift":true}`, "key"},
		{`not json`, ""},
		{`{"key":1}`, ""},
		{`{"key":"1","shift":"yes"}`, ""},
		{`{"key":"1","hold":true}`, "hold"},
	}
	for _, c := range cases {
		st, body := h.do("POST", "/v1/keys", c.body)
		assertError(t, st, body, 400, "invalid_request")
		if msg, _ := body["message"].(string); !strings.Contains(msg, c.msgContains) {
			t.Errorf("%s: message = %q, want it to contain %q", c.body, msg, c.msgContains)
		}
	}
	if got := fk.presses(); len(got) != 0 {
		t.Errorf("presses = %v, want none", got)
	}
}

// 認証が無ければ 401、GET は 405。
func TestKeys_AuthAndMethod(t *testing.T) {
	h, fk := newKeysHarness(t, true, "1")

	st, body := h.doAuth("POST", "/v1/keys", `{"key":"1"}`, "")
	assertError(t, st, body, 401, "unauthorized")
	st, body = h.do("GET", "/v1/keys", "")
	assertError(t, st, body, 405, "method_not_allowed")
	if got := fk.presses(); len(got) != 0 {
		t.Errorf("presses = %v, want none", got)
	}
}

// ゲームループが止まっていれば 503 loop_timeout。
func TestKeys_LoopTimeout(t *testing.T) {
	h, _ := newKeysHarness(t, false, "1")

	st, body := h.do("POST", "/v1/keys", `{"key":"1"}`)
	assertError(t, st, body, 503, "loop_timeout")
}
