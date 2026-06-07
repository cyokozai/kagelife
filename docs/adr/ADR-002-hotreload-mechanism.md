# ADR-002: ホットリロード実装方式 — fsnotify + buffered channel

**ステータス**: Accepted
**日付**: 2026-04-18
**決定者**: cyokozai

---

## コンテキスト

KageLifeのコア機能は「ファイル保存 → 即座に画面反映」。
Ebitengineのゲームループは`Update()`が毎フレーム（60fps）メインgoroutineで動く。
ファイル監視はブロッキング操作なので、別goroutineで行う必要がある。

## 検討した選択肢

### 選択肢 A: fsnotify goroutine + buffered channel
**概要**: `FileWatcher`をgoroutineで起動し、イベントを`chan ReloadEvent`でGameに渡す。`Update()`内でnon-blockingな`select`で受け取る。
**メリット**:
- goroutine間通信がchannelで型安全
- ゲームループをブロックしない
- デバウンス処理をwatcher側に閉じ込められる
**デメリット**:
- channelの容量設計が必要（バッファ不足で落としうる）

### 選択肢 B: ポーリング（N秒ごとにファイルのmtimeを確認）
**メリット**: 実装がシンプル
**デメリット**:
- レイテンシが設定間隔に依存（200ms目標を達成しにくい）
- CPU使用率が無駄に上がる

### 選択肢 C: `sync.Mutex`で共有変数を直接保護
**メリット**: channelなしでシンプル
**デメリット**:
- ゲームループ内でロック取得するとフレームドロップリスク
- Go的にchannelの方が慣用的

## 決定

**選択肢 A（fsnotify goroutine + buffered channel）を採用する。**

### 理由
- レイテンシ目標（< 200ms）をfsnotifyなら確実に達成できる
- channelによる通信はGoの慣用的パターンで安全
- バッファサイズ8で、高速連続保存時のイベントロストを防ぐ

### 前提条件
- fsnotifyがmacOS/Linuxで動作すること（実績あり）
- デバウンス100msで重複イベントを抑制すること

## 影響・結果

**ポジティブ**:
- ゲームループへの影響ゼロ
- デバウンスをwatcher側に閉じ込め、Gameは単純に受け取るだけ

**ネガティブ**:
- fsnotifyがmacOS上でCreate+Writeの2イベントを発火することがある → デバウンスで対処

**Action Items**:
- [ ] Day 1 PoCで`ebiten.NewShader()`の再呼び出し可否を確認
- [ ] Day 2でfsnotifyのデバウンス値（50ms or 100ms）を計測して最適化
