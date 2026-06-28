# クイックスタート

MailShield を Docker Compose で起動する最短手順です。
詳細な設定は [MailShield 設定ガイド](./mailshield-config.md) を参照してください。

---

## 前提条件

- Docker Engine 24.0 以上
- Docker Compose v2.20 以上
- `make` コマンド
- 受信 MTA（Postfix + Rspamd 等）がすでにセットアップ済みであること  
  → まだの場合は [MTA セットアップガイド](./mta-self-managed.md) を先に参照

---

## 手順

### 1. リポジトリをクローン

```bash
git clone https://github.com/koizumib/mailshield.git
cd mailshield
```

### 2. `.env` を作成してパスワードを設定

```bash
cp .env.example .env
```

`.env` を開いて `CHANGE_ME_` の箇所を実際の値に変更します。

```dotenv
MARIADB_ROOT_PASSWORD=（任意のパスワード）
DB_PASSWORD=（任意のパスワード）
MINIO_ACCESS_KEY=（8文字以上の任意の文字列）
MINIO_SECRET_KEY=（8文字以上の任意の文字列）
RABBITMQ_PASSWORD=（任意のパスワード）

# 自前 MTA の接続先
MAILSHIELD_REINJECT_HOST=（MTA のホスト名 or IP）
MAILSHIELD_REINJECT_PORT=10025

# 通知メールの送信に使う SMTP リレー
MAILSHIELD_NOTIFICATION_SMTP_HOST=（SMTP リレーのホスト名）
MAILSHIELD_NOTIFICATION_SMTP_PORT=25
```

> **先に `.env` を設定してから起動してください。**
> 起動後に変更してもパスワードが反映されません（`make clean` でやり直し）。

### 3. `config/mailshield.yaml` を編集

最低限変更が必要な箇所は2つです。

```yaml
server:
  trusted_sources:
    - （MTA のホスト名 or IP）  # 受信 MTA からの接続を許可

routes:
  - name: inbound
    match:
      to: "@example\\.com$"   # ← 自分のドメインに変更

  - name: outbound
    match:
      from: "@example\\.com$"  # ← 自分のドメインに変更
```

### 4. `config/policy-inbound.yaml` の配送先を設定

```yaml
  - name: default_deliver
    condition: "true"
    action: deliver
    destination: "（MTA のホスト名）:10025"   # ← 再インジェクト先に変更
```

### 5. 起動

```bash
# 標準構成（MinIO + RabbitMQ + Mailpit）
make dev-up

# 最小構成（MariaDB のみ・filesystem モード）
# ※ config/mailshield.yaml で storage.backend=filesystem, queue.backend=none を設定すること
docker compose up -d
```

### 6. 動作確認

```bash
# ヘルスチェック
curl http://localhost:8080/healthz   # → "ok"

# テストメール送信（MTA 経由）
swaks --to test@example.com \
      --from sender@external.example \
      --server （MTA のホスト名） --port 25 \
      --header "Subject: MailShield テスト"

# smtp-gateway のログ確認
docker compose logs -f smtp-gateway
```

---

## 次のステップ

設定の詳細・全パラメータの説明は以下を参照してください。

- [MailShield 設定ガイド](./mailshield-config.md) — 全設定項目のステップバイステップ解説
- [Docker プロファイル](./profiles.md) — 起動するコンポーネントの選択方法
- [ワーカー設定](../guide/workers.md) — av-worker / dlp-worker 等を有効化する
- [ポリシー設定](../guide/policy.md) — 配送・隔離・拒否のルールを定義する
