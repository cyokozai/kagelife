FROM golang:1.26-alpine

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
# golangci-lint
RUN curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh \
    | sh -s -- -b "$(go env GOPATH)/bin" v1.64.8

# delve (デバッガ)
RUN go install github.com/go-delve/delve/cmd/dlv@latest

# gopls (Language Server)
RUN go install golang.org/x/tools/gopls@latest

WORKDIR /workspace

CMD ["/bin/bash"]
