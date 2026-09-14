MODULE  := github.com/cyokozai/kagelife
BINARY  := kagelife
OUT_DIR := dist

# ── ビルドフラグ ───────────────────────────────────────────────
# CGO が必要（Ebitengine は Metal/OpenGL を使用）
CGO_ENABLED := 1
LDFLAGS     := -s -w

# ── デフォルトターゲット ───────────────────────────────────────
.DEFAULT_GOAL := help

.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ── 開発 ──────────────────────────────────────────────────────
.PHONY: run
run: ## アプリを起動する
	go run .

.PHONY: build
build: ## ネイティブバイナリをビルドする（カレントOS向け）
	CGO_ENABLED=$(CGO_ENABLED) go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

.PHONY: test
test: ## テストを実行する
	go test ./...

.PHONY: test-v
test-v: ## テストを詳細出力で実行する
	go test -v ./...

.PHONY: lint
lint: ## golangci-lint を実行する
	golangci-lint run

.PHONY: clean
clean: ## ビルド成果物を削除する
	rm -f $(BINARY)
	rm -rf $(OUT_DIR)

# ── クロスコンパイル（CGO のためランナーOSに依存） ─────────────
# 注意: CGO_ENABLED=1 のため、以下は対応するOSのCI上でのみ動作する。
#       macOS → darwin ターゲット, Linux → linux ターゲット
#       CI (GitHub Actions) でOS別ランナーを使ってビルドすること。

.PHONY: build-darwin-arm64
build-darwin-arm64: ## [macOS CI用] darwin/arm64 バイナリをビルド
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=$(CGO_ENABLED) \
		go build -ldflags "$(LDFLAGS)" -o $(OUT_DIR)/$(BINARY)-darwin-arm64 .

.PHONY: build-darwin-amd64
build-darwin-amd64: ## [macOS CI用] darwin/amd64 バイナリをビルド
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=$(CGO_ENABLED) \
		go build -ldflags "$(LDFLAGS)" -o $(OUT_DIR)/$(BINARY)-darwin-amd64 .

.PHONY: build-linux-amd64
build-linux-amd64: ## [Linux CI用] linux/amd64 バイナリをビルド
	GOOS=linux GOARCH=amd64 CGO_ENABLED=$(CGO_ENABLED) \
		go build -ldflags "$(LDFLAGS)" -o $(OUT_DIR)/$(BINARY)-linux-amd64 .

.PHONY: build-linux-arm64
build-linux-arm64: ## [Linux CI用] linux/arm64 バイナリをビルド
	GOOS=linux GOARCH=arm64 CGO_ENABLED=$(CGO_ENABLED) \
		go build -ldflags "$(LDFLAGS)" -o $(OUT_DIR)/$(BINARY)-linux-arm64 .

.PHONY: build-all
build-all: build-darwin-arm64 build-darwin-amd64 build-linux-amd64 build-linux-arm64 ## 全プラットフォーム向けにビルド（CI専用）

# ── リリース ──────────────────────────────────────────────────
.PHONY: dist
dist: $(OUT_DIR) build-all ## dist/ に全バイナリを生成する（CI専用）

$(OUT_DIR):
	mkdir -p $(OUT_DIR)
