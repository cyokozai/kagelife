---
phase: 3
title: Assumption Mapping
date: 2026-04-18
---

# Phase 3: Assumption Mapping — KageLife

## IDEO三角形の評価

| 軸 | 評価 | 理由 |
|----|------|------|
| **Desirability** | ✅ 低リスク | 自分がユーザーなので「欲しいか」は確定済み |
| **Viability** | ✅ 低リスク | 個人ツール・非商用なのでビジネスモデル不要 |
| **Feasibility** | ⚠️ 要検証 | Kage/Ebitengineの実行時挙動が未確認 |

## Assumption Map（Feasibility重点）

| # | 仮定 | 重要度 | 不確かさ | 優先検証 | 検証方法 |
|---|------|--------|---------|---------|---------|
| 1 | `ebiten.NewShader()`は実行時に何度でも再呼び出しできる | 🔴 High | 中 | **Day 1** | 最小PoC: ループで再コンパイル |
| 2 | コンパイルエラーを`error`として取得でき、クラッシュしない | 🔴 High | 低 | Day 1 | エラーを故意に仕込んでテスト |
| 3 | `fsnotify`がmacOSのfseventsで< 100msで検知できる | 🟡 Mid | 低 | Day 2 | ファイル保存→検知の計測 |
| 4 | `DrawRectShader`に`map[string]interface{}`でUniformを動的に渡せる | 🟡 Mid | 中 | Day 2 | 可変Uniform渡しのPoC |
| 5 | 複数の`*ebiten.Shader`をメモリに保持したままキー切替できる | 🟡 Mid | 低 | Day 2 | スライスで保持してキー切替 |
| 6 | `ebiten.NewShader()`の再呼び出しでメモリリークが起きない | 🟠 Mid | 高 | Day 3 | 繰り返し呼び出してメモリ計測 |
| 7 | `Time`をUniformとして毎フレーム渡してもパフォーマンスに影響しない | 🟢 Low | 低 | - | 実装時に確認 |

## 最大リスク

**仮定#1と#6が最重要。** `ebiten.NewShader()`の再利用可否とメモリ挙動はDay 1-3で必ずPoC確認する。

古い`*ebiten.Shader`を`.Dispose()`で明示解放する必要がある可能性が高い。
