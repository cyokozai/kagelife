package shadermgr

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var FadePresets = []float32{0.5, 1, 2, 3, 4, 8, 16}

const DefaultFadeBeatsIdx = 4

type ShaderCompiler func(src []byte) (Shader, error)

type Shader interface{ Dispose() }

// slot はファイル 1 つ分の枠。コンパイルに失敗したファイルは shader が nil のまま枠だけを持つ。
type slot struct {
	path   string // filepath.Clean 済みのパス
	shader Shader
}

// Manager はシェーダーをファイル名順のスロットで管理し、A→B のクロスフェードを進める。
type Manager struct {
	slots        []slot         // パスの昇順に並ぶ
	activeAIdx   int            // 現在アクティブなインデックス A
	activeBIdx   int            // フェード先のインデックス B（フェード中でなければ -1）
	fadeBeatsIdx int            // FadePresets のインデックス
	mixRatio     float32        // B の混合率（0〜1）
	fading       bool           // フェード中か
	progress     float64        // フェードの積算進捗（0〜1）
	lastTick     time.Time      // 前回 TickAt の時刻
	hasLastTick  bool           // lastTick が有効か（フェード開始直後・凍結明けは false）
	fadeOverride float32        // BeginFadeBeats で指定した拍数（0 ならプリセットに従う）
	Compiler     ShaderCompiler // コンパイル関数
}

func New(compiler ShaderCompiler) *Manager {
	return &Manager{
		Compiler:     compiler,
		activeBIdx:   -1,
		fadeBeatsIdx: DefaultFadeBeatsIdx,
	}
}

// Load はファイルを読み込んでコンパイルし、スロットに登録する。
// コンパイルに失敗した場合もスロットは確保し（シェーダーは nil）、エラーを返す。
func (m *Manager) Load(path string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err // *fs.PathError はパスを含むので包まない
	}

	return m.Reload(path, src)
}

// Reload は src をコンパイルし、path のスロットを差し替える（無ければファイル名順の位置に挿入する）。
// 失敗した場合、既存のスロットは旧シェーダーのまま、新しいパスは nil のスロットとして登録する。
func (m *Manager) Reload(path string, src []byte) error {
	path = filepath.Clean(path)
	newShader, err := m.Compiler(src)

	i, _ := m.slotFor(path)
	if err != nil {
		return &CompileError{Path: path, Err: err}
	}
	m.put(i, newShader)

	return nil
}

// Install はコンパイル済みのシェーダーを path のスロットに登録する（制御口の PUT 用）。
// 既存のスロットなら旧シェーダーを Dispose して差し替え、無ければファイル名順の位置に挿入する。
// A の扱いは Reload の成功時と同じ（A が nil のスロットを指していたときだけ寄せる）。
// スロットを新しく作ったら true を返す。shader は nil にしないこと。
func (m *Manager) Install(path string, shader Shader) bool {
	i, created := m.slotFor(filepath.Clean(path))
	m.put(i, shader)

	return created
}

// IndexOf は path（Clean して比べる）のスロット位置を返す。無ければ -1。
// コンパイルに失敗した nil のスロットも位置を返す（使えるかは Usable で確かめる）。
func (m *Manager) IndexOf(path string) int {
	i, found := m.find(filepath.Clean(path))
	if !found {
		return -1
	}

	return i
}

// Usable は idx が範囲内かつシェーダーを持つ（Switch・BeginFade できる）スロットかを返す。
func (m *Manager) Usable(idx int) bool {
	return m.usable(idx)
}

// slotFor は path のスロット位置を返す。無ければ nil のスロットを挿入し、作ったことを true で返す。
func (m *Manager) slotFor(path string) (int, bool) {
	i, found := m.find(path)
	if !found {
		m.insert(i, slot{path: path})
	}

	return i, !found
}

// put はスロット i のシェーダーを差し替える。
func (m *Manager) put(i int, shader Shader) {
	if old := m.slots[i].shader; old != nil {
		old.Dispose() // 旧シェーダーを解放してから差し替え
	}
	m.slots[i].shader = shader

	// A が nil のスロット（起動時の失敗など）を指していれば、成功したスロットに寄せる。
	// A が nil でなければ動かさない（ライブコーディング中に勝手に切り替えない）。
	// フェード中は A が nil になり得ない（B は nil でなく、B が成功した時点で A が寄るため）。
	if m.slots[m.activeAIdx].shader == nil {
		m.activeAIdx = i
	}
}

// Remove は path のスロットを Dispose してから取り除く。
// アクティブなスロットを消した場合は、残っているスロットに寄せる。
func (m *Manager) Remove(path string) {
	i, found := m.find(filepath.Clean(path))
	if !found {
		return
	}
	if s := m.slots[i].shader; s != nil {
		s.Dispose()
	}
	m.slots = append(m.slots[:i], m.slots[i+1:]...)

	switch {
	case m.activeBIdx == i:
		m.endFade()
	case m.activeBIdx > i:
		m.activeBIdx--
	}

	switch {
	case m.activeAIdx == i && m.fading:
		m.activeAIdx = m.activeBIdx // フェード先を A として確定する
		m.endFade()
	case m.activeAIdx > i:
		m.activeAIdx--
	}
	m.activeAIdx = min(m.activeAIdx, max(len(m.slots)-1, 0))
	m.activeAIdx = m.nearestUsable(m.activeAIdx)
}

// nearestUsable は idx から最も近い、シェーダーを持つスロットの位置を返す。
// 等距離なら次側を優先する。該当が無ければ idx をそのまま返す。
func (m *Manager) nearestUsable(idx int) int {
	for d := range len(m.slots) {
		if m.usable(idx + d) {
			return idx + d
		}
		if m.usable(idx - d) {
			return idx - d
		}
	}

	return idx
}

// Switch は index を即座に A にする。範囲外や nil のスロットは無視する。
func (m *Manager) Switch(index int) {
	if !m.usable(index) {
		return
	}
	m.activeAIdx = index
	m.endFade()
}

func (m *Manager) Active() Shader {
	if len(m.slots) == 0 {
		return nil
	}

	return m.slots[m.activeAIdx].shader
}

func (m *Manager) ActiveIndex() int {
	return m.activeAIdx
}

func (m *Manager) Len() int {
	return len(m.slots)
}

// Names はスロット順（ファイル名順）のパス一覧を返す。
func (m *Manager) Names() []string {
	names := make([]string, len(m.slots))
	for i, s := range m.slots {
		names[i] = s.path
	}

	return names
}

func (m *Manager) ActiveB() Shader {
	if m.activeBIdx < 0 || m.activeBIdx >= len(m.slots) {
		return nil
	}

	return m.slots[m.activeBIdx].shader
}

func (m *Manager) Fading() bool {
	return m.fading
}

func (m *Manager) MixRatio() float32 {
	return m.mixRatio
}

// BeginFade は idx へのフェードを 0 から始める。
//
//   - 範囲外・nil のスロット、フェード中でないときの A と同じインデックスは無視する。
//   - フェード中に B と同じインデックスを指定したら無視する。
//   - フェード中に別のインデックスを指定したら、Mix ≥ 0.5 なら B を A として確定し、
//     Mix < 0.5 なら A のままにしたうえで、新しいフェードを始める。
//     その結果の A と idx が同じなら、フェードを取りやめる。
//
// 拍数はプリセット（FadeBeats）に従い、フェード中にプリセットを変えると進行中のフェードにも効く。
func (m *Manager) BeginFade(idx int) {
	m.beginFade(idx, 0)
}

// BeginFadeBeats は beats 拍で idx へのフェードを始める（制御口の crossfade の beats 指定）。
// プリセットは変えない。beats が 0 以下なら何もしない。
// 無視・取りやめの規則は BeginFade と同じで、無視したときは進行中のフェードの拍数も変えない。
func (m *Manager) BeginFadeBeats(idx int, beats float32) {
	if beats <= 0 {
		return
	}
	m.beginFade(idx, beats)
}

// beginFade は BeginFade の本体。override が 0 より大きければ、新しく始めるフェードの拍数にする。
func (m *Manager) beginFade(idx int, override float32) {
	if !m.usable(idx) {
		return
	}
	if m.fading {
		if idx == m.activeBIdx {
			return
		}
		if m.mixRatio >= 0.5 {
			m.activeAIdx = m.activeBIdx
		}
		if idx == m.activeAIdx {
			m.endFade()

			return
		}
	} else if idx == m.activeAIdx {
		return
	}

	m.activeBIdx = idx
	m.fading = true
	m.mixRatio = 0
	m.progress = 0
	m.hasLastTick = false
	m.fadeOverride = override
}

// FadeTargetIndex はフェード先 B のインデックスを返す。フェード中でなければ -1。
func (m *Manager) FadeTargetIndex() int {
	return m.activeBIdx
}

// CurrentFadeBeats は進行中（または次に BeginFade で始まる）フェードの拍数を返す。
// BeginFadeBeats で始めたフェードの最中ならその拍数、それ以外はプリセットの値。
func (m *Manager) CurrentFadeBeats() float32 {
	if m.fadeOverride > 0 {
		return m.fadeOverride
	}

	return m.FadeBeats()
}

// Tick は現在時刻でフェードを進める。
func (m *Manager) Tick(bpm float64) {
	m.TickAt(time.Now(), bpm)
}

// TickAt は now までの経過時間ぶんフェードを進める。
// 進捗は「前回からの経過 / (拍数 × 60 / bpm)」を積算するので、
// フェード中に拍数や BPM を変えても Mix は連続する。
// bpm が 0 以下のフレームでは進めず（凍結）、凍結明けの最初のフレームは基準時刻の記録だけを行う。
func (m *Manager) TickAt(now time.Time, bpm float64) {
	if !m.fading {
		return
	}
	if bpm <= 0 {
		m.hasLastTick = false

		return
	}
	if !m.hasLastTick {
		m.lastTick = now
		m.hasLastTick = true

		return
	}

	elapsed := max(now.Sub(m.lastTick).Seconds(), 0)
	m.lastTick = now

	fadeDuration := float64(m.CurrentFadeBeats()) * 60.0 / bpm
	m.progress += elapsed / fadeDuration
	if m.progress >= 1.0 {
		m.activeAIdx = m.activeBIdx
		m.endFade()

		return
	}
	m.mixRatio = float32(m.progress)
}

func (m *Manager) FadeBeats() float32 {
	return FadePresets[m.fadeBeatsIdx]
}

func (m *Manager) IncFadeBeats() {
	if m.fadeBeatsIdx < len(FadePresets)-1 {
		m.fadeBeatsIdx++
	}
}

func (m *Manager) DecFadeBeats() {
	if m.fadeBeatsIdx > 0 {
		m.fadeBeatsIdx--
	}
}

// endFade はフェードを終え、B と指定拍数を解除する（A はそのまま）。
func (m *Manager) endFade() {
	m.activeBIdx = -1
	m.fadeOverride = 0
	m.fading = false
	m.mixRatio = 0
	m.progress = 0
	m.hasLastTick = false
}

// usable は idx が範囲内かつシェーダーを持つスロットかを返す。
func (m *Manager) usable(idx int) bool {
	return idx >= 0 && idx < len(m.slots) && m.slots[idx].shader != nil
}

// find は path のスロット位置を返す。無ければ挿入すべき位置と false を返す。
func (m *Manager) find(path string) (int, bool) {
	i := sort.Search(len(m.slots), func(i int) bool { return m.slots[i].path >= path })

	return i, i < len(m.slots) && m.slots[i].path == path
}

// insert は位置 i にスロットを挿入し、A・B が同じスロットを指し続けるよう付け替える。
func (m *Manager) insert(i int, s slot) {
	wasEmpty := len(m.slots) == 0
	m.slots = append(m.slots, slot{})
	copy(m.slots[i+1:], m.slots[i:])
	m.slots[i] = s

	if !wasEmpty && m.activeAIdx >= i {
		m.activeAIdx++
	}
	if m.activeBIdx >= i {
		m.activeBIdx++
	}
}

// CompileError はシェーダーのコンパイルエラー。
// Error() は Kage の「行:列: メッセージ」を「path:行:列: メッセージ」に、
// 形が合わない行は「path: メッセージ」にして返す。
type CompileError struct {
	Path string
	Err  error
}

var lineColRe = regexp.MustCompile(`^\d+:\d+: `)

func (e *CompileError) Error() string {
	lines := strings.Split(e.Err.Error(), "\n")
	for i, l := range lines {
		if lineColRe.MatchString(l) {
			lines[i] = e.Path + ":" + l
		} else {
			lines[i] = e.Path + ": " + l
		}
	}

	return strings.Join(lines, "\n")
}

func (e *CompileError) Unwrap() error { return e.Err }

var _ error = (*CompileError)(nil)
