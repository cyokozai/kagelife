# KageLife

KageLife は，Ebitengine の Kage シェーダーを保存するだけで画面にリアルタイム反映する，Go 製の VJ ライブコーディングツールです．
好きなエディタで `shaders/` 以下の `.kage` を書き換えて保存すると，自動で再コンパイルして描画を差し替えます．
`Time` や `Beat` などの Uniform は自動で注入され，数字キーでのシェーダー切り替え，タップテンポに同期したクロスフェードでパフォーマンスできます．

---

## 使い方

### 起動

リポジトリのルートディレクトリで次のどちらかを実行します．

```bash
make run
# または
go run .
```

シェーダーは相対パス `shaders/` から読み込みます．必ずリポジトリのルートで起動してください．
起動時に `shaders/` 直下の `.kage` をファイル名順にすべてコンパイルし，読み込んだ一覧をターミナルに表示します．
コンパイルできなかったファイルは警告を出して読み飛ばします．

### シェーダーを置く・書き換える（ホットリロード）

- `shaders/` 直下に `.kage` ファイルを置きます（サブディレクトリは監視しません）．
- ファイルを保存すると（書き込みまたは新規作成のイベント），100ms のデバウンスをかけてから再コンパイルし，描画を差し替えます．
- 起動後に新しく作った `.kage` は，一覧の末尾に追加されます．
- コンパイルエラーのときは直前のシェーダーをそのまま描画し続け，HUD とターミナルにエラーを表示します．アプリは落ちません．

### キー操作

| キー | 動作 |
| --- | --- |
| `1` 〜 `9` | 読み込み順で N 番目のシェーダーに即座に切り替える |
| `Shift` + `1` 〜 `9` | N 番目のシェーダーへクロスフェードする（フェード時間は拍数で指定） |
| `Space` | タップテンポ．叩いた間隔から BPM を求める |
| `[` | クロスフェードの拍数を 1 段階減らす |
| `]` | クロスフェードの拍数を 1 段階増やす |
| `H` | HUD（BPM・フェード拍数・ミックス比・エラー）の表示を切り替える |
| `F` | フルスクリーンを切り替える |

- クロスフェードの拍数は `0.5 / 1 / 2 / 3 / 4 / 8 / 16` から選びます．初期値は `4` です．
- クロスフェードは BPM から時間を計算するため，BPM が 0 のまま（一度もタップテンポをしていない状態）では進みません．先に `Space` を 2 回以上叩いてください．
- ウィンドウの初期サイズは 640x480 で，リサイズできます．

### 自動注入される Uniform

シェーダー内で同じ名前と型の変数を宣言すると，毎フレーム値が入ります．宣言していない Uniform は無視されます．

| 名前 | Kage の型 | 意味 |
| --- | --- | --- |
| `Time` | `float` | 起動からの経過秒数 |
| `Resolution` | `vec2` | 描画先の幅と高さ（ピクセル） |
| `Beat` | `float` | 最後のタップを拍頭とした拍内の位相 `0..1`．2 秒以上タップが空くと BPM 測定をやり直す．BPM が未確定のときは `0` |
| `Cursor` | `vec2` | マウスカーソルの位置（ピクセル） |
| `Frame` | `int` | 起動からのフレーム数 |
| `Random` | `float` | フレームごとに変わる `[0, 1)` の乱数 |

### 最小のシェーダー例

```go
//kage:unit pixels
package main

var Time float
var Beat float
var Resolution vec2

func Fragment(dstPos vec4, srcPos vec2, color vec4) vec4 {
	uv := dstPos.xy / Resolution
	pulse := 1.0 - Beat
	r := sin(Time + uv.x*6.28)*0.5 + 0.5
	return vec4(r*pulse, uv.y*pulse, 0.5, 1.0)
}
```

`//kage:unit pixels` を付けると `dstPos` がピクセル座標になり，`Resolution` で割ると `0..1` の座標になります．

### サンプルシェーダー

| ファイル | 内容 |
| --- | --- |
| `shaders/01_uv.kage` | UV 座標をそのまま色にする |
| `shaders/02_time_sin.kage` | `Time` の sin 波で色が移ろうグラデーション |
| `shaders/03_smooth_circle.kage` | 拍に合わせて脈打つ，縁のなめらかな円 |
| `shaders/04_ripple.kage` | 中心から広がる波紋が拍で光る |
| `shaders/05_rotation_grid.kage` | 回転する格子．拍で明るさが変わる |
| `shaders/06_kaleidoscope.kage` | 6 分割の万華鏡 |
| `shaders/07_plasma.kage` | sin を重ねたプラズマ |
| `shaders/08_beat_flash.kage` | 拍頭で広がって減衰するフラッシュ |
| `shaders/09_cursor_light.kage` | マウスカーソルの位置に光源が付いてくる |

---

## 必要環境

- Go 1.27 以上
- cgo（`CGO_ENABLED=1`）と C コンパイラ．Ebitengine が OpenGL / Metal を使うためです．
- Linux では X11 と OpenGL の開発パッケージが要ります．Dev Container のイメージ（Alpine）では次のパッケージを入れています．
  - `build-base` `pkgconfig` `mesa-dev` `libx11-dev` `libxrandr-dev` `libxinerama-dev` `libxcursor-dev` `libxi-dev` `libxxf86vm-dev` `alsa-lib-dev`

---

## 開発

### Dev Container

Dev Container を使うと，チーム全員が同じ開発環境を再現できます．
イメージは `container/Dockerfile` から作られ，Go ツールチェーン，上記の開発パッケージ，`golangci-lint`，`dlv`，`gopls`，GitHub CLI が入っています．

#### 前提条件

- [Docker](https://www.docker.com/products/docker-desktop/) がインストール済みであること
- [Visual Studio Code](https://code.visualstudio.com/) がインストール済みであること
- VS Code 拡張機能 [Dev Containers](https://marketplace.visualstudio.com/items?itemName=ms-vscode-remote.remote-containers) がインストール済みであること

#### 手順

1. VS Code でリポジトリのルートディレクトリを開く
2. コマンドパレットを開く（`Cmd+Shift+P` / `Ctrl+Shift+P`）
3. `Dev Containers: Reopen in Container` を選択する
4. コンテナのビルドが完了するまで待つ

コンテナ起動後は，`.devcontainer/post_create.sh` が依存ライブラリを取得します（`go mod download` / `go mod tidy`）．

### make ターゲット

| コマンド | 内容 |
| --- | --- |
| `make run` | アプリを起動する |
| `make test` | `go test ./...` を実行する |
| `make test-v` | テストを詳細出力で実行する |
| `make lint` | `golangci-lint run` を実行する |
| `make build` | カレント OS 向けのネイティブバイナリ `kagelife` をビルドする |
| `make clean` | ビルド成果物を削除する |
| `make help` | ターゲットの一覧を表示する |

`build-darwin-*` / `build-linux-*` / `build-all` / `dist` は cgo を使うため，対応する OS のランナー（CI）でのみ動きます．

### テスト駆動開発

テスト駆動（t_wada 流: Red → Green → Refactor）で進めます．

```text
Red      → 失敗するテストを書く
Green    → テストが通る最小限のコードを書く
Refactor → コードをきれいにする（テストは常にグリーン）
```

ロジックは `internal/` 以下のパッケージ（`filewatcher` / `shadermgr` / `tempo`）に切り出してユニットテストを書きます．
シェーダーの描画結果そのものは目視で確認します．

### CI

GitHub Actions（`.github/workflows/ci.yml`）で，`main` / `dev` への push とプルリクエストごとに，gofmt の未整形検出・`go vet`・`go test`・`golangci-lint` を実行します．

### ディレクトリ構成

```text
.
├── main.go                 # エントリポイント．入力処理・Uniform 注入・描画
├── internal/
│   ├── filewatcher/        # fsnotify による shaders/ の監視とデバウンス
│   ├── shadermgr/          # シェーダーのロード・再コンパイル・切り替え・クロスフェード
│   └── tempo/              # タップテンポ（BPM と拍内の位相）
├── shaders/                # .kage シェーダー（ホットリロード対象）
├── container/              # 開発用 Dockerfile
├── .devcontainer/          # Dev Container 設定
└── docs/                   # PRD・ADR・開発プロセス
```

設計判断の記録は `docs/adr/` に，要件は `docs/prd.md` にあります．

---

## 開発フロー

### 1. ブランチ保護ルールの設定（Rulesets）

`main` ブランチへの直接プッシュを禁止し，プルリクエスト経由のマージを強制します．
GitHub の新しい **Rulesets** を使用して設定します．

#### 設定手順

1. GitHub リポジトリの **Settings** > **Rules** > **Rulesets** を開く
2. **New ruleset** > **New branch ruleset** をクリック
3. 以下を設定して **Save changes** をクリックする

#### Ruleset 設定内容

| 項目 | 値 |
| --- | --- |
| Ruleset name | `pullreq`（任意） |
| Enforcement status | Active |
| Target branches | Default branch（`main`） |

#### Bypass list

| ロール | 許可内容 |
| --- | --- |
| Organization admin | Allow for pull requests only |
| Repository admin | Always allow |

#### 有効にするルール

| ルール | 説明 |
| --- | --- |
| Restrict deletions | ブランチの誤削除を防ぐ |
| Require a pull request before merging | マージ前に PR を必須にする |
| Require status checks to pass | CI テストが通過しないとマージ不可 |
| Block force pushes | 履歴の強制書き換えを禁止する |
| Automatically request Copilot code review | PR 作成時に Copilot によるコードレビューを自動リクエストする |

---

### 2. チケット駆動開発のブランチ運用

GitHub Issues をチケットとして使用し，1 チケット 1 ブランチで作業を管理します．

#### ブランチ運用フロー

```text
main
 └─ dev
     └─ feat/*** ─── 作業 ─── PR ──→ dev ─── PR ──→ main
```

##### feat/\*\*\* → dev（日常の開発）

1. GitHub Issues で `[FEAT]` チケットを作成する
2. `dev` ブランチから `feat/***` ブランチを切る（ブランチ名はチケットに記載）
3. `feat/***` ブランチで作業を行う
4. 作業完了後，`feat/***` から `dev` へ PR を作成してマージする

##### dev → main（リリース）

`dev` から `main` へマージするには，以下の条件をすべて満たす必要があります．

| 条件 | 状態 |
| --- | --- |
| CI テストが全て通過していること | 必須 |
| CD によるステージング環境へのデプロイが成功していること | 予定 |
| 開発者がログ・メトリクス・トレースの取得を確認していること | 予定 |

---

### 3. コミットメッセージテンプレートの設定

`.gitmessage` をコミットメッセージのテンプレートとして使用します．
リポジトリをクローン後，以下のコマンドを **1回だけ** 実行してください．

```bash
git config commit.template .gitmessage
```

設定後は `git commit` を実行すると，以下のテンプレートがエディタに表示されます．

```text
# feat | fix | docs | refactor | test | chore
<type>: <subject>

Refs: #
```

| type | 用途 |
| --- | --- |
| `feat` | 新機能の追加 |
| `fix` | バグ修正 |
| `docs` | ドキュメントのみの変更 |
| `refactor` | 機能変更を伴わないコード改善 |
| `test` | テストの追加・修正 |
| `chore` | ビルド・設定などの雑務 |

---

### 4. RACI

各チケットには以下の役割を記載します．**R はチケット作成者自身**が担います．

| 役割 | 説明 |
| --- | --- |
| R: 実行責任者 (Responsible) | 実際に作業を行う人．チケット作成者が担当する |
| A: 説明責任者 (Accountable) | 成果物に対して最終責任を持つ人 |
| C: 協業先 (Consulted) | 作業に際して相談・協力を求める人 |
| I: 報告先 (Informed) | 進捗・完了を報告する人 |
