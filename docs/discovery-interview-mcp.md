# Discovery Interview: KageLife MCP（LLM 共演 VJ）

**実施日**: 2026-09-30
**対象**: LLM が Kage シェーダを操作してリアルタイムコーディング VJ を行う MCP サーバの追加
**前提**: [assumptions-20260930.md](assumptions-20260930.md)（16 項目すべて Yes。#3・#8・#15 は同日に変更）

---

## 背景

KageLife の外部からゲームループの状態を変える手段は、現状 `shaders/*.kage` のファイル変更だけである。
コンパイル結果を呼び出し側へ返す経路も無い（エラーは stderr と HUD にだけ出る）。
LLM を VJ の共演者にするには、書き換え・切り替え・テンポ操作と、その結果（成否・エラー位置・状態）を
LLM が受け取れる口が要る。先行事例（Strudel 向け MCP など）はどれもコードを丸ごと置き換え、エラーをテキストで返す方式で、
画面のスクリーンショットを返す事例は見つかっていない。

---

## ペルソナ

| ペルソナ | 説明 | v0.1 からの変化 |
|---------|------|---------------|
| VJ 兼 Go エンジニア（作者自身） | 手で Kage を書き、キーボードで切り替えて演じる | 変更なし |
| **LLM を共演者とする演者（追加）** | 作者本人が、Claude Desktop または Claude Code に指示を出しながら演じる。LLM はシェーダを書き、エラーを読んで直し、BPM とフェードを操作する。演者は必要ならキーボードで割り込む | 新規 |
| 観客 | 画面を見るだけ | 変更なし |

LLM（Claude Desktop / Claude Code）は利用者ではなく、MCP のツールを呼ぶ**共演者**として扱う。

---

## ユーザーストーリー

| # | ストーリー | 受け入れ条件 | 優先度 |
|---|-----------|------------|--------|
| US-1 | 演者として、LLM に「拍に合わせて脈打つ円にして」と頼むと、LLM が書いたシェーダがすぐ画面に出てほしい | write_shader の呼び出しから画面反映まで 200ms 未満（LLM の生成時間を除く）。成功したシェーダは `shaders/<名前>.kage` に残る | Must |
| US-2 | 演者として、LLM がコンパイルエラーを出しても映像が止まらず、LLM が自分で直してほしい | 失敗時は前のシェーダを維持し、ファイルにも触れない。LLM には行・列・メッセージの一覧（最大 10 件、ソースの行の範囲内）が返る | Must |
| US-3 | 演者として、LLM に今の状態（BPM・アクティブなシェーダ・fps・直近のエラー）を読ませ、拍に合わせてクロスフェードを仕掛けさせたい | get_state で状態が返る（BPM が既定の 120 か、設定・測定された値かも分かる）。crossfade はタップ前でも既定の 120 BPM で進み、LLM は set_bpm（40〜300）で先に BPM を設定できる（2026-10-01 改訂、PRD Q-008） | Must |
| US-4 | 演者として、Claude のクライアントが落ちたり再起動したりしても、映像は流れ続けてほしい | GUI は MCP クライアントと別プロセスで常駐する。制御口に届かない呼び出しはツールのエラーとして LLM に返り、描画には影響しない | Must |
| US-5 | 演者として、LLM に今の画面を見せ、見た目を踏まえて次の手を考えさせたい | capture_frame が縮小したフレーム（長辺既定 1024px、png / jpeg）を画像として返す | Must（後続 PR: feat/mcp-capture-frame） |

---

## MVP 範囲

### 含める

| 区分 | 内容 | 届け先（リポジトリ: ブランチ） |
|------|------|--------------|
| 制御口 v1 | 127.0.0.1 限定の HTTP/JSON、トークン認証、発見ファイル、Update キュー | kagelife: feat/mcp-control-api |
| MCP 中継 | 別リポジトリ `cyokozai/kagelife-mcp` の別バイナリ `kagelife-mcp`（stdio、純 Go）と 8 ツール: list_shaders / read_shader / write_shader / switch_shader / crossfade / set_bpm / get_state / get_kage_guide | kagelife-mcp: feat/mcp-stdio-server |
| 画面の確認 | capture_frame | kagelife: feat/mcp-capture-frame（制御口）＋ kagelife-mcp 側のツール追加 |
| ユーザー定義 uniform | set_uniform（`go/parser` で抽出したグローバル宣言が対象） | kagelife: feat/mcp-uniform-params（制御口）＋ kagelife-mcp 側のツール追加 |

前提 #6・#7 により capture_frame と set_uniform も MVP に含む。制御口の契約 v1 には入れず、後続 PR で契約を足す。

### 含めない（スコープ外）

- 差分での編集（write_shader は丸ごと置き換え）
- 重いシェーダの自動ロールバック（MVP は get_state の fps で知らせるだけ）
- シェーダの削除・リネーム（現状のアプリも未対応）
- ループバック以外からの接続、リモートからの操作
- MCP の Streamable HTTP トランスポート（MVP は stdio だけ）
- 画像入力・マルチパス・フィードバック描画
- Windows、WASM
- LLM の利用料の扱い（利用者の Claude 契約で賄う）

---

## 非機能要件

| 項目 | 目標値 |
|------|--------|
| フレームレート | 60fps を維持（MCP の呼び出し中も） |
| 反映の速さ | ツール呼び出し → 画面反映 200ms 未満（LLM の生成時間を除く）。PoC（Xvfb）の往復は 17ms |
| エラー耐性 | コンパイル失敗時は前のシェーダを維持。MCP 側・中継側の障害で描画を止めない |
| 応答の上限 | ゲームループが 2 秒以内に応答しなければ `loop_timeout` を返す |
| 安全性 | 書き込みは `shaders/` 直下の名前検査済みファイルだけ。127.0.0.1 限定、トークン認証 |
| 画像 | 呼ばれたときだけ取得。各辺 2000px 以下 |

---

## 制約

| 項目 | 内容 |
|------|------|
| MCP クライアント | Claude Code は stdio サーバを自動で再接続しない。Claude Desktop は画像対応と書かれているが、stdio での上限は未確認 |
| 画像トークン | 約 ⌈幅/28⌉×⌈高さ/28⌉。長辺 1280 で約 1200。会話中の画像が 20 枚を超えると寸法の制限が厳しくなる |
| Kage | 型は bool / int / float / vec / ivec / mat と固定長配列だけ。struct・slice・switch・import は使えない。画像入力は最大 4 枚 |
| 行番号 | v2.10.4 は `//kage:unit` 指定なし（texels）で行番号が 4 行ずれる。書き込みでは `//kage:unit pixels` を必須にする |
| 実行環境 | GUI は macOS ホストでネイティブ実行。Alpine（musl）のコンテナではウィンドウを伴うテストが SIGSEGV になるので CI に入れない |
| 既存不具合 | `shaders/06_kaleidoscope.kage` の `atan` はコンパイルに失敗する（fix/kaleidoscope-atan2 で修正） |
| 開発 | 1 人。1 タスク＝1 worktree＝1 PR。すべて dev から切って dev へ。kagelife と kagelife-mcp（public、main ← dev ← feat）の 2 リポジトリで進め、契約の正典は kagelife の ADR-006 に置く |

---

## 未確認事項

- macOS（Metal）実機での ReadPixels の速さ、ウィンドウ最小化時の挙動
- Claude Desktop への実際の接続と画像の上限
