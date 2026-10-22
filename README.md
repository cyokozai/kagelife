# KageLife

KageLife は，Ebitengine の Kage シェーダーを保存するだけで画面にリアルタイム反映する，Go 製の VJ ライブコーディングツールです．
好きなエディタで `shaders/` 以下の `.kage` を書き換えて保存すると，自動で再コンパイルして描画を差し替えます．
`Time` や `Beat` などの Uniform は自動で注入され，数字キーでのシェーダー切り替え，タップテンポに同期したクロスフェードでパフォーマンスできます．

---

## インストール

[GitHub Releases](https://github.com/cyokozai/kagelife/releases) からビルド済みの配布物を入手できます．
対象は次の 4 つです．

| OS | アーキテクチャ | ファイル名 |
| --- | --- | --- |
| macOS | Apple シリコン | `kagelife-<version>-darwin-arm64.tar.gz` |
| macOS | Intel | `kagelife-<version>-darwin-amd64.tar.gz` |
| Linux | x86_64 | `kagelife-<version>-linux-amd64.tar.gz` |
| Linux | arm64 | `kagelife-<version>-linux-arm64.tar.gz` |

### 1. ダウンロードと検証

使う環境の `tar.gz` と，同じリリースにある `SHA256SUMS` を同じディレクトリに置き，チェックサムを確かめます．

`SHA256SUMS` には全 OS の配布物が載っているので，落としたファイルの行だけを取り出して検証します．

```bash
# macOS
grep kagelife-<version>-<os>-<arch>.tar.gz SHA256SUMS | shasum -a 256 -c -
# Linux
grep kagelife-<version>-<os>-<arch>.tar.gz SHA256SUMS | sha256sum -c -
```

`OK` と表示されれば検証は完了です．

### 2. 展開

```bash
tar -xzf kagelife-<version>-<os>-<arch>.tar.gz
cd kagelife-<version>-<os>-<arch>
```

展開したディレクトリには次のファイルが入っています．

| ファイル | 内容 |
| --- | --- |
| `kagelife` | 実行ファイル |
| `shaders/` | サンプルシェーダー |
| `README.md` | このファイル |
| `LICENSE` | ライセンス（MIT） |

### 3. 起動前の準備

- **macOS**: ブラウザで落としたファイルには quarantine 属性が付き，そのままでは Gatekeeper に止められます．次のコマンドで外してください．

  ```bash
  xattr -d com.apple.quarantine kagelife
  ```

- **Linux**: X11 と OpenGL の実行時ライブラリが要ります．デスクトップ環境の入った多くのディストリビューションでは入っています．

展開したディレクトリで `./kagelife` を実行すると起動します．

---

## 使い方

### 起動

配布物を使う場合は，展開したディレクトリで次を実行します．

```bash
./kagelife
```

ソースから動かす場合は，リポジトリのルートディレクトリで次のどちらかを実行します．

```bash
make run
# または
go run .
```

シェーダーは既定で相対パス `shaders/` から読み込みます．別の場所を使うときは `-shaders` で指定します．
起動時に `shaders/` 直下の `.kage` をファイル名順にすべてコンパイルし，読み込んだ一覧をターミナルに表示します．
コンパイルできなかったファイルは警告を出しますが，ファイル名順の番号はそのまま持ちます（後述の「シェーダーの番号」を参照）．

### 起動オプション

| オプション | 既定値 | 内容 |
| --- | --- | --- |
| `-shaders <dir>` | `shaders` | シェーダーを読み込み，監視するディレクトリ |
| `-control-addr <addr>` | `127.0.0.1:0` | 制御口の待受アドレス．ポート `0` は空いているポートを自動で選ぶ |
| `-version` | — | バージョンを表示して終了する |

```bash
./kagelife -shaders ~/vj/shaders
```

### シェーダーを置く・書き換える（ホットリロード）

- `shaders/` 直下に `.kage` ファイルを置きます（サブディレクトリは監視しません）．
- ファイルを保存すると（書き込みまたは新規作成のイベント），100ms のデバウンスをかけてから再コンパイルし，描画を差し替えます．
- 起動後に新しく作った `.kage` は，ファイル名順の位置に加わります．
- コンパイルエラーのときは直前のシェーダーをそのまま描画し続け，HUD とターミナルにエラーを表示します．アプリは落ちません．

### シェーダーの番号

- `shaders/` 直下の `.kage` は，ファイル名の昇順に `1`，`2`，`3` … と番号（スロット）を持ちます．
- コンパイルに失敗したファイルも番号を持ちます．あとで直して保存すれば，その番号のまま使えるようになります．
- コンパイルに失敗したファイルの番号キー（`Shift` 付きも含む）を押しても無視され，表示は変わりません．
- 番号をそろえたいときは，`01_uv.kage` のようにファイル名の先頭に数字を付けてください．

### キー操作

| キー | 動作 |
| --- | --- |
| `1` 〜 `9` | N 番目のシェーダーに即座に切り替える |
| `Shift` + `1` 〜 `9` | N 番目のシェーダーへクロスフェードする（フェード時間は拍数で指定） |
| `Space` | タップテンポ．叩いた間隔から BPM を求める |
| `[` | クロスフェードの拍数を 1 段階減らす |
| `]` | クロスフェードの拍数を 1 段階増やす |
| `H` | HUD（BPM・フェード拍数・ミックス比・エラー）の表示を切り替える |
| `F` | フルスクリーンを切り替える |

- ウィンドウの初期サイズは 640x480 で，リサイズできます．
- HUD の BPM は，まだタップしていない既定値のときに `(default)` と表示されます．

### クロスフェード

- クロスフェードの拍数は `0.5 / 1 / 2 / 3 / 4 / 8 / 16` から選びます．初期値は `4` です．
- フェードの長さは拍数と BPM から求めます．タップ前でも既定の 120 BPM で進むので，すぐに使えます．
- フェード中に拍数や BPM を変えても画面は飛ばず，その時点から新しい速さで進みます．
- フェード中に別のフェードを指示したときは，次のように扱います（Mix は切り替え先が占める割合 `0..1`）．

| 指示 | 動作 |
| --- | --- |
| フェード中の切り替え先と同じシェーダー | 無視する（フェードを続ける） |
| Mix < 0.5 で，切り替え元のシェーダー | フェードを取りやめ，切り替え元のままにする |
| 上記以外で，Mix ≥ 0.5 | 切り替え先を確定させてから，新しいフェードを始める |
| 上記以外で，Mix < 0.5 | 切り替え元のままにして，新しいフェードを始める |

詳細は [ADR-008](docs/adr/ADR-008-crossfade-compositing.md) にあります．

### 自動注入される Uniform

シェーダー内で同じ名前と型の変数を宣言すると，毎フレーム値が入ります．宣言していない Uniform は無視されます．

| 名前 | Kage の型 | 意味 |
| --- | --- | --- |
| `Time` | `float` | 起動からの経過秒数 |
| `Resolution` | `vec2` | 描画先の幅と高さ（ピクセル） |
| `Beat` | `float` | 拍内の位相 `0..1`．タップ前は既定の 120 BPM で刻み，タップすると最後のタップを拍頭として叩いた間隔の BPM で刻む．2 秒以上空けてタップし直しても直前の BPM を保つ．BPM は 40〜300 に制限される |
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

### 制御口（MCP 連携）

KageLife は起動中，`127.0.0.1` に HTTP/JSON の制御口（v1）を開きます．
待受アドレスは `-control-addr` で変えられます．同じマシンの外からは届きません．
LLM などの外部のプログラムは，この制御口を通して状態の取得，シェーダーの読み書き，切り替えとクロスフェード，BPM の設定を行えます．

- 契約（エンドポイント・認証・エラー）は [ADR-006](docs/adr/ADR-006-control-api.md) を正典とします．
- MCP の stdio サーバは別リポジトリ [cyokozai/kagelife-mcp](https://github.com/cyokozai/kagelife-mcp) にあり，この制御口へ中継します．

---

## 必要環境（ソースからビルドする場合）

配布物（GitHub Releases の `tar.gz`）を使うだけなら，Go や cgo は要りません．以下はソースからビルドする場合の要件です．

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
| `make test` | `go test -race ./...` を実行する（データ競合検出つき） |
| `make test-v` | テストを詳細出力で実行する |
| `make lint` | `golangci-lint run` を実行する |
| `make build` | カレント OS 向けのネイティブバイナリ `kagelife` をビルドする |
| `make clean` | ビルド成果物を削除する |
| `make help` | ターゲットの一覧を表示する |

`build-darwin-*` / `build-linux-*` / `build-all` / `dist` / `package-*` は cgo を使うため，対応する OS のランナー（CI）でのみ動きます．

### テスト駆動開発

テスト駆動（t_wada 流: Red → Green → Refactor）で進めます．

```text
Red      → 失敗するテストを書く
Green    → テストが通る最小限のコードを書く
Refactor → コードをきれいにする（テストは常にグリーン）
```

ロジックは `internal/` 以下のパッケージ（`control` / `filewatcher` / `shadermgr` / `tempo` / `uniform`）に切り出してユニットテストを書きます．
シェーダーの描画結果そのものは目視で確認します．

### CI

GitHub Actions で次を実行します．

| ワークフロー | きっかけ | 内容 |
| --- | --- | --- |
| `.github/workflows/ci.yml` | `main` / `dev` への push とプルリクエスト | gofmt の未整形検出・`go vet`・`go test -race`・golangci-lint |
| `.github/workflows/release.yml` | タグ `v*.*.*` の push | CI を通したうえで 4 ターゲットをビルドし，`tar.gz` と `SHA256SUMS` を添えて GitHub Release を作る．タグに `-` を含む（`v1.0.0-rc.1` など）ときはプレリリースにする |

### ディレクトリ構成

```text
.
├── main.go                 # エントリポイント．入力処理・Uniform 注入・描画
├── internal/
│   ├── control/            # 127.0.0.1 の HTTP/JSON の制御口 v1（ADR-006）
│   ├── filewatcher/        # fsnotify による shaders/ の監視とデバウンス
│   ├── shadermgr/          # シェーダーのロード・再コンパイル・切り替え・クロスフェード
│   ├── tempo/              # タップテンポ（BPM と拍内の位相）
│   └── uniform/            # シェーダーに渡す Uniform の組み立て
├── shaders/                # .kage シェーダー（ホットリロード対象）
├── container/              # 開発用 Dockerfile
├── .devcontainer/          # Dev Container 設定
└── docs/                   # PRD・ADR・開発プロセス
```

設計判断の記録は `docs/adr/` に，要件は `docs/prd.md` にあります．

---

## 開発フロー

### 1. ブランチ保護ルールの設定（Rulesets）

`main` と `dev` への直接プッシュを禁止し，プルリクエスト経由のマージを強制します．
GitHub の **Rulesets**（**Settings** > **Rules** > **Rulesets**）で設定しています．

#### 対象ブランチ

`main` と `dev`

#### 有効なルール

| ルール | 説明 |
| --- | --- |
| Restrict deletions | ブランチの削除を禁止する |
| Block force pushes | force push による履歴の書き換えを禁止する |
| Require a pull request before merging | マージ前に PR を必須にする．承認 1 件・コードオーナーのレビュー・レビューのスレッドの解決が必要．マージ方式は merge のみ |
| Automatically request Copilot code review | PR 作成時に Copilot のコードレビューを自動でリクエストする |

- 必須のステータスチェックは，今は設定していません．CI の `test` / `lint` を登録する予定です．
- 個人リポジトリのため，Organization admin によるバイパスはありません．

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

---

## ライセンス

MIT License です．全文は [LICENSE](LICENSE) を参照してください．
