# アップグレード手順

---

## 基本的な流れ

```
1. 変更点の確認（CHANGELOG.md）
2. バックアップ（MariaDB + MinIO）
3. DB マイグレーション適用
4. コンテナ / バイナリの更新
5. 動作確認
```

---

## 1. 変更点の確認

アップグレード前に CHANGELOG.md の「Breaking Changes」セクションを必ず確認してください。
設定ファイルの変更が必要な場合は記載されています。

---

## 2. バックアップ

**必須。** アップグレード前に必ずバックアップを取得してください。

### MariaDB のバックアップ

```bash
# Docker の場合
docker compose -f docker/docker-compose.yml exec mariadb \
  mysqldump -u root -p${MARIADB_ROOT_PASSWORD} mailshield > backup_$(date +%Y%m%d).sql

# 外部 MariaDB の場合
mysqldump -h <host> -u root -p mailshield > backup_$(date +%Y%m%d).sql
```

### MinIO のバックアップ

```bash
# mc sync で別バケット / ローカルにコピー
mc mirror mailshield/mailshield-eml /backup/eml/
mc mirror mailshield/mailshield-attachments /backup/attachments/
```

---

## 3. DB マイグレーション

`schema/mariadb/` に新しい SQL ファイルが追加されている場合は適用します。

```bash
# 適用済みのマイグレーション番号を確認
docker compose -f docker/docker-compose.yml exec mariadb mysql -u mailshield -p mailshield \
  -e "SHOW TABLES;"

# 番号順に未適用のファイルを適用
docker compose -f docker/docker-compose.yml exec -T mariadb \
  mysql -u mailshield -p mailshield < schema/mariadb/007_xxxx.sql
```

> **注意:** `schema/mariadb/` のファイルはコンテナ初回起動時のみ自動実行されます。
> アップグレード時は **手動で** 適用してください。

---

## 4. コンテナの更新

### Docker Compose の場合

```bash
# イメージを再ビルド
docker compose -f docker/docker-compose.yml build smtp-gateway api-server

# サービスを順番に再起動（ダウンタイムを最小化）

# 1. api-server を先に停止（受付を止める）
docker compose -f docker/docker-compose.yml stop api-server

# 2. smtp-gateway をグレースフルに再起動
# SIGTERM を送ると処理中のメールを完了してから停止する
docker compose -f docker/docker-compose.yml stop smtp-gateway
docker compose -f docker/docker-compose.yml up -d smtp-gateway

# 3. api-server を再起動
docker compose -f docker/docker-compose.yml up -d api-server
```

### バイナリの場合

```bash
# バイナリを再ビルド
make build
# または個別に:
# cd services/smtp-gateway && go build -o ../../dist/smtp-gateway ./cmd/server/
# cd services/api-server && go build -o ../../dist/api-server ./cmd/server/

# systemd でリスタート
systemctl restart smtp-gateway
systemctl restart api-server
```

---

## 5. 動作確認

```bash
# ヘルスチェック
curl http://localhost:8080/healthz   # smtp-gateway
curl http://localhost:8081/healthz   # api-server

# テストメール送信
make e2e-normal

# ログ確認
docker compose -f docker/docker-compose.yml logs smtp-gateway --tail=50
docker compose -f docker/docker-compose.yml logs api-server --tail=50
```

---

## ロールバック

問題が発生した場合は以下の手順でロールバックします。

```bash
# 1. 更新したコンテナを停止
docker compose -f docker/docker-compose.yml stop smtp-gateway api-server

# 2. DB をバックアップから復元
docker compose -f docker/docker-compose.yml exec -T mariadb \
  mysql -u root -p${MARIADB_ROOT_PASSWORD} mailshield < backup_YYYYMMDD.sql

# 3. 前バージョンのイメージで起動
# （事前にタグを付けておくか、git checkout で前バージョンに戻す）
git checkout <前バージョンのタグ>
docker compose -f docker/docker-compose.yml build smtp-gateway api-server
docker compose -f docker/docker-compose.yml up -d smtp-gateway api-server
```

---

## 設定ファイルの変更

バージョンによっては `mailshield.yaml` に新しいセクションの追加が必要な場合があります。
[設定リファレンス](../specs/configuration.md) と `config/mailshield.yaml`（サンプル）を参照して差分を確認してください。
