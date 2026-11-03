package control

import "sort"

// features は GET /v1/state の features に出す任意機能の名前（snake_case）。
// v1 の基本機能は含めない。任意機能（例: 拍への量子化）を足すときはここに名前を加える。
//
//   - llm_slots: -llm-slots の予約名（state の llm_slots、PUT の 403 forbidden_name / 409 slot_live）。
//     このビルドが機能を持つことだけを示す。設定済みかは state の llm_slots が空かで見る。
var features = []string{"llm_slots"}

// Features は features をソートした複製を返す。空でも nil ではなく空のスライスを返す。
func Features() []string {
	out := make([]string, len(features))
	copy(out, features)
	sort.Strings(out)

	return out
}
