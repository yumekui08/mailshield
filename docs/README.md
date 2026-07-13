# MailShield ドキュメント

MailShield OSS の全ドキュメントの索引。初めての場合は [システム概要と前提アーキテクチャ](setup/overview.md) から読むこと。

## 読者別ガイド

| 読者 | 読む順序 |
|------|---------|
| 評価・検証したい | [overview](setup/overview.md) → [quick-start](setup/quick-start.md) |
| 本番導入したい | [overview](setup/overview.md) → [binary-install](setup/binary-install.md) / [profiles](setup/profiles.md) → [mta-self-managed](setup/mta-self-managed.md) → [mailshield-config](setup/mailshield-config.md) |
| 運用管理者 | [guide/](guide/) 一式 → [operations/troubleshooting](operations/troubleshooting.md) → [operations/backup](operations/backup.md) |
| ワーカー開発者 | [specs/workers](specs/workers.md) → [development/custom-worker-lua](development/custom-worker-lua.md) / [custom-worker-go](development/custom-worker-go.md) |
| API 連携開発者 | [specs/api-authentication](specs/api-authentication.md) → [development/api-reference](development/api-reference.md) |

## ディレクトリ構成

| ディレクトリ | 内容 |
|------------|------|
| [`setup/`](setup/) | インストール・初期設定（Docker Compose / バイナリ / MTA 連携 / ARC / アップグレード） |
| [`guide/`](guide/) | 運用ガイド（ルーティング・ポリシー・ワーカー・隔離管理） |
| [`specs/`](specs/) | 技術仕様（設定リファレンス・処理フロー・ストレージ・キュー・ログ・シグナル・認証） |
| [`development/`](development/) | 開発者向け（カスタムワーカー実装・API リファレンス・テスト） |
| [`operations/`](operations/) | 運用（トラブルシューティング・バックアップ） |
| [`decisions/`](decisions/) | ADR（Architecture Decision Records）— 設計判断の記録 |
| [`architecture.md`](architecture.md) | システム全体のアーキテクチャ概要 |

## 技術仕様（specs/）一覧

| ドキュメント | 対象 |
|------------|------|
| [configuration.md](specs/configuration.md) | mailshield.yaml / api-server.yaml / 環境変数の全設定項目 |
| [mail-processing-flow.md](specs/mail-processing-flow.md) | メール1通が辿る 7 ステップ・エラー時挙動・隔離解放フロー |
| [workers.md](specs/workers.md) | 組み込みワーカー・Lua ワーカーの実装仕様と設定 |
| [storage.md](specs/storage.md) | オブジェクトキー命名規則・ストレージバックエンド |
| [events.md](specs/events.md) | mail.received webhook（統合イベント通知）の仕様 |
| [logging.md](specs/logging.md) | 構造化ログのフォーマット・フィールド定義 |
| [signals.md](specs/signals.md) | SIGTERM / SIGINT / SIGHUP とグレースフルシャットダウン |
| [api-authentication.md](specs/api-authentication.md) | セッション Cookie / API キー認証 |
| [metrics.md](specs/metrics.md) | Prometheus 形式メトリクス・/readyz レディネスチェック |

## ドキュメント更新ポリシー

コード変更時は対応する仕様書を同一コミットで更新する。対応表は `CLAUDE.md`（ローカル）または以下を参照:

- 設定項目の変更 → `specs/configuration.md`
- 処理ステップの変更 → `specs/mail-processing-flow.md`
- ワーカーの追加・変更 → `specs/workers.md`
- ストレージ・イベント仕様の変更 → `specs/storage.md` / `specs/events.md`
- アーキテクチャ変更 → `architecture.md` と `README.md`
