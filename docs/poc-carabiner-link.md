# PoC: Carabiner 経由で Ableton Link のテンポと拍を受け取る

## 目的

rekordbox（DDJ-FLX4 で使用。Link ボタンあり）と kagelife のテンポ・拍を合わせられるかを確かめる。
後で「案 A の拍時計」に組み込む前提の部品（`internal/carabiner`）を先に作り、実機で精度と使い勝手を見る。

- kagelife 本体には Ableton Link の SDK を入れない（MIT を保つため）。
- Link との橋渡しは [Carabiner](https://github.com/Deep-Symmetry/carabiner)（v1.2.0）に任せる。
  Carabiner は別プロセスで、利用者が自分で起動する。
- kagelife 側は 127.0.0.1:17000 の TCP でテキストの行をやり取りするだけ（標準ライブラリのみ・cgo なし）。

## 構成

```
 rekordbox (LINK: ON) ──┐
                        │  Ableton Link（LAN / 同じ Mac 内のマルチキャスト）
 他の Link アプリ ───────┤
                        ▼
              ┌──────────────────┐
              │ Carabiner        │  別プロセス（利用者が起動）
              │ 127.0.0.1:17000  │
              └────────┬─────────┘
                       │ TCP・改行区切りのテキスト
                       │  → "enable-start-stop-sync\n"（接続のたび、-startstop 時）
                       │  → "status\n"（-poll ごと、既定 100ms）
                       │  ← "status { :peers 1 :bpm 128.000000 :start ... :beat 597.73 :playing true }\n"
                       ▼
 ┌───────────────────────────────────────────────────────────┐
 │ cmd/linkpoc                                               │
 │   internal/carabiner                                      │
 │     Client     接続・読み取り・status の定期送信・再接続        │
 │     ParseStatus 応答 1 行の解析（エラー応答は型付きのエラー）    │
 │     Estimator  (受信時のローカル単調時刻, beat, bpm) から外挿  │
 │   10Hz で 1 行ずつ表示                                      │
 └───────────────────────────────────────────────────────────┘
```

### 拍の求め方

Carabiner の `:start` や beat-at-time などの時刻は Link の単調時計のマイクロ秒で、Go の時計とは基準が違う。
PoC ではこれらを使わず、status の応答を受け取ったローカルの単調時刻 `t0` と `:beat`（`beat0`）・`:bpm` を組にして外挿する。

```
beat(now)  = beat0 + (now - t0) 秒 × bpm / 60
小節内の位置 = mod(beat, quantum)      （quantum の既定は 4 拍）
拍内の位相   = mod(beat, 1)            （0〜1）
```

status は定期的には送られてこない（接続直後・テンポ変更・peer 数の変化・再生状態の変化のときだけ）ので、
`-poll` の間隔で自分から `status` を投げて補正する。誤差は同じマシン内の往復の半分程度（1ms 未満の見込み）。

## 実機で確かめる手順

### 1. Carabiner を入手して署名を確かめる

1. <https://github.com/Deep-Symmetry/carabiner/releases> の v1.2.0 から macOS 用の配布物を落とし、展開する。
2. 署名と公証を確かめる（展開した場所で実行）。

   ```sh
   spctl --assess --type install -vv Carabiner
   ```

   `accepted` と `source=Notarized Developer ID` が出ることを確認する。`rejected` なら実行せず、入手元を見直す。

### 2. ターミナルから Carabiner を起動する

```sh
./Carabiner
```

- 既定で 127.0.0.1:17000 を待ち受ける。起動したターミナルは開いたままにする。
- 初回は macOS が「ローカルネットワーク上のデバイスの検出」や受信接続の許可を尋ねることがある。
  Link はローカルネットワークのマルチキャストで相手を探すので、許可しないと peers が 0 のままになる。

### 3. rekordbox で Link を ON にする

- PERFORMANCE モードで、画面上部の **LINK** を ON にする（環境設定で Ableton Link が有効になっている必要がある。表示の位置は版によって違う）。
- Carabiner のターミナルに peer が増えた旨が出る、または linkpoc の `peers=` が 1 以上になれば Link のセッションに入っている。

### 4. linkpoc を実行する

リポジトリの直下で実行する。

```sh
go run ./cmd/linkpoc
# フラグの例
go run ./cmd/linkpoc -quantum 4 -poll 100ms -startstop=true -addr 127.0.0.1:17000
```

| フラグ | 既定 | 意味 |
|---|---|---|
| `-addr` | `127.0.0.1:17000` | Carabiner の待ち受けアドレス |
| `-quantum` | `4` | 小節の長さ（拍） |
| `-poll` | `100ms` | `status` を投げる間隔 |
| `-startstop` | `true` | 接続のたびに `enable-start-stop-sync` を送る |

Carabiner が起動していないときは、起動を促す 1 行を出して再接続を待つ（100ms から倍々に延ばし、上限 2 秒）。
Ctrl+C で終了する。

### 5. 見る項目

表示は 10Hz で 1 行ずつ出る。

```
21:03:04.567 synced  peers=1 bpm=128.00 beat=597.74    bar=150.2  [#####...] play=on  ● +18ms
```

| 欄 | 意味 |
|---|---|
| 状態 | `offline`（未接続）／`waiting`（接続済み・status 未着）／`alone`（peers 0）／`synced`（同期中） |
| `peers` | Link セッションの相手の数 |
| `bpm` | テンポ |
| `beat` | 外挿した拍の位置（小数 2 桁） |
| `bar` | `小節.拍`（どちらも 1 始まり。quantum ごとに小節が進む） |
| `[####....]` | 拍内の位相（8 段） |
| `play` | Start/Stop Sync の再生状態（`on` / `off`、無効なら `-`） |
| `● +NNms` | 直前の行から拍の頭をまたいだ印と、拍の頭から表示までの遅れ |

`●` の行の出かたを、rekordbox の拍の表示（BEAT の点滅・波形の拍位置・BEAT COUNTDOWN の小節番号など）と目で比べる。
表示は 100ms 刻みなので、`●` は拍の頭から最大 100ms 遅れて出る。遅れの量は `+NNms` に出ている。

## 確かめる項目

| # | 項目 | 手順 | 期待 | 結果 |
|---|---|---|---|---|
| 1 | 署名の確認 | `spctl --assess --type install -vv Carabiner` | accepted / Notarized Developer ID | |
| 2 | Carabiner 未起動時の挙動 | Carabiner を起動せずに linkpoc を実行 | 案内が 1 行出て `offline` が続き、落ちない | |
| 3 | 接続 | Carabiner を起動 | `接続しました` の後、`alone` または `synced` になる | |
| 4 | peers | rekordbox の LINK を ON／OFF | `peers` が 1 以上／0 に変わり、状態が `synced`／`alone` に変わる | |
| 5 | テンポ | rekordbox でテンポを変える | `bpm` が 1 秒以内に追従する | |
| 6 | 拍の頭 | `●` と rekordbox の拍の点滅を比べる | 目で見てずれが分からない（表示の刻みの 100ms を除く） | |
| 7 | 小節 | `bar` の拍番号と rekordbox の小節内の位置 | quantum 4 で 1〜4 が rekordbox と一致する | |
| 8 | 再生と停止 | `-startstop=true` で rekordbox を再生／停止 | `play` が `on`／`off` に変わる（rekordbox が Start/Stop Sync に対応している場合） | |
| 9 | 切断と再接続 | linkpoc を動かしたまま Carabiner を終了し、再び起動 | `offline` になり、案内の後、自動で再接続する | |
| 10 | 長時間 | 10 分以上流す | 拍の頭のずれが広がらない | |
| 11 | 終了 | Ctrl+C | `# 終了します` を出して終わる | |

## 既知の制限と注意

- 外挿は「status を受け取った時刻」を基準にするので、Carabiner が応答を作ってから kagelife が読むまでの時間がそのまま遅れになる。
  同じ Mac 内の TCP なら往復で 1ms 未満の見込みで、映像のフレーム（60fps で約 16.7ms）より十分小さい。
- 100ms ごとに補正するので、補正の間に bpm が変わっても次の status（変化時は Carabiner から自発的に届く）で直る。
- rekordbox が Start/Stop Sync に対応していなければ `play` は変わらない。項目 8 はその確認も兼ねる。
- 表示の `●` は 10Hz の刻みに乗るので、精密な比較は `+NNms` を差し引いて読む。
