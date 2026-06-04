# Discovery Brief: KageLife

> arch-requirements への引き渡し資料
> 作成日: 2026-04-18

---

## 1. Problem Statement

**対象ユーザー**: Go開発者 兼 VJアーティスト（作者自身）

**ユーザーのジョブ**:
VJパフォーマンス中に、Kageシェーダーをリアルタイムに切り替え・プレビューしたい。
ファイルを保存したら即座に反映され、時間ベースのUniformが自動で流れることで、
コードを書きながら視覚的なフィードバックを得たい。

**現在の解決策の課題**:
- KodeLifeはGLSL専用でKage非対応
- Hydra/TidalCyclesはNode.js/Haskell依存で環境が重い
- Go/Ebitengineエコシステム内で完結するVJツールが存在しない

---

## 2. Solution Hypothesis

**価値提案**:
KageLifeは、Ebitengine/Kageシェーダーをファイル保存だけでリアルタイム反映し、
キーボードで瞬時に切り替えられる、Go開発者のためのVJライブコーディングツール。

**North Star Metric**: ファイル保存からシェーダー反映までのレイテンシ < 200ms

**成功の定義（1週間後）**:
- ホットリロードが動作する
- 複数シェーダーをキーで切り替えられる
- コンパイルエラーでクラッシュしない

---

## 3. Validated Assumptions

| 仮定 | 重要度 | 検証状況 | 証拠 |
|------|--------|---------|------|
| Kage特化VJツールは存在しない | High | ✅ 検証済 | OSS調査（2026-04-18） |
| 自分がユーザーなのでDesirabilityは問題ない | High | ✅ 検証済 | 作者自身がユーザー |
| `ebiten.NewShader()`の実行時再呼び出し可否 | High | ⚠️ 未検証 | **Day 1にPoC必須** |
| `fsnotify`でmacOSのファイル変更を< 100ms検知 | Mid | ⚠️ 未検証 | Day 2に計測 |
| 複数`*ebiten.Shader`をメモリ保持してキー切替 | Mid | ⚠️ 未検証 | Day 2-3に確認 |

---

## 4. MVP Scope

**含める（Must Have）**:
- `.kage`ファイルのホットリロード（`fsnotify` + 自動再コンパイル）
- `Time` / `Resolution` Uniformの毎フレーム自動注入
- 複数シェーダーをキー（1〜9）で切り替え
- コンパイルエラー時に前のシェーダーを維持（クラッシュしない）

**含めない（Won't Have）**:
- シェーダー重ね合わせ（V2: DrawRectShaderの複数パス設計が必要）
- GUIパラメータUI（V2: ImGui/tview統合コストが高い）
- Ableton Link / MIDI連携（V2: Go実装が少ない）
- 頂点シェーダー（Kageの仕様上フラグメントシェーダのみ）

---

## 5. Priority Backlog

| 機能 | 優先度 | 目安日 | 根拠 |
|------|--------|-------|------|
| `ebiten.NewShader`再呼び出しPoC | P0 | Day 1 | 最大技術リスクの早期排除 |
| fsnotify監視 + 自動再コンパイル | P0 | Day 2-3 | North Star Metricの中核 |
| Time/Resolution Uniform自動注入 | P0 | Day 1-2 | アニメーションの基盤 |
| 複数シェーダー + キー切り替え | P0 | Day 2-3 | VJツールとしての最低要件 |
| Dispose()によるメモリ管理 | P1 | Day 3 | メモリリークリスク対処 |
| シェーダー一覧のターミナル表示 | P1 | Day 3-4 | 使い勝手の向上 |
| タップBPM | P2 | Day 5 | 時間があれば |

---

## 6. Key Risks & Open Questions

- **`ebiten.NewShader()`のメモリリーク**: 繰り返し呼び出し時に古い`*ebiten.Shader`を`.Dispose()`で明示解放する必要がある可能性が高い → Day 1-3で必ず確認
- **ホットリロードのレイテンシ**: fsnotify検知 → コンパイル → 描画更新のパイプラインが200ms以内に収まるか
- **未解決**: カスタムUniformの定義方法（`.kage`ファイルのコメントから解析するか、別ファイルで管理するか）

---

## 7. Next Steps

- [ ] Day 1: `ebiten.NewShader()`再呼び出しPoCでDay 1に技術リスクを排除
- [ ] arch-requirements でシステムアーキテクチャ・パッケージ設計を行う
- [ ] `shaders/` ディレクトリ構成とUniform規約を決める

---

## 関連ドキュメント

- [Context](discovery/context.md)
- [Problem Space](discovery/problem-space.md)
- [Solution Space](discovery/solution-space.md)
- [Assumptions](discovery/assumptions.md)
- [Prioritization](discovery/prioritization.md)

---

## 参考リンク

- [Ebitengine Kageドキュメント](https://ebitengine.org/ja/documents/shader.html)
- [kage-desk（参考OSS）](https://github.com/tinne26/kage-desk)
- [KageLife リポジトリ](https://github.com/cyokozai/kagelife)
