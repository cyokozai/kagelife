package shadermgr_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cyokozai/kagelife/internal/shadermgr"
)

// writeFile は dir/name に src を書き、そのパスを返す。
func writeFile(t *testing.T, dir, name, src string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad_Success(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "a.kage", "hello")
	m := shadermgr.New(srcCompiler)

	if err := m.Load(p); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if m.Len() != 1 || shaderName(m.Active()) != "hello" {
		t.Errorf("Len()=%d Active()=%q, want 1 hello", m.Len(), shaderName(m.Active()))
	}
}

func TestLoad_MissingFile(t *testing.T) {
	m := shadermgr.New(srcCompiler)
	p := filepath.Join(t.TempDir(), "none.kage")

	err := m.Load(p)

	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("errors.Is(err, os.ErrNotExist) = false: %v", err)
	}
	if n := strings.Count(err.Error(), p); n != 1 {
		t.Errorf("path appears %d times in %q, want 1", n, err.Error())
	}
	if m.Len() != 0 {
		t.Errorf("Len() = %d, want 0", m.Len())
	}
}

// コンパイルに失敗したファイルも nil のスロットとして枠を持つ。
func TestLoad_CompileErrorReservesNilSlot(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a.kage", "ERR 3:5: undefined: foo")
	b := writeFile(t, dir, "b.kage", "b")
	m := shadermgr.New(srcCompiler)

	if err := m.Load(a); err == nil {
		t.Fatal("Load(a) error = nil, want error")
	}
	if err := m.Load(b); err != nil {
		t.Fatalf("Load(b) error = %v", err)
	}

	if m.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", m.Len())
	}
	if got := m.Names(); !slices.Equal(got, []string{a, b}) {
		t.Errorf("Names() = %v, want [%s %s]", got, a, b)
	}
}

func TestCompileErrorFormat(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{name: "行:列: の形", src: "ERR 3:5: undefined: foo", want: "shaders/a.kage:3:5: undefined: foo"},
		{name: "形が合わない", src: "ERR something broke", want: "shaders/a.kage: something broke"},
		{
			name: "複数行",
			src:  "ERR 1:2: first\n4:1: second",
			want: "shaders/a.kage:1:2: first\nshaders/a.kage:4:1: second",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := shadermgr.New(srcCompiler)
			err := m.Reload("shaders/a.kage", []byte(tt.src))
			if err == nil {
				t.Fatal("Reload() error = nil")
			}
			if err.Error() != tt.want {
				t.Errorf("error = %q, want %q", err.Error(), tt.want)
			}
		})
	}
}

func TestCompileError_Unwrap(t *testing.T) {
	sentinel := errors.New("boom")
	m := shadermgr.New(func([]byte) (shadermgr.Shader, error) { return nil, sentinel })

	err := m.Reload("s/a.kage", nil)

	if !errors.Is(err, sentinel) {
		t.Errorf("errors.Is(err, sentinel) = false: %v", err)
	}
	var ce *shadermgr.CompileError
	if !errors.As(err, &ce) || ce.Path != "s/a.kage" {
		t.Errorf("errors.As CompileError failed or wrong path: %v", err)
	}
}

// ── スロットの固定と挿入 ─────────────────────────────────────────

func TestSlots_SortedByFileName(t *testing.T) {
	m := newManagerWith(t, "s/c.kage", "s/a.kage", "s/b.kage")
	want := []string{"s/a.kage", "s/b.kage", "s/c.kage"}
	if got := m.Names(); !slices.Equal(got, want) {
		t.Errorf("Names() = %v, want %v", got, want)
	}
}

func TestSlots_PathIsCleaned(t *testing.T) {
	m := newManagerWith(t, "s/a.kage")
	mustReload(t, m, "s/./x/../a.kage", []byte("v2"))

	if m.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", m.Len())
	}
	if got := shaderName(m.Active()); got != "v2" {
		t.Errorf("Active() = %q, want v2", got)
	}
}

// 起動時に失敗したファイルを後で直すと、同じスロットに入る（末尾に追加されない）。
func TestSlots_FixingFailedFileKeepsPosition(t *testing.T) {
	m := shadermgr.New(srcCompiler)
	mustReload(t, m, "s/a.kage", []byte("a"))
	if err := m.Reload("s/b.kage", []byte("ERR 1:1: bad")); err == nil {
		t.Fatal("want error")
	}
	mustReload(t, m, "s/c.kage", []byte("c"))

	mustReload(t, m, "s/b.kage", []byte("b"))

	if got := m.Names(); !slices.Equal(got, []string{"s/a.kage", "s/b.kage", "s/c.kage"}) {
		t.Errorf("Names() = %v", got)
	}
	m.Switch(1)
	if got := shaderName(m.Active()); got != "b" {
		t.Errorf("Active() after Switch(1) = %q, want b", got)
	}
}

// 失敗した再読み込みは既存のシェーダーを残す（nil にしない）。
func TestSlots_FailedReloadKeepsOldShader(t *testing.T) {
	m := newManagerWith(t, "s/a.kage")
	if err := m.Reload("s/a.kage", []byte("ERR x")); err == nil {
		t.Fatal("want error")
	}
	if got := shaderName(m.Active()); got != "s/a.kage" {
		t.Errorf("Active() = %q, want s/a.kage", got)
	}
}

// 挿入でインデックスがずれても、A と B は同じシェーダーを指し続ける。
func TestSlots_InsertKeepsActiveAAndB(t *testing.T) {
	m := newManagerWith(t, "s/b.kage", "s/d.kage")
	m.Switch(0) // A = b
	m.BeginFade(1)
	m.TickAt(at(0), bpm60)
	m.TickAt(at(1), bpm60)

	mustReload(t, m, "s/a.kage", []byte("s/a.kage")) // 先頭に挿入
	mustReload(t, m, "s/c.kage", []byte("s/c.kage")) // b と d の間に挿入

	if got := shaderName(m.Active()); got != "s/b.kage" {
		t.Errorf("Active() = %q, want s/b.kage", got)
	}
	if m.ActiveIndex() != 1 {
		t.Errorf("ActiveIndex() = %d, want 1", m.ActiveIndex())
	}
	if got := shaderName(m.ActiveB()); got != "s/d.kage" {
		t.Errorf("ActiveB() = %q, want s/d.kage", got)
	}
	if !approx(m.MixRatio(), 0.25) {
		t.Errorf("MixRatio() = %v, want 0.25", m.MixRatio())
	}

	m.TickAt(at(4), bpm60)
	if got := shaderName(m.Active()); got != "s/d.kage" || m.ActiveIndex() != 3 {
		t.Errorf("after completion Active()=%q idx=%d, want s/d.kage 3", got, m.ActiveIndex())
	}
}

// 空の Manager への最初の挿入は A になる。
func TestSlots_FirstInsertBecomesA(t *testing.T) {
	m := newManagerWith(t, "s/b.kage")
	if m.ActiveIndex() != 0 || shaderName(m.Active()) != "s/b.kage" {
		t.Errorf("idx=%d Active=%q", m.ActiveIndex(), shaderName(m.Active()))
	}
}

// ── nil スロット ──────────────────────────────────────────────────

func nilSlotManager(t *testing.T) *shadermgr.Manager {
	t.Helper()
	m := newManagerWith(t, "s/a.kage", "s/c.kage")
	if err := m.Reload("s/b.kage", []byte("ERR 1:1: bad")); err == nil {
		t.Fatal("want error")
	}
	return m // [a, b(nil), c]
}

func TestNilSlot_SwitchIgnored(t *testing.T) {
	m := nilSlotManager(t)
	m.Switch(1)
	if m.ActiveIndex() != 0 {
		t.Errorf("ActiveIndex() = %d, want 0", m.ActiveIndex())
	}
}

func TestNilSlot_BeginFadeIgnored(t *testing.T) {
	m := nilSlotManager(t)
	m.BeginFade(1)
	if m.Fading() {
		t.Error("Fading() = true, want false")
	}
}

func TestNilSlot_ActiveMayBeNil(t *testing.T) {
	m := shadermgr.New(srcCompiler)
	if err := m.Reload("s/a.kage", []byte("ERR bad")); err == nil {
		t.Fatal("want error")
	}
	if m.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", m.Len())
	}
	if m.Active() != nil {
		t.Error("Active() should be nil for nil slot")
	}
}

// ── Remove ───────────────────────────────────────────────────────

func TestRemove_DisposesAndDeletes(t *testing.T) {
	var captured []*fakeShader
	m := shadermgr.New(capturingCompiler(&captured))
	mustReload(t, m, "s/a.kage", nil)
	mustReload(t, m, "s/b.kage", nil)

	m.Remove("s/./a.kage")

	if !captured[0].disposed {
		t.Error("removed shader should be Disposed")
	}
	if got := m.Names(); !slices.Equal(got, []string{"s/b.kage"}) {
		t.Errorf("Names() = %v", got)
	}
}

func TestRemove_UnknownPathIsNoop(t *testing.T) {
	m := newManagerWith(t, "s/a.kage")
	m.Remove("s/zzz.kage")
	if m.Len() != 1 {
		t.Errorf("Len() = %d, want 1", m.Len())
	}
}

func TestRemove_NilSlot(t *testing.T) {
	m := nilSlotManager(t)
	m.Remove("s/b.kage")
	if got := m.Names(); !slices.Equal(got, []string{"s/a.kage", "s/c.kage"}) {
		t.Errorf("Names() = %v", got)
	}
}

func TestRemove_BeforeActiveShiftsIndex(t *testing.T) {
	m := newManagerWith(t, "s/a.kage", "s/b.kage", "s/c.kage")
	m.Switch(2)
	m.Remove("s/a.kage")
	if m.ActiveIndex() != 1 || shaderName(m.Active()) != "s/c.kage" {
		t.Errorf("idx=%d Active=%q, want 1 s/c.kage", m.ActiveIndex(), shaderName(m.Active()))
	}
}

func TestRemove_ActiveA(t *testing.T) {
	tests := []struct {
		name     string
		active   int
		remove   string
		wantIdx  int
		wantName string
	}{
		{name: "中間を消すと次に寄る", active: 1, remove: "s/b.kage", wantIdx: 1, wantName: "s/c.kage"},
		{name: "末尾を消すと前に寄る", active: 2, remove: "s/c.kage", wantIdx: 1, wantName: "s/b.kage"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newManagerWith(t, "s/a.kage", "s/b.kage", "s/c.kage")
			m.Switch(tt.active)
			m.Remove(tt.remove)
			if m.ActiveIndex() != tt.wantIdx || shaderName(m.Active()) != tt.wantName {
				t.Errorf("idx=%d Active=%q, want %d %s", m.ActiveIndex(), shaderName(m.Active()), tt.wantIdx, tt.wantName)
			}
		})
	}
}

func TestRemove_LastSlot(t *testing.T) {
	m := newManagerWith(t, "s/a.kage")
	m.Remove("s/a.kage")
	if m.Len() != 0 || m.Active() != nil || m.ActiveIndex() != 0 {
		t.Errorf("Len=%d Active=%v idx=%d", m.Len(), m.Active(), m.ActiveIndex())
	}
	// 空になった後の追加で A が有効になる
	mustReload(t, m, "s/b.kage", []byte("s/b.kage"))
	if shaderName(m.Active()) != "s/b.kage" {
		t.Errorf("Active() = %q, want s/b.kage", shaderName(m.Active()))
	}
}

func TestRemove_ActiveBCancelsFade(t *testing.T) {
	m := newManagerWith(t, "s/a.kage", "s/b.kage")
	m.BeginFade(1)
	m.TickAt(at(0), bpm60)
	m.TickAt(at(1), bpm60)

	m.Remove("s/b.kage")

	if m.Fading() || m.ActiveB() != nil || m.MixRatio() != 0 {
		t.Errorf("fading=%v B=%v mix=%v", m.Fading(), m.ActiveB(), m.MixRatio())
	}
	if shaderName(m.Active()) != "s/a.kage" {
		t.Errorf("Active() = %q, want s/a.kage", shaderName(m.Active()))
	}
}

// フェード中に A を消したら、B を A に確定してフェードを終える。
func TestRemove_ActiveADuringFadeCommitsB(t *testing.T) {
	m := newManagerWith(t, "s/a.kage", "s/b.kage", "s/c.kage")
	m.BeginFade(2)
	m.TickAt(at(0), bpm60)
	m.TickAt(at(1), bpm60)

	m.Remove("s/a.kage")

	if m.Fading() || m.ActiveB() != nil {
		t.Errorf("fading=%v B=%v, want false nil", m.Fading(), m.ActiveB())
	}
	if shaderName(m.Active()) != "s/c.kage" || m.ActiveIndex() != 1 {
		t.Errorf("Active=%q idx=%d, want s/c.kage 1", shaderName(m.Active()), m.ActiveIndex())
	}
}

// 前方のスロットを消しても、フェード中の B は同じシェーダーを指す。
func TestRemove_BeforeBShiftsB(t *testing.T) {
	m := newManagerWith(t, "s/a.kage", "s/b.kage", "s/c.kage")
	m.Switch(1)
	m.BeginFade(2)

	m.Remove("s/a.kage")

	if shaderName(m.ActiveB()) != "s/c.kage" || shaderName(m.Active()) != "s/b.kage" {
		t.Errorf("A=%q B=%q", shaderName(m.Active()), shaderName(m.ActiveB()))
	}
}

// ── A が nil のスロットを指しているときの寄せ ─────────────────────

// mustFail は失敗が前提の Reload を呼ぶ。
func mustFail(t *testing.T, m *shadermgr.Manager, path string) {
	t.Helper()
	if err := m.Reload(path, []byte("ERR bad")); err == nil {
		t.Fatalf("Reload(%q) error = nil, want error", path)
	}
}

// 起動時に先頭が失敗していても、後から成功して挿入されたスロットに A が寄る。
func TestNilA_NewSuccessfulInsertMovesA(t *testing.T) {
	m := shadermgr.New(srcCompiler)
	mustFail(t, m, "s/a.kage")
	mustReload(t, m, "s/b.kage", []byte("s/b.kage"))

	if m.ActiveIndex() != 1 || shaderName(m.Active()) != "s/b.kage" {
		t.Errorf("idx=%d Active=%q, want 1 s/b.kage", m.ActiveIndex(), shaderName(m.Active()))
	}
}

// 失敗していたスロットを再読み込みで直したときも A が寄る。
func TestNilA_FixedReloadMovesA(t *testing.T) {
	m := shadermgr.New(srcCompiler)
	mustFail(t, m, "s/b.kage")
	mustFail(t, m, "s/c.kage")
	mustReload(t, m, "s/c.kage", []byte("s/c.kage"))

	if m.ActiveIndex() != 1 || shaderName(m.Active()) != "s/c.kage" {
		t.Errorf("idx=%d Active=%q, want 1 s/c.kage", m.ActiveIndex(), shaderName(m.Active()))
	}
}

// A が nil でないときは、別のスロットが成功しても A は動かない。
func TestNonNilA_NotMovedByOtherSuccess(t *testing.T) {
	m := newManagerWith(t, "s/b.kage")
	mustReload(t, m, "s/a.kage", []byte("s/a.kage"))
	mustReload(t, m, "s/c.kage", []byte("s/c.kage"))
	mustReload(t, m, "s/c.kage", []byte("v2"))

	if shaderName(m.Active()) != "s/b.kage" {
		t.Errorf("Active() = %q, want s/b.kage", shaderName(m.Active()))
	}
}

// Remove の寄せ先が nil なら、nil でない最も近いスロットに寄せる。
func TestRemove_SkipsNilNeighbor(t *testing.T) {
	tests := []struct {
		name     string
		ok       []string // 成功させるパス
		bad      []string // 失敗させるパス
		active   int
		remove   string
		wantName string
	}{
		{
			// [a, b, c(nil), d] で b を消す → 同じ位置は c(nil)。a と d が等距離で、次側の d を優先
			name: "等距離なら次側", ok: []string{"s/a.kage", "s/b.kage", "s/d.kage"}, bad: []string{"s/c.kage"},
			active: 1, remove: "s/b.kage", wantName: "s/d.kage",
		},
		{
			// [a, b, c(nil), d(nil), e] で b を消す → a が距離 1、e が距離 2
			name: "近い前側", ok: []string{"s/a.kage", "s/b.kage", "s/e.kage"}, bad: []string{"s/c.kage", "s/d.kage"},
			active: 1, remove: "s/b.kage", wantName: "s/a.kage",
		},
		{
			// [a, b, c(nil)] で b を消す → 次側に nil しか無く、前側の a に寄る
			name: "前側に寄る", ok: []string{"s/a.kage", "s/b.kage"}, bad: []string{"s/c.kage"},
			active: 1, remove: "s/b.kage", wantName: "s/a.kage",
		},
		{
			// [a, b(nil)] で a を消す → nil しか残らないので nil のまま
			name: "nil しか残らない", ok: []string{"s/a.kage"}, bad: []string{"s/b.kage"},
			active: 0, remove: "s/a.kage", wantName: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newManagerWith(t, tt.ok...)
			for _, p := range tt.bad {
				mustFail(t, m, p)
			}
			m.Switch(tt.active)
			m.Remove(tt.remove)
			if got := shaderName(m.Active()); got != tt.wantName {
				t.Errorf("Active() = %q, want %q", got, tt.wantName)
			}
		})
	}
}
