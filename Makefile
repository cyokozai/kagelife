MODULE  := github.com/cyokozai/kagelife
BINARY  := kagelife
OUT_DIR := dist

# ── バージョン ─────────────────────────────────────────────────
# タグから決める。git が無い・.git が読めない環境（コンテナ等）では dev に落とす。
# CI (release.yml) では VERSION=<タグ名> を明示して渡す。
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null)
ifeq ($(strip $(VERSION)),)
override VERSION := dev
endif

# ── ビルドフラグ ───────────────────────────────────────────────
# Ebitengine v2.10 は macOS / Linux とも CGO_ENABLED=0 でもビルド自体は通る。
# ただし v1.0 の配布物は cgo=1 のネイティブビルドで出す（ADR-004 / v1.0 の決定）。
CGO_ENABLED := 1
# main.version は main.go 側で定義する。未定義のうちは -X は黙って無視される。
LDFLAGS     := -s -w -X main.version=$(VERSION)

# ── デフォルトターゲット ───────────────────────────────────────
.DEFAULT_GOAL := help

.PHONY: help
help:
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

# ── 開発 ──────────────────────────────────────────────────────
.PHONY: run
run: ## アプリを起動する
	go run .

.PHONY: build
build: ## ネイティブバイナリをビルドする（カレントOS向け）
	CGO_ENABLED=$(CGO_ENABLED) go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

.PHONY: test
test: ## テストを実行する（データ競合検出つき）
	go test -race ./...

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

# ── OS/アーキ別ビルド（cgo=1 のためビルド機の OS に依存） ──────
# CGO_ENABLED=1 なので、darwin は macOS 上、linux は Linux 上でビルドする。
#   - darwin/amd64 は arm64 の macOS から GOARCH=amd64 でクロスビルドできる
#   - linux/arm64 は arm64 の Linux 上でネイティブにビルドする
# CI (release.yml) では OS/アーキ別のランナーでこれらを呼ぶ。

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

# 配布物: dist/kagelife-<version>-<os>-<arch>.tar.gz
# 中身は kagelife-<version>-<os>-<arch>/ の下に binary・shaders/・README.md・（あれば）LICENSE。
PKG_DOCS := README.md $(wildcard LICENSE)

# $(call package,<os>,<arch>)
define package
	@set -e; \
	name=$(BINARY)-$(VERSION)-$(1)-$(2); \
	stage=$(OUT_DIR)/stage/$$name; \
	rm -rf $$stage; mkdir -p $$stage; \
	cp $(OUT_DIR)/$(BINARY)-$(1)-$(2) $$stage/$(BINARY); \
	cp -R shaders $$stage/shaders; \
	cp $(PKG_DOCS) $$stage/; \
	tar -czf $(OUT_DIR)/$$name.tar.gz -C $(OUT_DIR)/stage $$name; \
	rm -rf $(OUT_DIR)/stage; \
	echo "packaged $(OUT_DIR)/$$name.tar.gz"
endef

.PHONY: package-darwin-arm64
package-darwin-arm64: build-darwin-arm64 ## [macOS CI用] darwin/arm64 の tar.gz を作る
	$(call package,darwin,arm64)

.PHONY: package-darwin-amd64
package-darwin-amd64: build-darwin-amd64 ## [macOS CI用] darwin/amd64 の tar.gz を作る
	$(call package,darwin,amd64)

.PHONY: package-linux-amd64
package-linux-amd64: build-linux-amd64 ## [Linux CI用] linux/amd64 の tar.gz を作る
	$(call package,linux,amd64)

.PHONY: package-linux-arm64
package-linux-arm64: build-linux-arm64 ## [Linux CI用] linux/arm64 の tar.gz を作る
	$(call package,linux,arm64)

# sha256sum（Linux）が無ければ shasum -a 256（macOS）を使う。出力形式は同じ。
SHA256 := $(shell command -v sha256sum >/dev/null 2>&1 && echo sha256sum || echo "shasum -a 256")

.PHONY: checksum
checksum: ## dist/*.tar.gz の SHA256SUMS を作る
	@set -e; cd $(OUT_DIR); \
	ls *.tar.gz >/dev/null; \
	$(SHA256) *.tar.gz > SHA256SUMS; \
	cat SHA256SUMS
