# アーキテクチャ設計書: KageLife

**作成日**: 2026-04-18

---

## システム全体構成図

```mermaid
flowchart TD
    subgraph "KageLife プロセス"
        MAIN["main.go\nEbitengine起動・設定"]
        GAME["Game struct\nebiten.Game実装\nUpdate / Draw / Layout"]
        SM["ShaderManager\n*ebiten.Shaderの管理\nロード・切替・Dispose"]
        UB["UniformBuilder\nTime/Resolution/Custom\n毎フレーム生成"]
        FW["FileWatcher\nfsnotify goroutine"]
        CH["chan ReloadEvent\nbuffered(8)"]
    end

    subgraph "ファイルシステム"
        DIR["shaders/\n*.kageファイル群"]
    end

    subgraph "ユーザー操作"
        ED["外部エディタ\nVS Code / Neovim"]
        KB["キーボード\n1-9: 切替"]
    end

    ED -->|保存| DIR
    DIR -->|fsnotify Write/Create| FW
    FW -->|ReloadEvent| CH
    CH -->|"select (non-blocking)"| GAME
    GAME -->|Reload| SM
    KB -->|KeyPressed| GAME
    GAME -->|Switch| SM
    GAME -->|Build| UB
    UB -->|"map[string]any"| GAME
    GAME -->|DrawRectShader| SM
```

---

## ホットリロード シーケンス

```mermaid
sequenceDiagram
    participant ED as 外部エディタ
    participant FW as FileWatcher goroutine
    participant CH as chan ReloadEvent
    participant UPD as Game.Update()
    participant SM as ShaderManager

    ED->>FW: ファイル保存
    Note over FW: 100msデバウンス処理
    FW->>CH: ReloadEvent{Path}

    loop 毎フレーム (60fps)
        UPD->>CH: select (non-blocking)
        alt ReloadEventあり
            CH-->>UPD: path (string)
            UPD->>UPD: os.ReadFile(path)
            UPD->>SM: Reload(path, src)
            SM->>SM: ebiten.NewShader(src)
            alt コンパイル成功
                SM->>SM: old.Dispose()
                SM->>SM: shaders[idx] = newShader
                Note over SM: ターミナルに✅ログ出力
            else コンパイルエラー
                SM->>SM: 旧shaderを維持
                Note over SM: ターミナルに❌エラー出力
            end
        else イベントなし
            Note over UPD: 何もしない
        end
    end
```

---

## パッケージ構成

```
kagelife/
├── main.go                        # エントリーポイント・Game struct (Update/Draw/Layout)
├── internal/
│   ├── shadermgr/
│   │   ├── manager.go             # Manager: ロード・切替・Dispose（ebiten非依存）
│   │   └── manager_test.go
│   └── filewatcher/
│       ├── watcher.go             # Watcher: fsnotify goroutine + デバウンス
│       └── watcher_test.go
├── shaders/                       # ユーザーが編集する .kage ファイル置き場
│   └── example.kage
├── docs/
│   ├── prd.md
│   ├── architecture-decisions.md
│   ├── adr/
│   └── discovery/
└── .github/
    ├── release.yml       # リリースノートのラベル分類設定
    └── workflows/
        ├── ci.yml        # lint + test
        └── release.yml   # クロスコンパイル + GitHub Release
```

---

## 主要な型設計

```go
// ReloadEvent: FileWatcher → Game への通知
type ReloadEvent struct {
    Path string
}

// ShaderManager: シェーダーの一元管理
type ShaderManager struct {
    shaders []*ebiten.Shader  // ロード済みシェーダースライス
    names   []string          // ファイル名（表示用）
    active  int               // 現在アクティブなインデックス
}

// Game: ebiten.Game インターフェース実装
type Game struct {
    sm      *ShaderManager
    watcher *FileWatcher
    events  chan ReloadEvent   // buffered(8)
    startAt time.Time         // Time Uniform用
}
```

---

## CI/CD パイプライン

```mermaid
flowchart LR
    subgraph "ci.yml\nPR / push to main"
        C1["golangci-lint"] --> C2["go test ./..."]
    end

    subgraph "release.yml\ntag: v*.*.*"
        R1["go build\nGOOS=darwin GOARCH=arm64"]
        R2["go build\nGOOS=darwin GOARCH=amd64"]
        R3["go build\nGOOS=linux GOARCH=amd64"]
        R4["go build\nGOOS=linux GOARCH=arm64"]
        R1 & R2 & R3 & R4 --> R5["gh release create TAG\n--generate-notes\n--attach binaries"]
    end
```

### リリースノートの自動生成

`.github/release.yml` でPRラベルをセクション分類:

```yaml
changelog:
  categories:
    - title: "✨ New Features"
      labels: ["enhancement"]
    - title: "🐛 Bug Fixes"
      labels: ["bug"]
    - title: "🔧 Maintenance"
      labels: ["chore", "dependencies"]
```
