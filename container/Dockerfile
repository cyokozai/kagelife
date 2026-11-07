FROM golangci/golangci-lint:v2.15.0@sha256:fbfcfd12a6f8d7f1b97b3362ca80fc2a244d46863148c73cb88e7793af96c481 AS golangci-lint

FROM golang:1.27.3-alpine

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
