# クイックスタート（Docker）

MailShield を Docker Compose で起動する手順です。
**用途に合わせて2パターンから選んでください。**

---

## 前提条件

- Docker Engine 24.0 以上
- Docker Compose v2.20 以上
- `make` コマンド

---

## パターン A: 開発・動作確認（組み込み MTA を使う）

Postfix + Rspamd + Mailpit をまとめて起動します。
メールサーバーを自前で用意しなくてもすぐ動作確認できます。

### 手順

**1. リポジトリをクローン**

```bash
git clone https://github.com/koizumib/mailshield.git
cd mailshield
```

**2. `.env` を作成してパスワードを設定**

```bash
cp .env.example .env
```

`.env` を開いて STEP 1 の4箇所を変更します（`CHANGE_ME_` の部分）。

```dotenv
MARIADB_ROOT_PASSWORD=（任意のパスワード）
DB_PASSWORD=（任意のパスワード）
MINIO_ACCESS_KEY=（8文字以上の任意の文字列）
MINIO_SECRET_KEY=（8文字以上の任意の文字列）
RABBITMQ_PASSWORD=（任意のパスワード）
```

STEP 2・3 はデフォルトのままで構いません。

**3. 受信ドメインを設定（本番環境のみ）**

デフォルトの `config/mailshield.yaml` は開発用ドメイン `internal.test` が設定済みです。
**開発・動作確認ならこの手順はスキップできます。**

本番環境で自分のドメインを使う場合は `config/mailshield.yaml` を編集します。

```yaml
routes:
  - name: inbound
    match:
      to: "@internal\\.test$"   # ← 自分のドメインに変更（例: "@example\\.com$"）
```

`config/policy-inbound.yaml` の `destination` も変更してください。

```yaml
  - name: default_deliver
    action: deliver
    destination: "mailpit:1025"   # ← 本番の配送先 MTA に変更（例: "smtp-relay.example.com:25"）
```

**4. 起動**

```bash
make dev-up
# smtp-gateway + MariaDB + MinIO + RabbitMQ + Postfix + Rspamd + Mailpit が起動する
```

**5. 動作確認**

```bash
# ヘルスチェック
curl http://localhost:8080/healthz

# テストメール送信
swaks --to test@internal.test --from sender@external.test \
      --server localhost --port 25 \
      --header "Subject: Hello MailShield"

# Mailpit でメールを確認
open http://localhost:8025
```

---

## パターン B: 自前 MTA と組み合わせる（本番に近い構成）

自前の Postfix + Rspamd をすでに持っている場合の手順です。
スキャナー（ClamAV・Tika）と Web UI も含めた全機能構成で起動します。

### 手順

**1. リポジトリをクローン**

```bash
git clone https://github.com/koizumib/mailshield.git
cd mailshield
```

**2. `.env` を作成してパスワードと MTA 接続先を設定**

```bash
cp .env.example .env
```

`.env` を開いて以下を変更します。

```dotenv
# STEP 1: パスワード（必須）
MARIADB_ROOT_PASSWORD=（任意のパスワード）
DB_PASSWORD=（任意のパスワード）
MINIO_ACCESS_KEY=（8文字以上の任意の文字列）
MINIO_SECRET_KEY=（8文字以上の任意の文字列）
RABBITMQ_PASSWORD=（任意のパスワード）

# STEP 2: 自前 MTA の接続先
MAILSHIELD_REINJECT_HOST=（自前の Postfix ホスト名または IP）
MAILSHIELD_REINJECT_PORT=10025   # content_filter なしのポート
MAILSHIELD_NOTIFICATION_SMTP_HOST=（通知メール用の SMTP リレー）
MAILSHIELD_NOTIFICATION_SMTP_PORT=（ポート番号）
```

**3. `config/mailshield.yaml` を編集**

```yaml
server:
  trusted_sources:
    - （自前 Postfix の IP またはホスト名）  # ← 追加

routes:
  - name: inbound
    match:
      to: "@example\\.com$"   # ← 自分のドメインに変更

  - name: outbound
    match:
      from: "@example\\.com$"   # ← 自分のドメインに変更
```

**4. `config/api-server.yaml` を編集**

```yaml
server:
  frontend_url: "http://（このサーバのIPまたはホスト名）:3000"  # ← Web UI の URL

storage:
  public_endpoint: "（このサーバのIPまたはホスト名）:9000"  # ← ブラウザから MinIO にアクセスできる URL

notification:
  reinject_host: （自前の Postfix ホスト名または IP）  # ← 隔離解放後の再インジェクト先
  reinject_port: 10025

approval:
  base_url: "http://（このサーバのIPまたはホスト名）:3000"  # ← 承認通知リンクのベース URL
```

**5. 自前 Postfix の設定**

Postfix の `main.cf` に content_filter を追加します。

```
content_filter = smtp:[（MailShield のホスト名または IP）]:10024
```

再インジェクト用のポート 10025 も `master.cf` で開けてください（詳細: [自前 MTA との連携](./mta-self-managed.md)）。

**6. 起動**

```bash
COMPOSE_PROFILES=storage,queue,scanners,api docker compose up -d
```

**7. 動作確認**

```bash
# MailShield ヘルスチェック
curl http://localhost:8080/healthz   # smtp-gateway
curl http://localhost:8090/healthz   # api-server

# Web UI にログイン
open http://localhost:3000
# デフォルト管理者: admin@example.com / password

# 自前 MTA 経由でテストメール送信（メールは Web UI の「メール一覧」に現れる）
swaks --to user@example.com --from sender@external.example \
      --server （自前 Postfix のホスト） --port 25 \
      --header "Subject: MailShield test"
```

---

## 停止

```bash
make dev-down    # パターン A
docker compose down   # パターン B
```

---

## 次のステップ

- [Docker プロファイルの詳細](./profiles.md) — 起動するコンポーネントを細かく選択する
- [ワーカー設定](../guide/workers.md) — av-worker・dlp-worker 等を有効化する
- [ポリシー設定](../guide/policy.md) — 配送・隔離・拒否のルールを定義する
- [自前 MTA との連携](./mta-self-managed.md) — Postfix の詳細設定
