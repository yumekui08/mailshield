# バックアップ・リストアガイド

MailShield のデータは MariaDB と MinIO の2箇所に保存されます。

---

## データの種類と保存場所

| データ | 保存先 | 重要度 |
|-------|-------|-------|
| メールメタデータ（件名・送受信者・ステータス） | MariaDB | 高 |
| ユーザー・メールボックス・ポリシー設定 | MariaDB | 高 |
| 監査ログ | MariaDB | 高 |
| 原本 EML / 処理済み EML | MinIO | 高 |
| 分離添付ファイル | MinIO | 高 |
| 設定ファイル | ローカルファイル（`config/`） | 高 |
| RabbitMQ キューの滞留メッセージ | RabbitMQ | 低（再配送可能） |

---

## MariaDB のバックアップ

### Docker Compose 環境

```bash
# mysqldump でフルバックアップ
docker compose -f docker/docker-compose.yml exec mariadb \
  mysqldump \
    --single-transaction \
    --routines \
    --triggers \
    -u root -p${MARIADB_ROOT_PASSWORD} \
    mailshield \
  > backup_mariadb_$(date +%Y%m%d_%H%M%S).sql

# 圧縮する場合
docker compose -f docker/docker-compose.yml exec mariadb \
  mysqldump --single-transaction -u root -p${MARIADB_ROOT_PASSWORD} mailshield \
  | gzip > backup_mariadb_$(date +%Y%m%d).sql.gz
```

### 外部 MariaDB

```bash
mysqldump -h <host> -u root -p \
  --single-transaction \
  mailshield > backup_mariadb_$(date +%Y%m%d).sql
```

### 自動化（cron）

```bash
# /etc/cron.d/mailshield-backup
0 3 * * * root docker compose -f /opt/mailshield/docker/docker-compose.yml exec -T mariadb \
  mysqldump --single-transaction -u root -pmysqlrootpassword mailshield \
  | gzip > /backup/mariadb/mailshield_$(date +\%Y\%m\%d).sql.gz
```

---

## MinIO のバックアップ

### mc（MinIO Client）を使う

```bash
# mc のインストール
# https://min.io/docs/minio/linux/reference/minio-mc.html

# エイリアス設定
mc alias set local http://localhost:9000 <access_key> <secret_key>

# バケット全体をローカルにコピー
mc mirror local/mailshield-eml /backup/eml/
mc mirror local/mailshield-attachments /backup/attachments/

# 外部 S3 にミラーリング
mc alias set s3backup s3://s3.amazonaws.com <aws_key> <aws_secret>
mc mirror local/mailshield-eml s3backup/mailshield-backup-eml/
```

### Docker 環境の MinIO データボリューム

```bash
# Docker ボリュームを直接バックアップ
docker run --rm \
  -v mailshield_minio_data:/data \
  -v /backup:/backup \
  alpine tar czf /backup/minio_$(date +%Y%m%d).tar.gz /data
```

---

## 設定ファイルのバックアップ

```bash
# config/ ディレクトリ全体をアーカイブ
tar czf config_backup_$(date +%Y%m%d).tar.gz \
  config/ \
  .env \
  docker/
```

設定ファイルは Git で管理することを推奨します（`.env` は `.gitignore` に追加）。

---

## リストア手順

### MariaDB のリストア

```bash
# 既存データを削除してリストア
docker compose -f docker/docker-compose.yml exec -T mariadb \
  mysql -u root -p${MARIADB_ROOT_PASSWORD} -e "DROP DATABASE mailshield; CREATE DATABASE mailshield;"

docker compose -f docker/docker-compose.yml exec -T mariadb \
  mysql -u root -p${MARIADB_ROOT_PASSWORD} mailshield < backup_mariadb_YYYYMMDD.sql
```

### MinIO のリストア

```bash
# ローカルバックアップからリストア
mc alias set local http://localhost:9000 <access_key> <secret_key>
mc mirror /backup/eml/ local/mailshield-eml/
mc mirror /backup/attachments/ local/mailshield-attachments/
```

---

## バックアップの確認

定期的にリストアテストを実施することを推奨します。

```bash
# MariaDB バックアップの内容確認
zcat backup_mariadb_YYYYMMDD.sql.gz | mysql -u test -p test_mailshield

# 件数確認
mysql -u test -p test_mailshield \
  -e "SELECT COUNT(*) FROM mail_messages; SELECT COUNT(*) FROM users;"
```
