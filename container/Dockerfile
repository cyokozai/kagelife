FROM golangci/golangci-lint:v2.14.0@sha256:ad862ba6b3798cbe0fd9fd7408d498fd74fbd2623a92406b2fd3898faf0bf98f AS golangci-lint

FROM golang:1.27.1-alpine

# ── ビルドツール + Ebitengine 依存（Alpine パッケージ名） ──────────
RUN apk add --no-cache \
    # CGO コンパイルに必要
    build-base \
    pkgconfig \
    # Ebitengine: OpenGL
    mesa-dev \
    # Ebitengine: X11 / 入力
    libx11-dev \
    libxrandr-dev \
    libxinerama-dev \
    libxcursor-dev \
    libxi-dev \
    libxxf86vm-dev \
    # Ebitengine: オーディオ
    alsa-lib-dev \
    # 汎用ツール
    git \
    curl \
    ca-certificates \
    bash

# ── GitHub CLI ──
RUN apk add --no-cache github-cli

# ── Go 開発ツール ──
# golangci-lint: 公式イメージから digest 固定で取得
# 版を上げるときは先頭の FROM のタグと digest を一緒に更新する
COPY --from=golangci-lint /usr/bin/golangci-lint /go/bin/golangci-lint

# delve (デバッガ)
RUN go install github.com/go-delve/delve/cmd/dlv@v1.27.2

# gopls (Language Server)
RUN go install golang.org/x/tools/gopls@v0.23.0

WORKDIR /workspace

CMD ["/bin/bash"]
