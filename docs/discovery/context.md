---
phase: 0
title: Context
date: 2026-04-18
---

# Phase 0: Context — KageLive

## プロジェクト概要

| 項目 | 内容 |
|------|------|
| **アプリ名** | KageLive（KodeLifeにちなんで命名） |
| **概要** | Ebitengine + Kageシェーダーを使ったVJ向けリアルタイムコーディングツール |
| **リポジトリ** | https://github.com/cyokozai/kagelive |

## コンテキスト

| 項目 | 内容 |
|------|------|
| **ターゲットユーザー** | 作者自身（Goが書けるVJアーティスト） |
| **ステージ** | アイデア段階 |
| **チーム** | 1人 |
| **期間** | 約1週間 |
| **シェーダー言語** | Kage（Ebitengine内部言語） |
| **公開方針** | 個人利用（将来的にOSS公開も視野） |

## 技術的前提（Kageの制約）

- フラグメントシェーダのみ（頂点シェーダなし）
- 入力画像は最大4枚（`DrawRectShaderOptions.Images`）
- Uniform変数で外部パラメータを渡す
- Go互換の構文だが、スライス・ポインタ・構造体・メソッド定義は不可

## 競合調査結果

Kage特化のVJライブコーディングツールは存在しない。

| ツール | 言語/技術 | 差異 |
|--------|----------|------|
| kage-desk（tinne26） | Kage | 学習・プレビュー用。VJ操作機能なし |
| KodeLife | GLSL | 最も近い体験。Go非対応 |
| Hydra | JS | ブラウザベース。明示評価型 |
| Shadertoy | GLSL | Web。VJパフォーマンス向きでない |
| TouchDesigner | 独自 | 高機能だが重い |

→ **Go/Ebitengineエコシステムで軽量VJツールを自作する価値あり**

## 選択フレームワーク

- **パターン: F（時間制約あり） + E（開発者ツール）**
- **Cynefin: Complex（技術的不確かさが高い）**
- **最大の不確かさ: 1週間で何が実現可能か（Feasibility）**

| フェーズ | 使用フレームワーク |
|---------|-----------------|
| Phase 1 | JTBD |
| Phase 2 | OST, User Story Mapping |
| Phase 3 | Assumption Mapping（Feasibility重点） |
| Phase 4 | MoSCoW |
