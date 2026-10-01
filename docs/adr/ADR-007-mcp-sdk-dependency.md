# ADR-007: MCP SDK の採用 — modelcontextprotocol/go-sdk v1.8.0（kagelife-mcp だけで使う）

**ステータス**: Accepted
**日付**: 2026-09-30
**決定者**: cyokozai
**関連**: ADR-005（プロセス構成）、assumptions-20260418 #8、assumptions-20260930 #8

---

## コンテキスト

MCP の中継は、別リポジトリ `cyokozai/kagelife-mcp` の別バイナリ `kagelife-mcp` が担う（ADR-005）。
`kagelife-mcp` は stdio で MCP の JSON-RPC を話すので、その実装手段を選ぶ。

2026-04-18 の合意（assumptions #8、PRD v0.1 の依存表）は「外部依存は Go 標準 + Ebitengine + fsnotify のみ」だった。
なお、現在の kagelife の `go.mod` には HUD のフォント用に `golang.org/x/image` が直接依存として既に入っている（v0.1 の依存表には載っていない）。
kagelife の `go.mod` の Go は 1.27.1 である。

## 検討した選択肢

### 選択肢 A: 公式 `github.com/modelcontextprotocol/go-sdk` v1.8.0
**概要**: MCP 公式の Go SDK。v1.8.0 は 2026-09-14 公開、要求は Go 1.25 以上。stdio と Streamable HTTP に対応し、ツールの結果として `ImageContent` を返せる。
**メリット**:
- PoC で動作を確認済み。3 ツールの stdio サーバを TDD で書き、11 テストが `-race` で PASS。initialize → tools/list → tools/call、PNG の ImageContent、stdin EOF での正常終了を確認
- 引数のスキーマ違反、接続拒否、タイムアウトが `isError` で LLM に返る（LLM が自分で直せる）
- 仕様の改訂への追従を SDK 側に任せられる
**デメリット**:
- 間接依存が増える（下記。ただし kagelife-mcp の中だけ）

### 選択肢 B: `github.com/mark3labs/mcp-go`
**概要**: コミュニティ製で、同等の機能を持つ。
**メリット**:
- 利用例が多い
**デメリット**:
- 公式 SDK と機能が同等なら、仕様との対応を公式が保証する側を選ぶ理由が勝る
- 本件の PoC では確かめていない

### 選択肢 C: 自前の JSON-RPC 実装
**概要**: 標準ライブラリだけで stdio の JSON-RPC と MCP のメッセージを実装する。
**メリット**:
- 外部依存を増やさない
**デメリット**:
- 初期化の手順、ツールの一覧とスキーマ、内容型（テキスト・画像）、エラーの形を自分で保守し、仕様の改訂に追従し続ける手間がかかる
- 1 人開発では、この保守の手間が MCP 機能そのものの開発時間を圧迫する

## 決定

**選択肢 A（`modelcontextprotocol/go-sdk` v1.8.0）を採用する。ただし使うのは kagelife-mcp リポジトリだけで、kagelife 本体の依存方針は変えない。**

kagelife 本体について行うのは、依存表の記載漏れ（`golang.org/x/image`）を直すことだけである。

### kagelife 本体の直接依存（変更なし。記載だけ訂正）

| 直接依存 | 役割 | 導入 |
|---------|------|------|
| `github.com/hajimehoshi/ebiten/v2` | ゲームループ・シェーダ API | v0.1 から |
| `github.com/fsnotify/fsnotify` | ファイル監視 | v0.1 から |
| `golang.org/x/image` | HUD のフォント | 既存（v0.1 の表に記載漏れ） |

### kagelife-mcp の直接依存

| 直接依存 | 役割 |
|---------|------|
| `github.com/modelcontextprotocol/go-sdk` v1.8.0 | MCP の stdio サーバ |

### 取り決め
- kagelife 本体は go-sdk を import しない。制御口（`internal/control`）は標準ライブラリの `net/http` と `encoding/json` だけで書く
- kagelife-mcp は ebiten を import しない（cgo 無しの純 Go を保つ）
- kagelife-mcp は kagelife のパッケージを import しない。両者の結び目は制御口の契約 v1（ADR-006）だけとする
- kagelife 本体にこれ以上の直接依存を足すときは、ADR を起こしてから足す

### 理由
- PoC で、本件に要る機能（stdio、ツール、ImageContent、`isError`、EOF 終了）がすべて動いた
- C の保守の手間は、別リポジトリに依存を 1 つ持つ代償より重い
- B は A と同等で、A を退けて選ぶ理由が無い
- リポジトリを分けたので（ADR-005）、go-sdk を採っても本体の依存方針を変える必要が無い

## 影響・結果

**ポジティブ**:
- MCP の手順を自前で持たずに済み、ツールの中身（制御口への中継）に集中できる
- kagelife 本体の依存・バイナリ・ビルド条件は変わらない。GUI だけを使う利用者のバイナリに MCP のコードは入らない

**ネガティブ**:
- **kagelife-mcp の間接依存が増える。** PoC の `go.mod`（go-sdk v1.8.0 だけを要求）では、次の間接依存が入った
  - `github.com/google/jsonschema-go`
  - `github.com/segmentio/asm`
  - `github.com/segmentio/encoding`
  - `github.com/yosida95/uritemplate/v3`
  - `golang.org/x/oauth2`
  - `golang.org/x/sync`
  - `golang.org/x/sys`
  - `golang.org/x/time`

  実際の一覧と版は、kagelife-mcp の feat/mcp-stdio-server の `go.mod` で確定させる
- SDK の更新に追従する作業が kagelife-mcp で定期的に発生する

**Action Items**:
- [ ] kagelife-mcp の feat/mcp-stdio-server で go-sdk v1.8.0 を追加し、PR 本文に `go.mod`（直接・間接）を載せる
- [ ] kagelife の PRD v0.2 の依存表に `golang.org/x/image` を載せる

## 参考資料
- go-sdk: https://github.com/modelcontextprotocol/go-sdk
- mcp-go: https://github.com/mark3labs/mcp-go
