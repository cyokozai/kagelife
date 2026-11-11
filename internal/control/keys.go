package control

import (
	"errors"
	"net/http"
)

// validKey は POST /v1/keys で受け付けるキー（数字 0〜9 と [ ]）かを返す。
func validKey(key string) bool {
	if len(key) != 1 {
		return false
	}
	c := key[0]

	return (c >= '0' && c <= '9') || c == '[' || c == ']'
}

// postKeys は GUI のキーボードでキーを押したのと同じ処理を、ゲームループの上で行う。
// 判定順は 本文 400 invalid_request → key 400 invalid_key → 割り当て無し 404 not_found。
func (s *Server) postKeys(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key   *string `json:"key"`
		Shift bool    `json:"shift"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Key == nil {
		writeMissing(w, "key")

		return
	}
	key := *req.Key
	if !validKey(key) {
		writeError(w, http.StatusBadRequest, "invalid_key", `key must be one of "0"-"9", "[", "]"`)

		return
	}
	keyErr, qerr := call(s.cfg.Queue, s.cfg.Timeout, func(e *Engine) error { return e.Key(key, req.Shift) })
	switch {
	case qerr != nil:
		writeLoopError(w)
	case errors.Is(keyErr, ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "key "+key+" is not assigned to anything (see the shader list in the GUI log)")
	default:
		writeJSON(w, http.StatusOK, map[string]string{"key": key})
	}
}
