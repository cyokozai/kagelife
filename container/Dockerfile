FROM golang:1.24-bookworm

# ── システム依存（Ebitengineビルドに必要なグラフィックス/オーディオライブラリ） ──
RUN apt-get update && apt-get install -y --no-install-recommends \
    # Ebitengine: OpenGL
    libgl1-mesa-dev \
    # Ebitengine: X11 / 入力
    libx11-dev \
    libxcursor-dev \
    libxi-dev \
    libxinerama-dev \
    libxrandr-dev \
    libxxf86vm-dev \
    # Ebitengine: オーディオ（未使用でもビルド要）
    libasound2-dev \
    # 汎用ツール
    git \
    curl \
    ca-certificates \
    && update-ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# ── GitHub CLI ──
RUN curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
        | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
        > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update && apt-get install -y --no-install-recommends gh \
    && rm -rf /var/lib/apt/lists/*

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
