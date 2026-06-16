# テストガイド

---

## ユニットテスト

### smtp-gateway

```bash
cd services/smtp-gateway

# 全テスト
go test ./...

# パッケージ単位
go test ./internal/smtp/
go test ./internal/pipeline/
go test ./internal/policy/
go test ./internal/router/
go test ./internal/worker/...

# 単一テスト関数
go test ./internal/policy/ -run TestEvaluate
go test ./internal/worker/builtin/sanitize/ -run TestSanitize

# 詳細出力
go test ./... -v
```

### api-server

```bash
cd services/api-server

go test ./...
go test ./internal/handler/ -v
go test ./internal/repository/mariadb/ -v  # 実 DB が必要（下記参照）
```

### フロントエンド（TypeScript）

```bash
cd web

# 型チェック
npx tsc --noEmit

# lint
npx eslint src/
```

---

## 統合テスト（DB あり）

MariaDB が必要なテストは `infra` プロファイルを起動してから実行します。

```bash
# infra 起動
make core-up

# MariaDB が ready になるまで待機
docker compose ps mariadb   # State が "healthy" になるまで

# 統合テスト実行（環境変数で接続先を指定）
DB_HOST=localhost DB_PORT=3306 DB_USER=mailshield DB_PASSWORD=mailshield \
  go test ./internal/repository/mariadb/ -v
```

---

## E2E テスト（メール送受信）

開発標準構成（infra + mta + dev）を起動してから実行します。

```bash
make dev-up

# 起動確認
curl http://localhost:8080/healthz
```

### Makefile ショートカット

```bash
# 受信: 通常メール（変換なし）
make e2e-normal

# 受信: ウイルス件名メール（[迷惑メール注意] プレフィックスが付くはず）
make e2e-virus

# 送信: 通常メール
make e2e-outbound-normal

# 送信: DLP 検出（マイナンバー形式の文字列）
make e2e-outbound-dlp
```

### swaks を使う場合

```bash
# 通常メール
swaks --to test@internal.test --from sender@external.test \
      --server localhost --port 25 \
      --header "Subject: Hello World" \
      --body "Normal mail"

# ウイルス件名
swaks --to test@internal.test --from sender@external.test \
      --server localhost --port 25 \
      --header "Subject: virus test mail" \
      --body "This contains virus in subject"

# EICAR テストファイル（ClamAV 検知確認）
swaks --to test@internal.test --from sender@external.test \
      --server localhost --port 25 \
      --header "Subject: EICAR test" \
      --attach - <<'EOF'
X5O!P%@AP[4\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*
EOF
```

### 結果確認

```bash
# Mailpit で受信確認
open http://localhost:8025

# MariaDB でステータス確認
docker compose exec mariadb mysql -u mailshield -pmailshield mailshield \
  -e "SELECT message_id, subject, status, direction FROM mail_messages ORDER BY created_at DESC LIMIT 5\G"

# smtp-gateway ログ確認
docker compose logs smtp-gateway --tail=50
```

---

## lint・フォーマット

```bash
# Go フォーマット確認
cd services/smtp-gateway && gofmt -l .
cd services/api-server && gofmt -l .

# Go vet
cd services/smtp-gateway && go vet ./...
cd services/api-server && go vet ./...

# Makefile ショートカット
make lint
```

---

## ビルド確認

```bash
# バイナリビルド
make build

# Docker イメージビルド
docker compose build smtp-gateway
docker compose build api-server
```
