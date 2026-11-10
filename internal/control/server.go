package control

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cyokozai/kagelife/internal/shadermgr"
	"github.com/cyokozai/kagelife/internal/tempo"
)

const (
	// DefaultTimeout はゲームループの返信を待つ上限。
	DefaultTimeout = 2 * time.Second
	// MaxSourceBytes は PUT で受け付けるシェーダソースの上限（64KiB）。
	MaxSourceBytes = 64 * 1024
	// maxBodyBytes は JSON 本文全体の上限。ソースのエスケープで膨らむ分を見込む。
	maxBodyBytes = 1 << 20
)

// ServerConfig は NewServer の設定。
type ServerConfig struct {
	Token     string
	ShaderDir string // 絶対パス
	Compile   shadermgr.ShaderCompiler
	Queue     *Queue
	Timeout   time.Duration // 0 なら DefaultTimeout
	// LLMSlots は -llm-slots の予約名（nil なら無効）。空でなければ PUT はこの名前だけに書け、
	// 映っている予約名には書けない。Engine.LLMSlots と同じものを渡す。
	LLMSlots []string
	// Previews は GET /v1/previews の状態。ゲームループ（描く側）と同じものを渡す。nil なら新しく作る（描く側が無いので jpeg は常に null）。
	Previews *Previews
}

// Server は制御口 v1 の http.Handler。
type Server struct {
	cfg     ServerConfig
	mux     *http.ServeMux
	shaders http.HandlerFunc // /v1/shaders/{name}（ServeMux のパス正規化を通さない）
	put     sync.Mutex       // PUT の「書き込み → 反映」の順序を保つ
}

// NewServer は制御口 v1 のハンドラを作る。
func NewServer(cfg ServerConfig) *Server {
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.Previews == nil {
		cfg.Previews = NewPreviews()
	}
	s := &Server{cfg: cfg, mux: http.NewServeMux()}

	s.mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no such endpoint")
	})
	s.route("/v1/state", map[string]http.HandlerFunc{http.MethodGet: s.getState})
	s.shaders = s.methods(map[string]http.HandlerFunc{
		http.MethodGet: s.getShader,
		http.MethodPut: s.putShader,
	})
	s.route("/v1/switch", map[string]http.HandlerFunc{http.MethodPost: s.postSwitch})
	s.route("/v1/crossfade", map[string]http.HandlerFunc{http.MethodPost: s.postCrossfade})
	s.route("/v1/bpm", map[string]http.HandlerFunc{http.MethodPost: s.postBPM})
	s.route("/v1/keys", map[string]http.HandlerFunc{http.MethodPost: s.postKeys})
	s.route("/v1/previews", map[string]http.HandlerFunc{http.MethodGet: s.getPreviews})

	return s
}

// route はパスごとにメソッドを振り分け、該当しなければ JSON の 405 を返す。
// （ServeMux のメソッド付きパターンは 405 を平文で返すため使わない）
func (s *Server) route(pattern string, handlers map[string]http.HandlerFunc) {
	s.mux.HandleFunc(pattern, s.methods(handlers))
}

// methods はメソッドで振り分けるハンドラを作る。
func (s *Server) methods(handlers map[string]http.HandlerFunc) http.HandlerFunc {
	var allow string
	for _, m := range []string{http.MethodGet, http.MethodPut, http.MethodPost} {
		if _, ok := handlers[m]; ok {
			if allow != "" {
				allow += ", "
			}
			allow += m
		}
	}
	return func(w http.ResponseWriter, r *http.Request) {
		h, ok := handlers[r.Method]
		if !ok {
			w.Header().Set("Allow", allow)
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", r.Method+" is not allowed; use "+allow)

			return
		}
		h(w, r)
	}
}

const shadersPrefix = "/v1/shaders/"

// ServeHTTP は Bearer 認証を通したリクエストだけをルーティングする。
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	want := "Bearer " + s.cfg.Token
	got := r.Header.Get("Authorization")
	if s.cfg.Token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")

		return
	}
	// /v1/shaders/ 以下は自前で切り出す。ServeMux に通すと "a/b" は 404、".." は
	// パス正規化のリダイレクトになり、invalid_name を返せないため
	if strings.HasPrefix(r.URL.EscapedPath(), shadersPrefix) {
		s.shaders(w, r)

		return
	}
	s.mux.ServeHTTP(w, r)
}

// ── 応答 ────────────────────────────────────────────────────────

type errorBody struct {
	Error       string       `json:"error"`
	Message     string       `json:"message"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Error: code, Message: message})
}

func writeLoopError(w http.ResponseWriter) {
	writeError(w, http.StatusServiceUnavailable, "loop_timeout", "the game loop did not respond in time; is the window stalled or minimized?")
}

// decodeBody は本文を v に読み込む。失敗時は応答を書いて false を返す。
// 未知のフィールドは打ち間違いを黙って無視しないよう invalid_request で拒否する。
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "request body is too large")

			return false
		}
		writeError(w, http.StatusBadRequest, "invalid_request", "request body must be a JSON object: "+err.Error())

		return false
	}

	return true
}

// shaderName は /v1/shaders/ の後ろ（エスケープ済みのまま）をデコードして返す。
// デコードできなければ "" を返す（名前検査で invalid_name になる）。
func shaderName(r *http.Request) string {
	name, err := url.PathUnescape(strings.TrimPrefix(r.URL.EscapedPath(), shadersPrefix))
	if err != nil {
		return ""
	}

	return name
}

func writeMissing(w http.ResponseWriter, field string) {
	writeError(w, http.StatusBadRequest, "invalid_request", "missing required field: "+field)
}

func checkName(w http.ResponseWriter, name string) bool {
	if ValidName(name) {
		return true
	}
	writeError(w, http.StatusBadRequest, "invalid_name",
		"shader name must match ^[a-z0-9][a-z0-9_-]{0,63}$ (without the .kage extension)")

	return false
}

// ── ハンドラ ────────────────────────────────────────────────────

func (s *Server) getState(w http.ResponseWriter, _ *http.Request) {
	st, err := call(s.cfg.Queue, s.cfg.Timeout, (*Engine).State)
	if err != nil {
		writeLoopError(w)

		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) shaderPath(name string) string {
	return filepath.Join(s.cfg.ShaderDir, name+ShaderExt)
}

func (s *Server) getShader(w http.ResponseWriter, r *http.Request) {
	name := shaderName(r)
	if !checkName(w, name) {
		return
	}
	b, err := os.ReadFile(s.shaderPath(name))
	if errors.Is(err, fs.ErrNotExist) {
		writeError(w, http.StatusNotFound, "not_found", "shader "+name+" does not exist")

		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to read shader: "+err.Error())

		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": name, "source": string(b)})
}

func (s *Server) putShader(w http.ResponseWriter, r *http.Request) {
	name := shaderName(r)
	if !checkName(w, name) {
		return
	}
	// -llm-slots の制限は本文を読む前に判定する（名前 → 403 → 409 → 本文の順）
	if !s.checkLLMSlot(w, name) {
		return
	}
	var req struct {
		Source *string `json:"source"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Source == nil {
		writeMissing(w, "source")

		return
	}
	source := *req.Source
	if len(source) > MaxSourceBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "shader source must be at most 64KiB")

		return
	}
	if !HasUnitPixels(source) {
		writeError(w, http.StatusUnprocessableEntity, "unit_pixels_required",
			"shader source must contain a line `//kage:unit pixels`")

		return
	}

	src := []byte(source)
	sh, err := s.cfg.Compile(src)
	if err != nil {
		s.writeCompileError(w, name, src, err)

		return
	}

	// 予約 uniform の型違いはコンパイルが通り、描いたときに panic する。書く前にゲームループで試し描きする。
	// ループが止まっていて検査できなければ飛ばす（Install の処理でもう一度確かめるので、落ちるシェーダは入らない）
	verr, qerr := call(s.cfg.Queue, s.cfg.Timeout, func(e *Engine) error { return e.validate(sh) })
	if qerr == nil && verr != nil {
		sh.Dispose()
		s.writeCompileError(w, name, src, verr)

		return
	}

	s.put.Lock()
	defer s.put.Unlock()

	// 本文の読み込みとコンパイルの間に映った場合に備え、書く直前にもう一度確かめる
	if !s.checkLLMSlot(w, name) {
		sh.Dispose()

		return
	}

	created, err := writeFileAtomic(s.shaderPath(name), src)
	if err != nil {
		sh.Dispose()
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to write shader: "+err.Error())

		return
	}

	installErr, err := call(s.cfg.Queue, s.cfg.Timeout, func(e *Engine) error {
		_, err := e.InstallValidated(name, sh)

		return err
	})
	if errors.Is(err, errNotQueued) {
		sh.Dispose() // ファイルは書けているので、反映はファイル監視に委ねる
	}
	if err != nil {
		writeLoopError(w)

		return
	}
	if installErr != nil {
		// 書く前の検査を飛ばした（ループが遅れた）ときだけ来る。ファイルは書いてあるが Manager には入れていない
		diags := Diagnose(src, installErr)
		writeJSON(w, http.StatusUnprocessableEntity, errorBody{Error: "compile_error", Message: Summarize(diags), Diagnostics: diags})

		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "created": created})
}

// writeCompileError は PUT のコンパイル失敗（予約 uniform の型違いを含む）を 422 で返し、last_error に記録する。
func (s *Server) writeCompileError(w http.ResponseWriter, name string, src []byte, err error) {
	diags := Diagnose(src, err)
	msg := Summarize(diags)
	s.cfg.Queue.Post(func(e *Engine) { e.RecordError(name, msg) })
	writeJSON(w, http.StatusUnprocessableEntity, errorBody{Error: "compile_error", Message: msg, Diagnostics: diags})
}

// checkLLMSlot は -llm-slots が有効なとき、name に PUT してよいかを判定する。
// 予約名以外なら 403 forbidden_name（ゲームループは読まない）、
// 映っている予約名なら 409 slot_live（ゲームループ上で判定する）を書いて false を返す。
// 無効なら常に true。
func (s *Server) checkLLMSlot(w http.ResponseWriter, name string) bool {
	slots := s.cfg.LLMSlots
	if len(slots) == 0 {
		return true
	}
	if !slices.Contains(slots, name) {
		writeError(w, http.StatusForbidden, "forbidden_name", forbiddenNameMessage(slots))

		return false
	}
	live, err := call(s.cfg.Queue, s.cfg.Timeout, func(e *Engine) bool { return e.Live(name) })
	if err != nil {
		writeLoopError(w)

		return false
	}
	if live {
		writeError(w, http.StatusConflict, "slot_live", slotLiveMessage(name, slots))

		return false
	}

	return true
}

// writeFileAtomic は同じディレクトリの一時ファイルに書いてから rename する。
// 一時ファイル名は .kage で終わらないのでファイル監視に拾われない。
// 置き換え前にファイルが無かったら true を返す。
func writeFileAtomic(path string, data []byte) (created bool, err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return false, err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()

	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()

		return false, err
	}
	if err = tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()

		return false, err
	}
	if err = tmp.Close(); err != nil {
		return false, err
	}

	_, statErr := os.Stat(path)
	created = errors.Is(statErr, fs.ErrNotExist)
	if err = os.Rename(tmp.Name(), path); err != nil {
		return false, err
	}

	return created, nil
}

// notLoadedMessage は switch・crossfade の not_found の説明。
func notLoadedMessage(name string) string {
	return "shader " + name + " is not loaded (missing, or its last compile failed; see last_error in GET /v1/state)"
}

func (s *Server) postSwitch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name *string `json:"name"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Name == nil {
		writeMissing(w, "name")

		return
	}
	name := *req.Name
	if !checkName(w, name) {
		return
	}
	switchErr, qerr := call(s.cfg.Queue, s.cfg.Timeout, func(e *Engine) error { return e.Switch(name) })
	switch {
	case qerr != nil:
		writeLoopError(w)
	case errors.Is(switchErr, ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", notLoadedMessage(name))
	default:
		writeJSON(w, http.StatusOK, map[string]string{"active": name})
	}
}

func (s *Server) postCrossfade(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  *string  `json:"name"`
		Beats *float64 `json:"beats"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Name == nil {
		writeMissing(w, "name")

		return
	}
	name := *req.Name
	if !checkName(w, name) {
		return
	}
	beats := 0.0
	if req.Beats != nil {
		beats = *req.Beats
		if beats <= 0 || beats > 64 {
			writeError(w, http.StatusBadRequest, "invalid_beats", "beats must satisfy 0 < beats <= 64")

			return
		}
	}

	type result struct {
		beats float64
		err   error
	}
	res, qerr := call(s.cfg.Queue, s.cfg.Timeout, func(e *Engine) result {
		b, err := e.Crossfade(name, beats)

		return result{b, err}
	})
	switch {
	case qerr != nil:
		writeLoopError(w)
	case errors.Is(res.err, ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", notLoadedMessage(name))
	default:
		writeJSON(w, http.StatusOK, map[string]any{"target": name, "beats": res.beats})
	}
}

func (s *Server) postBPM(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BPM *float64 `json:"bpm"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.BPM == nil {
		writeMissing(w, "bpm")

		return
	}
	bpm := *req.BPM
	if bpm < tempo.MinBPM || bpm > tempo.MaxBPM {
		writeError(w, http.StatusBadRequest, "invalid_bpm",
			fmt.Sprintf("bpm must be between %g and %g", tempo.MinBPM, tempo.MaxBPM))

		return
	}
	_, err := call(s.cfg.Queue, s.cfg.Timeout, func(e *Engine) struct{} {
		e.SetBPM(bpm)

		return struct{}{}
	})
	if err != nil {
		writeLoopError(w)

		return
	}
	writeJSON(w, http.StatusOK, map[string]float64{"bpm": bpm})
}
