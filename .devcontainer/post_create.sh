#!/bin/bash
set -e

echo "==> [postCreate] Starting devcontainer setup..."

# ── 1. go.mod の初期化（初回のみ） ────────────────────────────
if [ ! -f go.mod ]; then
    echo "==> Initializing go.mod..."
    go mod init github.com/cyokozai/kagelife
fi

# ── 2. 依存ライブラリの取得 ────────────────────────────────────
echo "==> Installing dependencies..."
go mod download
go mod tidy

echo ""
echo "==> [postCreate] Setup complete."
echo "    - go build ./...      : ビルド確認"
echo "    - go test ./...       : テスト実行"
echo "    - golangci-lint run   : lint実行"
echo ""

# ── 3. GitHub CLI 認証（最後に配置・失敗しても前処理に影響しない） ──
gh auth login --web || true
gh config set editor "code --wait" || true
ORIGIN=$(git remote get-url origin 2>/dev/null || echo "")
if echo "$ORIGIN" | grep -q "git@github.com:"; then
    git remote set-url origin "$(echo "$ORIGIN" | sed 's|git@github.com:|https://github.com/|')"
fi
gh auth setup-git || true
