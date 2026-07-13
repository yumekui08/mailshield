# Docker Compose プロファイル

MailShield は単一の `docker/docker-compose.yml` に全コンポーネントを定義し、`COMPOSE_PROFILES` 環境変数で起動するコンポーネントを選択する。本ページではプロファイルの構成と典型的な起動パターンを説明する。

| 項目 | 内容 |
|------|------|
| 対象読者 | Docker Compose 構成で起動コンポーネントを調整したい管理者 |
| 前提 | [クイックスタート](./quick-start.md) または [MailShield 設定ガイド](./mailshield-config.md) の初期設定が完了していること |

## プロファイル一覧

| プロファイル | 含まれるサービス | 用途 |
|------------|----------------|------|
| _(なし・常時起動)_ | smtp-gateway + MariaDB | MariaDB は唯一の必須サービス |
| `storage` | MinIO + minio-init | EML をオブジェクトストレージに保存する場合 |
| `scanners` | ClamAV + Apache Tika + Tesseract | ウイルス検査・DLP・QR コード検査 |
| `dev` | Mailpit | 処理後メールをキャッチして確認する場合 |
| `api` | api-server + Web UI + Redis | 管理画面・REST API |

> [!NOTE]
> `storage` プロファイルは省略できる。その場合は `config/mailshield.yaml` で
> `storage.backend: filesystem`（MinIO 不要）を設定すること。

> [!IMPORTANT]
> **MTA は同梱されない。** 受信 MTA は自前で用意し、content_filter を `smtp-gateway:10024` に向けること。詳細は [MTA との連携](./mta-self-managed.md) を参照。

## Makefile ターゲット

| ターゲット | プロファイル | 起動されるサービス |
|-----------|-------------|------------------|
| `make core-up` | _(なし)_ | smtp-gateway + MariaDB のみ（最小構成） |
| `make dev-up` | `storage,dev` | + MinIO + Mailpit（開発標準） |
| `make scanners-up` | `storage,dev,scanners` | + ClamAV / Tika / Tesseract |
| `make api-up` | `storage,dev,api` | + api-server + Web UI + Redis |
| `make full-up` | `storage,dev,scanners,api` | 全サービス |
| `make dev-down` ほか `*-down` | — | 対応する構成の停止 |
| `make docker-clean` | — | 全ボリューム削除（初期化のやり直し） |

Makefile を使わない場合は `COMPOSE_PROFILES` を直接指定する。

```bash
COMPOSE_PROFILES=storage,dev,scanners \
  docker compose -f docker/docker-compose.yml up -d
```

> [!NOTE]
> ClamAV は初回起動時にウイルス定義 DB（約 300MB）をダウンロードするため、healthy になるまで数分かかる（`start_period: 180s`）。

## 起動パターンの選び方

| 状況 | 推奨ターゲット |
|------|--------------|
| 最小リソースで本番運用（filesystem + キューなし） | `make core-up` |
| 開発・評価（処理後メールを Mailpit で確認） | `make dev-up` |
| ウイルス検査・DLP を含めて検証 | `make scanners-up` |
| 管理 Web UI・隔離管理を使う | `make api-up` |
| 全機能 | `make full-up` |

## 外部サービスへの切り替え

同梱インフラの代わりに外部サービスを使う場合は、`.env` の接続先を変更して該当プロファイルを外すだけでよい。コードの変更は不要。

```dotenv
# 外部 MariaDB（同梱 mariadb コンテナの代わり）
DB_HOST=your-mariadb-host.example.com
DB_PORT=3306

# 外部 S3（storage プロファイルの MinIO の代わり）
MINIO_ENDPOINT=s3.amazonaws.com
MINIO_ACCESS_KEY=AKIAXXXXXXXXXXXXXXXX
MINIO_SECRET_KEY=your-secret-key
MINIO_USE_SSL=true

# 外部 Redis（api プロファイルの redis コンテナの代わり）
REDIS_HOST=your-redis.example.com
REDIS_PORT=6379
```

対応する設定キーの詳細は [設定リファレンス](../specs/configuration.md) を参照。

## ポート一覧

| ポート | サービス | プロファイル | 説明 |
|-------|---------|-------------|------|
| 10024 | smtp-gateway | _(常時)_ | コンテンツフィルター SMTP（受信 MTA の content_filter 送信先） |
| 8080 | smtp-gateway | _(常時)_ | ヘルスチェック・`/simulate` |
| 8090 | api-server | `api` | REST API |
| 3000 | web | `api` | 管理 Web UI |
| 8025 | mailpit | `dev` | 開発用メール確認 UI |
| 9000 / 9001 | minio | `storage` | S3 API / 管理コンソール |

## 関連ドキュメント

- [MailShield 設定ガイド](./mailshield-config.md) — 初期設定の全手順
- [設定リファレンス](../specs/configuration.md) — 全設定キーと環境変数
