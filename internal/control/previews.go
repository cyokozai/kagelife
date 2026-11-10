package control

// PreviewSlot はキー 1 つに割り当たっているスロット。
type PreviewSlot struct {
	Key          string // "1"〜"9"
	Name         string // シェーダ名（拡張子なし）
	CompileError bool   // コンパイルに失敗したスロット（shadermgr の nil）なら true
}
