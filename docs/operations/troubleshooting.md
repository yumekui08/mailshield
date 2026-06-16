# トラブルシューティング

---

## メールが届かない

### 確認ステップ

```bash
# 1. smtp-gateway が起動しているか
curl http://localhost:8080/healthz

# 2. ログでエラーを確認
docker compose logs smtp-gateway --tail=100
docker compose logs postfix --tail=50

# 3. Postfix キューを確認
docker compose exec postfix mailq

# 4. MariaDB のメールステータスを確認
docker compose exec mariadb mysql -u mailshield -pmailshield mailshield \
  -e "SELECT message_id, subject, status, error_message FROM mail_messages ORDER BY created_at DESC LIMIT 10\G"
```

### よくある原因

**`trusted_sources` に Postfix が含まれていない**

```
smtp-gateway ログ: "接続元が信頼リストに含まれていません"
```

`config/mailshield.yaml` の `server.trusted_sources` に Postfix のホスト名または IP を追加してください。

```yaml
server:
  trusted_sources:
    - postfix
    - 10.0.0.5    # Postfix の IP
```

---

**`relay_domains` に受信ドメインが含まれていない**

```
Postfix ログ: "Relay access denied"
```

`.env` の `MAILSHIELD_RELAY_DOMAINS` を確認してください。

---

**ポリシーにマッチするルールがない**

```
smtp-gateway ログ: "マッチするルールがありません（デフォルト配送なし）"
```

`config/policy-inbound.yaml` の最後に `condition: "true"` のフォールバックルールを追加してください。

```yaml
  - name: default_deliver
    condition: "true"
    action: deliver
    destination: "postfix:10026"
```

---

**MinIO への接続失敗**

```
smtp-gateway ログ: "MinIO 保存失敗"
```

smtp-gateway は MinIO 保存失敗時に `451 Try again later` を返し、Postfix キューに残します。
MinIO の起動状態と認証情報を確認してください。

```bash
docker compose ps minio
curl http://localhost:9000/minio/health/live
```

---

## 隔離解放後もメールが届かない

```bash
# api-server ログで解放処理を確認
docker compose logs api-server | grep "quarantine"

# 解放先（SMTP 配送先）の設定を確認
# policy-inbound.yaml の deliver.destination が正しいか
```

通知メール設定（`notification.smtp_host`）と、再配送先の Postfix 再インジェクトポート（10026）が
正しいことを確認してください。

---

## ClamAV がウイルスを検知しない

```bash
# ClamAV の起動確認
docker compose ps clamav
docker compose logs clamav --tail=30

# EICAR テストファイルで動作確認
swaks --to test@example.com --from sender@example.com \
  --server localhost --port 25 \
  --header "Subject: EICAR Test" \
  --attach-type application/octet-stream \
  --attach <(echo 'X5O!P%@AP[4\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*')
```

ClamAV はウイルス定義ファイルのダウンロードに数分かかります。
初回起動直後は定義ファイルが更新されるまで待ってください。

---

## Rspamd の Authentication-Results ヘッダが付かない

```bash
# Rspamd の起動確認
docker compose exec rspamd rspamadm control stat

# milter_headers の設定確認
cat infra/rspamd/local.d/milter_headers.conf
# skip_local = false であること

# テストメールで AR ヘッダを確認（Mailpit または EML ダウンロード）
```

---

## api-server にアクセスできない

```bash
# api-server の起動確認
curl http://localhost:8081/healthz

# ログでエラーを確認
docker compose logs api-server --tail=50

# Redis の起動確認（セッション管理に使用）
docker compose exec redis redis-cli ping
```

---

## ログの見方

```bash
# JSON ログを jq で整形（レベルでフィルタ）
docker compose logs smtp-gateway -f | jq 'select(.level=="ERROR")'

# message_id で追跡
docker compose logs smtp-gateway | jq 'select(.message_id=="<uuid>")'

# ワーカーのログ
docker compose logs smtp-gateway | jq 'select(.worker != null)'
```

---

## デバッグモードの有効化

```yaml
# config/mailshield.yaml
log:
  level: debug
```

デバッグモードでは各ワーカーの詳細な処理ログが出力されます。
本番環境での常時有効化は推奨しません。

---

## よくあるエラーメッセージ

| メッセージ | 原因 | 対処 |
|-----------|------|------|
| `接続元が信頼リストに含まれていません` | trusted_sources 未設定 | mailshield.yaml に追加 |
| `マッチするルールがありません` | policy.yaml のフォールバックなし | `condition: "true"` を追加 |
| `MinIO 保存失敗` | MinIO 接続エラー | MinIO の起動・認証情報を確認 |
| `RabbitMQ 発行失敗` | RabbitMQ 接続エラー | 続行（メールは処理される）。RabbitMQ を確認 |
| `ワーカータイムアウト` | 外部サービスが遅い | timeout_seconds を増やすかワーカーを無効化 |
| `Relay access denied` | relay_domains 未設定 | .env の MAILSHIELD_RELAY_DOMAINS を確認 |
