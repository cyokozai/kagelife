---
phase: 4
title: Prioritization
date: 2026-04-18
---

# Phase 4: Prioritization — KageLive

## MoSCoW

### Must Have（MVPに必須）

| 機能 | 技術要素 | 目安日 |
|------|---------|-------|
| `.kage`ファイルのホットリロード（保存→自動再コンパイル→反映） | `fsnotify` + `ebiten.NewShader` | Day 2-3 |
| `Time` / `Resolution` Uniformを毎フレーム自動注入 | `DrawRectShaderOptions.Uniforms` | Day 1-2 |
| 複数シェーダーをキー（1〜9）で切り替え | `[]*ebiten.Shader` + キーイベント | Day 2-3 |
| コンパイルエラー時に前のシェーダーを維持（クラッシュしない） | エラーハンドリング | Day 3 |

### Should Have（できれば入れたい）

| 機能 | 技術要素 | 目安日 |
|------|---------|-------|
| 起動時・切り替え時にシェーダー一覧をターミナル表示 | `log` / `fmt` | Day 3 |
| `*ebiten.Shader`の明示的`Dispose()`によるメモリ管理 | Dispose API | Day 3 |

### Could Have（時間があれば）

| 機能 | 技術要素 | 目安日 |
|------|---------|-------|
| タップBPM（スペースキー連打→平均BPM計算→Uniformに渡す） | 時刻差計算 | Day 4-5 |
| カスタムUniform（`uniforms.json`やコメントから読み込み） | JSON解析 | Day 4-5 |
| ウィンドウタイトルに現在のシェーダー名を表示 | `ebiten.SetWindowTitle` | Day 3 |

### Won't Have（V2以降）

| 機能 | 理由 |
|------|------|
| シェーダー重ね合わせ（レイヤー合成） | `DrawRectShader`の複数パス設計が必要 |
| GUIパラメータUI（ImGui / tview） | UIフレームワーク選定・統合コスト |
| Ableton Link / MIDI連携 | Go実装が少なくスコープ超過 |
| 頂点シェーダー | Kageはフラグメントシェーダのみ |

## 1週間スケジュール（案）

```
Day 1: PoC — ebiten.NewShader()の再呼び出し・エラーハンドリング確認
Day 2: Core — fsnotify監視 + Time/Resolution Uniform自動注入
Day 3: Core — 複数シェーダー管理 + キー切り替え + Dispose
Day 4: Polish — エラー表示 + シェーダー一覧表示
Day 5: Could — タップBPM or カスタムUniform
Day 6: テスト・リファクタリング
Day 7: README整備・デモ動画
```
