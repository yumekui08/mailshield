# バイナリ・ソースビルドによるインストール

Docker を使わずに直接バイナリを実行する方法を説明します。
インフラ（MariaDB・RabbitMQ・MinIO・Redis）は別途用意してください。

---

## 前提条件

- Go 1.24 以上（smtp-gateway / api-server のビルド）
- Node.js 20 以上 + npm（Web UI のビルド）
- MariaDB 11.x
- RabbitMQ 3.13 以上
- MinIO（または S3 互換ストレージ）
- Redis 7 以上

---

## ソースからビルド

```bash
git clone https://github.com/koizumib/mailshield.git
cd mailshield

# ビルド（go.work ワークスペース使用）
make build

# または個別に
cd services/smtp-gateway && go build -o ../../dist/smtp-gateway ./cmd/server/ && cd ../..
cd services/api-server  && go build -o ../../dist/api-server  ./cmd/server/ && cd ../..

# Web UI
cd web && npm install && npm run build && cd ..
```

ビルド成果物:

```
dist/
├── smtp-gateway
└── api-server
web/dist/            ← 静的ファイル（Nginx 等で配信）
```

---

## DB スキーマの適用

```bash
# MariaDB に接続してスキーマを適用（番号順にすべて適用すること）
mysql -h <host> -u root -p mailshield < schema/mariadb/001_schema.sql
mysql -h <host> -u root -p mailshield < schema/mariadb/002_seed.sql
```

---

## RabbitMQ のセットアップ

```bash
# 管理 UI から definitions.json をインポート（推奨）
# またはコマンドで:
rabbitmqadmin import docker/infra/rabbitmq/definitions.json
```

---

## MinIO のバケット初期化

```bash
# MinIO クライアント（mc）でバケットを作成
mc alias set mailshield http://<minio-host>:9000 <access_key> <secret_key>
mc mb mailshield/mailshield-eml
mc mb mailshield/mailshield-attachments
```

---

## 設定ファイルの編集

`config/mailshield.yaml` をコピーして編集します。

```yaml
server:
  smtp_port: 10024
  trusted_sources:
    - <postfix-hostname-or-ip>

storage:
  endpoint: <minio-host>:9000
  bucket_eml: mailshield-eml
  bucket_attachments: mailshield-attachments

database:
  host: <mariadb-host>
  port: 3306
  name: mailshield

queue:
  host: <rabbitmq-host>
  port: 5672
```

---

## 起動

### smtp-gateway

```bash
export CONFIG_FILE=/path/to/config/mailshield.yaml
export DB_HOST=<mariadb-host>
export DB_PASSWORD=<password>
export MINIO_ACCESS_KEY=<key>
export MINIO_SECRET_KEY=<secret>
export RABBITMQ_USER=mailshield
export RABBITMQ_PASSWORD=<password>

./dist/smtp-gateway
```

### api-server

```bash
export DB_HOST=<mariadb-host>
export DB_PASSWORD=<password>
export MINIO_ACCESS_KEY=<key>
export MINIO_SECRET_KEY=<secret>
export REDIS_HOST=<redis-host>
export SESSION_SECRET=<32文字以上のランダム文字列>

./dist/api-server
```

---

## systemd での常駐化

```ini
# /etc/systemd/system/smtp-gateway.service
[Unit]
Description=MailShield smtp-gateway
After=network.target

[Service]
Type=simple
User=mailshield
WorkingDirectory=/opt/mailshield
EnvironmentFile=/opt/mailshield/.env
ExecStart=/opt/mailshield/dist/smtp-gateway
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
```

```bash
systemctl enable --now smtp-gateway
systemctl enable --now api-server
```

---

## シグナル

| シグナル | 動作 |
|---------|------|
| `SIGTERM` / `SIGINT` | グレースフルシャットダウン（処理中のメールを完了してから停止） |
| `SIGHUP` | 設定ファイルのリロード（対応状況は [signals.md](../specs/signals.md) を参照） |
