# ADR-004: CI/CD + リリース自動化 — GitHub Actions

**ステータス**: Accepted
**日付**: 2026-04-18（改訂: 2026-09-30）
**決定者**: cyokozai

---

## コンテキスト

個人OSSとして公開予定。バイナリ配布とリリースノートの自動化が必要。
1人開発なので複雑なCI/CDは不要だが、品質チェックとリリース作業の省力化は重要。

## 決定

**GitHub Actionsで以下の2ワークフローを構成する。**

### ci.yml（品質チェック）

トリガー: `push` / `pull_request` to `main` と `dev`

```yaml
env:
  CGO_ENABLED: "1"
jobs:
  test:   # ubuntu-latest。Ebitengine のビルド依存（開発ヘッダ）を setup-ebiten で導入
    - gofmt -l .          # 未整形のファイルがあれば失敗
    - go vet ./...
    - go test -race ./...
  lint:   # ubuntu-latest
    - golangci-lint run   # golangci-lint v2（golangci-lint-action）
```

- Ebitengine のビルド依存の導入は composite action（setup-ebiten）にまとめ、`ci.yml` と `release.yml` で共用する

### release.yml（リリース自動化）

トリガー: `push` tags `v*.*.*`

ビルドは cgo=1 のネイティブビルド（[ADR-003](ADR-003-cross-platform.md) の改訂 D4）。ランナーの提供状況は未確認で、`release.yml` は実際の Actions ではまだ実行していない。

```yaml
jobs:
  build:
    strategy:
      matrix:
        include:
          - { goos: darwin, goarch: arm64, runner: macos-latest }
          - { goos: darwin, goarch: amd64, runner: macos-latest }      # クロスビルド
          - { goos: linux,  goarch: amd64, runner: ubuntu-latest }
          - { goos: linux,  goarch: arm64, runner: ubuntu-24.04-arm }
    runs-on: ${{ matrix.runner }}
    env:
      CGO_ENABLED: "1"
    steps:
      - go build -o kagelife
      - tar.gz にまとめる: kagelife（バイナリ）・shaders/・README.md・LICENSE
        → kagelife-${{ matrix.goos }}-${{ matrix.goarch }}.tar.gz

  release:
    needs: build
    steps:
      - 全 tar.gz の SHA256SUMS を作る
      - gh release create ${{ github.ref_name }}
          --generate-notes
          kagelife-darwin-arm64.tar.gz
          kagelife-darwin-amd64.tar.gz
          kagelife-linux-amd64.tar.gz
          kagelife-linux-arm64.tar.gz
          SHA256SUMS
```

### リリースノート分類（.github/release.yml）

```yaml
changelog:
  categories:
    - title: "✨ New Features"
      labels: ["enhancement"]
    - title: "🐛 Bug Fixes"
      labels: ["bug"]
    - title: "🔧 Maintenance"
      labels: ["chore", "dependencies"]
  exclude:
    labels: ["skip-changelog"]
```

## 理由

- `gh release create --generate-notes`はPRタイトル・コミットメッセージから自動でChangelog生成
- matrix buildで4アーキテクチャを並列ビルド、手動作業ゼロ
- シンプルな構成で1週間以内に整備できる

**Action Items**:
- [x] `.github/workflows/ci.yml`を実装（gofmt・vet・test・golangci-lint v2。対象は `main` と `dev`）
- [x] `ci.yml` の `go test` に `-race` を付ける（W1-5）
- [x] `.github/workflows/release.yml`を実装（cgo=1 のネイティブビルド、tar.gz と `SHA256SUMS`。W1-5）
- [x] `.github/release.yml`でラベル分類を設定（W1-5）
- [x] Ebitengine のビルド依存を composite action（setup-ebiten）にまとめ、Makefile に package と checksum を用意（W1-5）
- [ ] `release.yml` を実際の Actions で実行し、各ランナーの提供状況を確かめる
- [ ] LICENSE ファイルを追加する（配布物に同梱するが、2026-09-30 時点でリポジトリに無い）
- [ ] `v1.0.0` タグを打って GitHub Release を確認
