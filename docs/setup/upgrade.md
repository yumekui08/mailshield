# アップグレード

MailShield を新しいバージョンへ更新する手順。Docker Compose 構成・バイナリ構成の両方を扱う。

| 項目 | 内容 |
|------|------|
| 対象読者 | 稼働中の MailShield を管理している運用者 |
| 前提 | バックアップ先のディスク容量があること |
| ダウンタイム | smtp-gateway 再起動中のメールは受信 MTA のキューに滞留し、復帰後に自動再送される（消失しない） |

## アップグレードの流れ

```
1. 変更点の確認（CHANGELOG.md の Breaking Changes）
2. バックアップ（MariaDB + オブジェクトストレージ）
3. DB マイグレーションの適用
4. コンテナ / バイナリの更新
5. 動作確認
```

> [!IMPORTANT]
> 手順 2 のバックアップは必須。ロールバックはバックアップがあることを前提とする。

## 1. 変更点の確認

アップグレード前に `CHANGELOG.md` の **Breaking Changes** セクションを確認する。設定ファイルの変更（キーの追加・改名・削除）が必要な場合はここに記載される。

設定差分の確認には [設定リファレンス](../specs/configuration.md) と `config/mailshield.default.yaml`（全パラメータのデフォルト値）を使う。

## 2. バックアップ

### MariaDB

```bash
# Docker Compose 構成の場合
docker compose -f docker/docker-compose.yml exec mariadb \
  mysqldump -u root -p"${MARIADB_ROOT_PASSWORD}" mailshield > backup_$(date +%Y%m%d).sql

# 外部 MariaDB の場合
mysqldump -h <mariadb-host> -u root -p mailshield > backup_$(date +%Y%m%d).sql
```

### オブジェクトストレージ

```bash
# MinIO / S3 の場合（mc mirror でローカルへコピー）
mc mirror mailshield/mailshield-eml /backup/eml/
mc mirror mailshield/mailshield-attachments /backup/attachments/

# filesystem バックエンドの場合
tar czf backup_eml_$(date +%Y%m%d).tar.gz <storage.local_dir のパス>
```

詳細は [バックアップ・リストア](../operations/backup.md) を参照。

## 3. DB マイグレーションの適用

`schema/mariadb/` に新しい番号の SQL ファイルが追加されている場合は、**番号順に手動で**適用する。

```bash
# 例: 007 が新規追加されていた場合
docker compose -f docker/docker-compose.yml exec -T mariadb \
  mysql -u mailshield -p"${DB_PASSWORD}" mailshield < schema/mariadb/007_xxxx.sql
```

> [!WARNING]
> `schema/mariadb/` の SQL はコンテナの**初回起動時のみ**自動実行される。既存環境のアップグレードでは自動適用されないため、必ず手動で適用すること。

## 4. 本体の更新

### Docker Compose 構成

```bash
# 新バージョンを取得してイメージを再ビルド
git pull
docker compose -f docker/docker-compose.yml build smtp-gateway api-server

# ダウンタイムを最小化する再起動順序:
# (1) api-server を停止（管理操作の受付を止める）
docker compose -f docker/docker-compose.yml stop api-server

# (2) smtp-gateway をグレースフル再起動
#     SIGTERM 受信後、処理中のメールを完了してから停止する
docker compose -f docker/docker-compose.yml stop smtp-gateway
docker compose -f docker/docker-compose.yml up -d smtp-gateway

# (3) api-server を再起動
docker compose -f docker/docker-compose.yml up -d api-server
```

> [!NOTE]
> `docker compose stop` のデフォルトタイムアウトは 10 秒。`shutdown_timeout_seconds`（デフォルト 30 秒）より短いため、`--timeout 35` の指定を推奨する。

### バイナリ構成

```bash
git pull
make build
sudo cp dist/smtp-gateway dist/api-server /opt/mailshield/bin/
sudo systemctl restart smtp-gateway
sudo systemctl restart mailshield-api
```

## 5. 動作確認

```bash
# ヘルスチェック
curl http://localhost:8080/healthz    # smtp-gateway → ok
curl http://localhost:8090/healthz    # api-server → ok

# テストメール送信（開発環境の場合）
make e2e-normal

# エラーログの確認
docker compose -f docker/docker-compose.yml logs smtp-gateway --tail=50
docker compose -f docker/docker-compose.yml logs api-server --tail=50
```

確認項目:

- [ ] ヘルスチェックが `ok` を返す
- [ ] 起動ログに ERROR がない（`ルート初期化完了` と `smtp-gateway 起動完了` が出力されている）
- [ ] テストメールが配送される
- [ ] Web UI にログインできる（api-server を使う場合）

## ロールバック

```bash
# 1. 更新したサービスを停止する
docker compose -f docker/docker-compose.yml stop smtp-gateway api-server

# 2. DB をバックアップから復元する
docker compose -f docker/docker-compose.yml exec -T mariadb \
  mysql -u root -p"${MARIADB_ROOT_PASSWORD}" mailshield < backup_YYYYMMDD.sql

# 3. 前バージョンに戻してビルド・起動する
git checkout <前バージョンのタグ>
docker compose -f docker/docker-compose.yml build smtp-gateway api-server
docker compose -f docker/docker-compose.yml up -d smtp-gateway api-server
```

> [!NOTE]
> ロールバックを迅速に行えるよう、アップグレード前のバージョンをタグまたはコミットハッシュで控えておくこと。

## 関連ドキュメント

- [バックアップ・リストア](../operations/backup.md)
- [トラブルシューティング](../operations/troubleshooting.md)
- [シグナルハンドリング仕様](../specs/signals.md) — グレースフルシャットダウンの詳細
