# ADR-003: クロスプラットフォーム戦略 — ネイティブバイナリ（WASM見送り）

**ステータス**: Accepted
**日付**: 2026-04-18
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

## 影響・結果

**Action Items**:
- [ ] `.github/workflows/release.yml`で4アーキテクチャのmatrix buildを設定
- [ ] V2でWASMを検討する際は「ファイルアップロード型プレビューア」として別機能扱いにする

## 参考資料
- Ebitengine クロスコンパイルガイド: https://ebitengine.org/ja/documents/install.html
