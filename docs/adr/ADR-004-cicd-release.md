# ADR-004: CI/CD + リリース自動化 — GitHub Actions

**ステータス**: Accepted
**日付**: 2026-04-18
**決定者**: cyokozai

---

## コンテキスト

個人OSSとして公開予定。バイナリ配布とリリースノートの自動化が必要。
1人開発なので複雑なCI/CDは不要だが、品質チェックとリリース作業の省力化は重要。

## 決定

**GitHub Actionsで以下の2ワークフローを構成する。**

### ci.yml（品質チェック）

トリガー: `push` / `pull_request` to `main`

```yaml
jobs:
  lint:
    - golangci-lint run
  test:
    - go test ./...
```

### release.yml（リリース自動化）

トリガー: `push` tags `v*.*.*`

```yaml
jobs:
  build:
    strategy:
      matrix:
        include:
          - { goos: darwin,  goarch: arm64 }
          - { goos: darwin,  goarch: amd64 }
          - { goos: linux,   goarch: amd64 }
          - { goos: linux,   goarch: arm64 }
    steps:
      - go build -o kagelife-${{ matrix.goos }}-${{ matrix.goarch }}
  
  release:
    needs: build
    steps:
      - gh release create ${{ github.ref_name }}
          --generate-notes
          kagelife-darwin-arm64
          kagelife-darwin-amd64
          kagelife-linux-amd64
          kagelife-linux-arm64
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
- [ ] `.github/workflows/ci.yml`を実装
- [ ] `.github/workflows/release.yml`を実装
- [ ] `.github/release.yml`でラベル分類を設定
