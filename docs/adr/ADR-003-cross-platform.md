# ADR-003: クロスプラットフォーム戦略 — ネイティブバイナリ（WASM見送り）

**ステータス**: Accepted
**日付**: 2026-04-18（改訂: 2026-09-30）
**決定者**: cyokozai

---

## コンテキスト

VJパフォーマンスに使うためmacOS以外でも動かしたい。
Ebitengineは`GOOS=js GOARCH=wasm`でWASMビルドに対応している。

## 検討した選択肢

### 選択肢 A: ネイティブバイナリのみ（macOS + Linux）
**概要**: `go build`のクロスコンパイルで4アーキテクチャを提供
**メリット**:
- ホットリロード（fsnotify）がそのまま動く
- ネイティブ速度、VJパフォーマンスに最適
- GitHub Actionsで自動ビルド可能
**デメリット**:
- Windows未対応（Windowsを使わないので問題なし）

### 選択肢 B: WASMのみ
**デメリット**:
- `fsnotify`がブラウザ環境で動作しない
- ホットリロードのコア機能を別方式で再設計する必要がある
- 1週間での実装は不可能

### 選択肢 C: ネイティブ + WASMのデュアルビルド
**デメリット**:
- WASM版は別UX（ファイルアップロード型）になり事実上別アプリ
- MVP期間での実装はスコープ超過

## 決定

**選択肢 A（ネイティブバイナリのみ）を採用する。WASMはV2以降。**

### 理由
ホットリロードはKageLifeのコア価値であり、WASMとは根本的に相性が悪い。
Goのクロスコンパイルで macOS arm64/amd64 + Linux amd64/arm64 の4バイナリを
GitHub Actionsで自動生成すれば、実用的なクロスプラットフォーム対応は達成できる。

### 前提条件
- Ebitengineがmacos/linuxでクロスコンパイル可能なこと

## 改訂（2026-09-30）: v1.0 のビルド方式（決定 D4）

当初は `go build` のクロスコンパイルで 4 アーキテクチャを作るとしていたが、v1.0 は **cgo=1（`CGO_ENABLED=1`）のネイティブビルド**とする。

### ビルド方式

| 対象 | ランナー | ビルド |
|------|---------|--------|
| darwin/arm64 | `macos-latest` | ネイティブ |
| darwin/amd64 | `macos-latest` | 同じランナーからのクロスビルド |
| linux/amd64 | `ubuntu-latest` | ネイティブ |
| linux/arm64 | `ubuntu-24.04-arm` | ネイティブ |

- ランナーは上表の構成とする予定だが、各ランナーの提供状況は未確認
- 配布物は対象ごとの tar.gz（バイナリ・`shaders/`・README・LICENSE）と、全 tar.gz の `SHA256SUMS`。詳細は [ADR-004](ADR-004-cicd-release.md)

### cgo=1 を採る理由

Ebitengine v2.10 は cgo 無しでもビルドは通るが、cgo 無しのバイナリが実機で起動するかは未確認である。
確認できていない経路を配布物にしないため、v1.0 は cgo=1 のネイティブビルドを採る。
Linux のビルドには Ebitengine が要求する開発ヘッダ（OpenGL・X11・ALSA など）が必要になる（CI での導入内容は composite action の setup-ebiten を参照）。

## 影響・結果

**Action Items**:
- [x] `.github/workflows/release.yml`で4アーキテクチャのmatrix buildを設定（上記のランナー構成・cgo=1。W1-5。実際の Actions では未実行）
- [ ] 各ランナー（`macos-latest` / `ubuntu-latest` / `ubuntu-24.04-arm`）の提供状況を確認
- [ ] V2でWASMを検討する際は「ファイルアップロード型プレビューア」として別機能扱いにする

## 参考資料
- Ebitengine クロスコンパイルガイド: https://ebitengine.org/ja/documents/install.html
