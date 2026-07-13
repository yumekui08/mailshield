# バイナリインストール

Docker を使わず、ソースからビルドしたバイナリを systemd で常駐させる手順。インフラ（MariaDB・MinIO・Redis）は別途用意されていることを前提とする。

| 項目 | 内容 |
|------|------|
| 対象読者 | Docker を利用できない・したい環境の管理者 |
| 前提知識 | Linux サーバー管理・systemd の基本操作 |
| 構築されるもの | smtp-gateway / api-server（バイナリ）+ Web UI（静的ファイル） |

## 前提条件

### ビルド環境

| ソフトウェア | バージョン | 用途 |
|------------|-----------|------|
| Go | 1.24 以上 | smtp-gateway / api-server のビルド |
| Node.js + npm | 20 以上 | Web UI のビルド（Web UI を使う場合のみ） |
| GNU Make / Git | — | ビルド・取得 |

### 実行環境（インフラ）

| コンポーネント | バージョン | 必須 / オプション |
|--------------|-----------|-----------------|
| MariaDB | 11.x | **必須** |
| MinIO または S3 互換ストレージ | — | オプション（`storage.backend: filesystem` で代替可） |
| Redis | 7 以上 | オプション（`redis.backend: mariadb` で省略可・api-server 用） |

> [!NOTE]
> 最小構成は **MariaDB のみ**。ストレージはローカルファイルシステム、キューは無効、api-server のセッションは MariaDB に保存する構成にすれば外部インフラは MariaDB だけで動作する。

## 1. ビルド

```bash
git clone https://github.com/koizumib/mailshield.git
cd mailshield

# smtp-gateway と api-server をビルドする（go.work ワークスペース使用）
make build

# Web UI（api-server を使う場合のみ）
cd web && npm install && npm run build && cd ..
```

成果物:

```
dist/
├── smtp-gateway
└── api-server
web/dist/            # 静的ファイル（Nginx 等で配信する）
```

> [!NOTE]
> 別マシン向けにビルドする場合は `make build-linux`（Linux amd64）または `make dist`（linux/darwin × amd64/arm64 全組み合わせ）を使う。

## 2. 配置

推奨レイアウト（以降の手順はこのレイアウトを前提とする）:

```
/opt/mailshield/
├── bin/
│   ├── smtp-gateway
│   └── api-server
├── config/              # リポジトリの config/ 一式をコピー
│   ├── mailshield.yaml
│   ├── mailshield.default.yaml
│   ├── api-server.yaml
│   ├── routes.d/
│   └── workers/
├── workers/             # Lua ワーカースクリプト
├── data/                # storage.backend: filesystem の場合の EML 保存先
└── .env                 # 環境変数（パスワード類）
```

```bash
# 専用ユーザーの作成
sudo useradd --system --home-dir /opt/mailshield --shell /usr/sbin/nologin mailshield

# 配置
sudo mkdir -p /opt/mailshield/bin
sudo cp dist/smtp-gateway dist/api-server /opt/mailshield/bin/
sudo cp -r config workers /opt/mailshield/
sudo mkdir -p /opt/mailshield/data
sudo chown -R mailshield:mailshield /opt/mailshield
sudo chmod 600 /opt/mailshield/.env 2>/dev/null || true
```

## 3. インフラの準備

### 3.1 MariaDB スキーマの適用

```bash
# データベースとユーザーを作成する
mysql -h <mariadb-host> -u root -p <<'SQL'
CREATE DATABASE IF NOT EXISTS mailshield CHARACTER SET utf8mb4;
CREATE USER IF NOT EXISTS 'mailshield'@'%' IDENTIFIED BY '<db-password>';
GRANT ALL PRIVILEGES ON mailshield.* TO 'mailshield'@'%';
SQL

# スキーマを番号順にすべて適用する
mysql -h <mariadb-host> -u root -p mailshield < schema/mariadb/001_schema.sql
mysql -h <mariadb-host> -u root -p mailshield < schema/mariadb/002_seed.sql
```

### 3.2 MinIO のバケット作成（storage.backend: minio / s3 の場合）

```bash
mc alias set mailshield http://<minio-host>:9000 <access-key> <secret-key>
mc mb mailshield/mailshield-eml
mc mb mailshield/mailshield-attachments
```

> [!IMPORTANT]
> smtp-gateway はバケットを自動作成しない（存在確認のみ）。バケットが存在しないと EML 保存に失敗し、メールは 451 で MTA のキューに残る。

### smtp-gateway

```ini
# /etc/systemd/system/smtp-gateway.service
[Unit]
Description=MailShield smtp-gateway
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=mailshield
Group=mailshield
WorkingDirectory=/opt/mailshield
EnvironmentFile=/opt/mailshield/.env
ExecStart=/opt/mailshield/bin/smtp-gateway
Restart=on-failure
RestartSec=5s
# グレースフルシャットダウン（処理中メールの完了待ち）に十分な時間を確保する
TimeoutStopSec=40
NoNewPrivileges=true
ProtectSystem=full
ReadWritePaths=/opt/mailshield/data

[Install]
WantedBy=multi-user.target
```

### api-server（Web UI を使う場合）

```ini
# /etc/systemd/system/mailshield-api.service
[Unit]
Description=MailShield api-server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=mailshield
Group=mailshield
WorkingDirectory=/opt/mailshield
EnvironmentFile=/opt/mailshield/.env
ExecStart=/opt/mailshield/bin/api-server
Restart=on-failure
RestartSec=5s
TimeoutStopSec=40
NoNewPrivileges=true
ProtectSystem=full

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now smtp-gateway
sudo systemctl enable --now mailshield-api    # api-server を使う場合

# 状態確認
systemctl status smtp-gateway
journalctl -u smtp-gateway -f
```

### Web UI の配信（Nginx の例）

`web/dist/` の静的ファイルを配信し、`/api/` を api-server にプロキシする。

```nginx
server {
    listen 3000;
    root /opt/mailshield/web;      # web/dist/ をコピーした場所
    index index.html;

    location /api/ {
        proxy_pass http://127.0.0.1:8090;
        proxy_set_header Host $host;
    }

    location / {
        try_files $uri /index.html;   # SPA ルーティング
    }
}
```

## 7. 最終確認

```bash
# ヘルスチェック
curl http://localhost:8080/healthz    # smtp-gateway → ok
curl http://localhost:8090/healthz    # api-server → ok（使用する場合）

# 受信 MTA からのテストメール（MTA 設定完了後）
swaks --to test@<your-domain> --from sender@external.example \
      --server <mta-host> --port 25 \
      --header "Subject: MailShield binary install test"
```

シグナルの動作（`SIGTERM` / `SIGINT` / `SIGHUP` いずれもグレースフルシャットダウン）は [シグナルハンドリング仕様](../specs/signals.md) を参照。

## アンインストール

```bash
sudo systemctl disable --now smtp-gateway mailshield-api
sudo rm /etc/systemd/system/smtp-gateway.service /etc/systemd/system/mailshield-api.service
sudo systemctl daemon-reload
sudo rm -rf /opt/mailshield          # データ（data/）も消えるため必要ならバックアップする
sudo userdel mailshield
```

MariaDB のデータベース・MinIO のバケットは必要に応じて手動で削除する。

## 次のステップ

- [MTA との連携](./mta-self-managed.md) — 受信 MTA（Postfix + Rspamd）の設定
- [MailShield 設定ガイド](./mailshield-config.md) — ルート・ポリシー・通知の詳細設定
- [アップグレード](./upgrade.md) — バージョンアップ手順
