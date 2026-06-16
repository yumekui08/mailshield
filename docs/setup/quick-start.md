# クイックスタート（Docker）

このガイドでは Docker Compose を使って MailShield を最短で起動する手順を説明します。

---

## 前提条件

- Docker Engine 24.0 以上
- Docker Compose v2.20 以上
- ポート 8080, 8081 が空いていること（開発環境では追加で 25, 587, 8025）

---

## 手順

### 1. リポジトリをクローン

```bash
git clone https://github.com/koizumib/mailshield.git
cd mailshield
```

### 2. 環境変数ファイルを作成

```bash
cp .env.example .env
```

`.env` を開いて、最低限以下の項目を設定してください。

```dotenv
# 配送先 MTA の SMTP アドレス（例: 本番環境の内部 SMTP リレー）
# 開発環境ではデフォルト（mailpit:1025）のままでよい
MAILSHIELD_DELIVER_DESTINATION=smtp-relay.example.com:25

# DB パスワード（必ず変更すること）
DB_PASSWORD=your_secure_password

# MinIO 認証情報（必ず変更すること）
MINIO_ACCESS_KEY=your_access_key
MINIO_SECRET_KEY=your_secret_key

# RabbitMQ パスワード（必ず変更すること）
RABBITMQ_PASSWORD=your_rabbitmq_password
```

### 3. 起動

```bash
# コアのみ（smtp-gateway + インフラ）
make core-up

# 開発標準（コア + 開発用 MTA + Mailpit）
make dev-up
```

### 4. 動作確認

```bash
# smtp-gateway ヘルスチェック
curl http://localhost:8080/healthz

# 開発用 MTA 経由でテストメール送信（swaks が必要、make dev-up の場合）
swaks --to test@internal.test \
      --from sender@external.test \
      --server localhost --port 25 \
      --header "Subject: Hello MailShield"
```

`dev` プロファイルを起動している場合は http://localhost:8025 (Mailpit) でメールを確認できます。

---

## 本番環境について

MailShield は既存の MTA（Postfix・Exchange・Sendmail 等）の **after-queue content filter**
として動作します。本番環境では `make dev-up` の MTA コンポーネント（Postfix）は使用せず、
自前の MTA から smtp-gateway (port 10025) に転送するよう設定してください。

詳細は [自前 MTA との連携](./mta-self-managed.md) を参照。

---

## 次のステップ

- [Docker profiles の詳細](./profiles.md) — 使うコンポーネントを選んで起動する
- [ワーカー設定](../guide/workers.md) — 検査・変換ワーカーを有効化する
- [自前 MTA を使う場合](./mta-self-managed.md) — Postfix/Rspamd の要件と設定例
