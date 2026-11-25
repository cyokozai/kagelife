package control

import "sort"

// features は GET /v1/state の features に出す任意機能の名前（snake_case）。
// v1 の基本機能は含めない。任意機能（例: 拍への量子化）を足すときはここに名前を加える。
//
//   - llm_slots: -llm-slots の予約名（state の llm_slots、PUT の 403 forbidden_name / 409 slot_live）。
//     このビルドが機能を持つことだけを示す。設定済みかは state の llm_slots が空かで見る。
//   - keys: POST /v1/keys（GUI のキーボードの数字・[・] を押したのと同じ処理。Stream Deck 用）。
//   - previews: GET /v1/previews（数字キー 1〜9 のシェーダの小さな動くデモ。Stream Deck 用）。
//     これだけは -previews=false で止められ、そのときは state の features に出さない（Engine.features）。
var features = []string{"keys", "llm_slots", "previews"}

// Features は features をソートした複製を返す。空でも nil ではなく空のスライスを返す。
func Features() []string {
	out := make([]string, len(features))
	copy(out, features)
	sort.Strings(out)

	return out
}

// FeatureAudio は -audio で音が有効なときだけ features に出す名前（ADR-009、FR-185）。
// 音の uniform（AudioBass・AudioMid・AudioHigh・AudioOnset）が動いていることを示す。
const FeatureAudio = "audio"

// features は state に出す features。NoPreviews なら previews を除き、Audio なら audio を足す。
func (e *Engine) features() []string {
	all := Features()
	out := all[:0]
	for _, f := range all {
		if f != "previews" || !e.NoPreviews {
			out = append(out, f)
		}
	}
	if e.Audio {
		out = append(out, FeatureAudio)
		sort.Strings(out)
	}

	return out
}
