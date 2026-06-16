# Docker Compose プロファイル

MailShield は単一の `docker-compose.yml` に全コンポーネントを定義し、
`COMPOSE_PROFILES` 環境変数で起動するコンポーネントを選択する構成です。

---

## プロファイル一覧

| プロファイル | 含むサービス | 用途 |
|------------|------------|------|
| _(なし)_ | smtp-gateway | 常時起動。他のプロファイルと組み合わせる |
| `infra` | MariaDB, RabbitMQ, MinIO, Redis | 同梱インフラ。外部サービスを使う場合は不要 |
| `scanners` | ClamAV, Apache Tika, Tesseract | ウイルス検査・DLP・QRコード検査 |
| `dev` | Mailpit + 開発用 MTA（Postfix + Rspamd） | 開発・動作確認専用 |
| `api` | api-server, Web UI | 管理画面・REST API |

> **dev プロファイルの MTA について**: `dev` プロファイルに含まれる Postfix と Rspamd は
> **開発・動作確認用**です。設定ファイルは `examples/mta/` に置かれています。
> 本番環境では自前の MTA を使い、`dev` プロファイルは起動しないでください。

---

## 典型的な起動パターン

### 開発環境

```bash
make dev-up
# = COMPOSE_PROFILES=infra,dev docker compose up -d
# smtp-gateway + インフラ + 開発用Postfix + Rspamd + Mailpit
```

### 開発環境（スキャナー有効）

```bash
COMPOSE_PROFILES=infra,scanners,dev docker compose up -d
```

ClamAV, Apache Tika, Tesseract も起動します。av-worker / dlp-worker / qr-worker が実際に動作します。

### コアのみ（自前の MTA と組み合わせる）

```bash
make core-up
# = COMPOSE_PROFILES=infra docker compose up -d
# smtp-gateway + インフラのみ
# 自前の MTA から port 10025 に転送するよう設定すること
```

### API・Web UI 込み

```bash
make api-up
# = COMPOSE_PROFILES=infra,dev,api docker compose up -d
```

---

## Makefile ショートカット

```bash
make core-up       # COMPOSE_PROFILES=infra
make dev-up        # COMPOSE_PROFILES=infra,dev
make scanners-up   # COMPOSE_PROFILES=infra,outbound,scanners,dev
make api-up        # COMPOSE_PROFILES=infra,dev,api
make dev-down      # 停止
```

---

## 外部サービスへの切り替え

`.env` の接続先を変更するだけでよい。コードの変更は不要。

```dotenv
# 外部 MariaDB を使う（infra profile の mariadb の代わり）
DB_HOST=your-mariadb-host.example.com
DB_PORT=3306

# 外部 S3 を使う
STORAGE_BACKEND=s3
MINIO_ENDPOINT=https://s3.amazonaws.com
AWS_REGION=ap-northeast-1

# 外部 RabbitMQ を使う
RABBITMQ_HOST=your-rabbitmq.example.com
RABBITMQ_PORT=5672
```

---

## ポート一覧

| ポート | サービス | 説明 |
|-------|---------|------|
| 25 | postfix（dev profile） | SMTP 受信（開発用） |
| 587 | postfix-submission（dev profile） | SMTP submission（開発用） |
| 10025 | smtp-gateway | コンテンツフィルター SMTP（MTA からの接続先） |
| 8080 | smtp-gateway | ヘルスチェック |
| 8081 | api-server | REST API |
| 8025 | mailpit（dev profile） | Web UI（開発用メール確認） |
| 9000 | minio | MinIO S3 API |
| 9001 | minio | MinIO コンソール（開発時） |
| 15672 | rabbitmq | RabbitMQ 管理UI（開発時） |
