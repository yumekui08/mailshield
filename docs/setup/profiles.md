# Docker Compose プロファイル

MailShield は単一の `docker-compose.yml` に全コンポーネントを定義し、
`COMPOSE_PROFILES` 環境変数で起動するコンポーネントを選択する構成です。

---

## プロファイル一覧

| プロファイル | 含むサービス | 用途 |
|------------|------------|------|
| _(なし)_ | smtp-gateway + MariaDB | 常時起動。MariaDB は唯一の必須サービス |
| `queue` | RabbitMQ | `mail.received` を外部システムへ通知する場合 |
| `storage` | MinIO | EML をオブジェクトストレージに保存する場合 |
| `dev` | Postfix + Rspamd + Mailpit | 開発・動作確認専用 |
| `scanners` | ClamAV, Apache Tika, Tesseract | ウイルス検査・DLP・QRコード検査 |
| `api` | api-server + Web UI + Redis | 管理画面・REST API |

> **dev プロファイルの MTA について**: `dev` プロファイルに含まれる Postfix と Rspamd は
> **開発・動作確認用**です。設定ファイルは `examples/mta/` に置かれています。
> 本番環境では自前の MTA を使い、`dev` プロファイルは起動しないでください。

> **queue / storage の省略について**: `queue.backend = none`（RabbitMQ 不要）、
> `storage.backend = filesystem`（MinIO 不要）に設定することで、それぞれのプロファイルを
> 省略した最小構成で起動できます。

---

## 典型的な起動パターン

### 開発環境

```bash
make dev-up
# = COMPOSE_PROFILES=storage,queue,dev docker compose up -d
# smtp-gateway + MariaDB + MinIO + RabbitMQ + 開発用Postfix + Rspamd + Mailpit
```

### 開発環境（スキャナー有効）

```bash
COMPOSE_PROFILES=storage,queue,dev,scanners docker compose up -d
```

ClamAV, Apache Tika, Tesseract も起動します。av-worker / dlp-worker / qr-worker が実際に動作します。

### コアのみ（自前の MTA と組み合わせる・MinIO/RabbitMQ 不要）

```bash
make core-up
# = docker compose up -d（追加プロファイルなし）
# smtp-gateway + MariaDB のみ
# mailshield.yaml で storage.backend=filesystem, queue.backend=none に設定すること
# 自前の MTA から port 10024 に転送するよう設定すること
```

### API・Web UI 込み

```bash
make api-up
# = COMPOSE_PROFILES=storage,queue,dev,api docker compose up -d
```

---

## Makefile ショートカット

```bash
make core-up       # COMPOSE_PROFILES=（なし）  smtp-gateway + MariaDB のみ
make dev-up        # COMPOSE_PROFILES=storage,queue,dev
make scanners-up   # COMPOSE_PROFILES=storage,queue,dev,scanners
make api-up        # COMPOSE_PROFILES=storage,queue,dev,api
make dev-down      # 停止
```

---

## 外部サービスへの切り替え

`.env` の接続先を変更するだけでよい。コードの変更は不要。

```dotenv
# 外部 MariaDB を使う（同梱の MariaDB の代わり）
DB_HOST=your-mariadb-host.example.com
DB_PORT=3306

# 外部 S3 を使う（storage プロファイルの MinIO の代わり）
STORAGE_BACKEND=s3
MINIO_ENDPOINT=https://s3.amazonaws.com
AWS_REGION=ap-northeast-1

# 外部 RabbitMQ を使う（queue プロファイルの RabbitMQ の代わり）
RABBITMQ_HOST=your-rabbitmq.example.com
RABBITMQ_PORT=5672
```

---

## ポート一覧

| ポート | サービス | 説明 |
|-------|---------|------|
| 25 | postfix（dev profile） | SMTP 受信（開発用） |
| 587 | postfix-submission（dev profile） | SMTP submission（開発用） |
| 10024 | smtp-gateway | コンテンツフィルター SMTP（MTA からの接続先） |
| 8080 | smtp-gateway | ヘルスチェック |
| 8090 | api-server（api profile） | REST API |
| 3000 | web（api profile） | Web UI |
| 8025 | mailpit（dev profile） | Web UI（開発用メール確認） |
| 9000 | minio（storage profile） | MinIO S3 API |
| 9001 | minio（storage profile） | MinIO コンソール（開発時） |
| 15672 | rabbitmq（queue profile） | RabbitMQ 管理UI（開発時） |
