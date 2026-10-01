# ADR-001: アーキテクチャパターン — フラットモノリス（2026-09-30 改訂: `main` + `internal/` 3 パッケージ）

**ステータス**: Accepted（2026-09-30 改訂）
**日付**: 2026-04-18（改訂: 2026-09-30）
**決定者**: cyokozai

---

## コンテキスト

KageLifeは1人が1週間で開発する個人VJツール。
サーバーもクラウドも不要で、単一バイナリとしてローカルで動作する。
Ebitengineのゲームループ（`Update/Draw/Layout`）が中心構造を規定する。

## 検討した選択肢

### 選択肢 A: フラットモノリス（5ファイル程度）
**概要**: `main.go` + `game.go` + `shader_manager.go` + `watcher.go` + `uniform.go`
**メリット**:
- 1週間で実装しきれる
- ファイル間の依存関係が明快
- テストが書きやすい
**デメリット**:
- V2で機能が増えると整理が必要になる可能性

### 選択肢 B: レイヤードアーキテクチャ（domain/infrastructure/ui分離）
**メリット**: 将来的な拡張性が高い
**デメリット**:
- 個人ツールには過剰設計
- 1週間では余計なオーバーヘッドになる

## 決定

**選択肢 A（フラットモノリス）を採用する。**

### 理由
1週間・1人・個人ツールという制約において、レイヤー分離は投資対効果が低い。
機能が増えた時点でV2でリファクタリングする方が現実的。

### 前提条件
- チームが1人のまま
- ファイル数が10を超えたらパッケージ分割を検討する

## 改訂（2026-09-30）: 実装の構成

実装は選択肢 A のフラットな 5 ファイル構成ではなく、`main` パッケージと `internal/` 配下の 3 パッケージに分かれている。
レイヤー分離（選択肢 B）は採っておらず、単一バイナリ・単一モジュールである点は当初の決定のまま。本 ADR はこの実態を正とする。

| パッケージ | ファイル | 責務 |
|-----------|---------|------|
| `main` | `main.go` | `Game`（`Update` / `Draw` / `Layout` / `LayoutF`）、キー入力、Uniform の組み立て、クロスフェードの描画、HUD |
| `internal/filewatcher` | `watcher.go` | fsnotify による `shaders/` の監視と 100ms のデバウンス（[ADR-002](ADR-002-hotreload-mechanism.md)） |
| `internal/shadermgr` | `manager.go` | シェーダーのロード・再ロード・切り替え・`Dispose()`、クロスフェードの状態（[ADR-008](ADR-008-crossfade-compositing.md)） |
| `internal/tempo` | `tapper.go` | タップテンポ（BPM と拍の位相） |

当初案との差分:

- `game.go` は独立させず、`Game` は `main.go` にある
- `uniform.go`（`UniformBuilder`）は作っておらず、Uniform は `Game.Draw()` の中で組み立てる
- `shadermgr` は `ebiten` に直接依存しない。コンパイル関数（`ShaderCompiler`）と `Dispose()` だけを持つ `Shader` インタフェースを外から受け取るため、GPU 無しでユニットテストできる
- 3 つの `internal/` パッケージにはそれぞれ `_test.go` がある

MCP 連携（[ADR-005](ADR-005-mcp-process-topology.md) / [ADR-006](ADR-006-control-api.md)）では、4 つ目のパッケージとして制御口の `internal/control`（ebiten 非依存）を足す予定である（feat/mcp-control-api、未実装）。
MCP の中継 `kagelife-mcp` は別リポジトリの別バイナリなので、kagelife 本体が単一バイナリ・単一モジュールである点は変わらない。
構成図は [architecture-decisions.md](../architecture-decisions.md) の「MCP 連携」の節を参照。

## 影響・結果

**ポジティブ**: 実装速度が最大化される。`internal/` の 3 パッケージは Ebitengine のゲームループから切り離してテストできる
**ネガティブ**: V2でシェーダーレイヤー合成を追加する際にリファクタリングが必要になる可能性（合成の布石は [ADR-008](ADR-008-crossfade-compositing.md) を参照）

**Action Items**:
- [x] `Game`に`ShaderManager`への依存を明示的に持たせる設計にする（`Game.sm *shadermgr.Manager`。当初案の `game.go` ではなく `main.go` に置いた）
