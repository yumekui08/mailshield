# クイックスタート

外部 MTA を用意せずに、単一マシン上で MailShield の受信パイプライン（検査 → 変換 → ポリシー評価 → 配送）を一通り動かして評価する手順。処理後のメールは同梱の [Mailpit](https://mailpit.axllent.org/) で確認する。

| 項目 | 内容 |
|------|------|
| 対象読者 | MailShield を初めて触る開発者・評価者 |
| 所要時間 | 約 15 分 |
| 構築されるもの | smtp-gateway + MariaDB + MinIO + Mailpit（Docker Compose） |

> [!IMPORTANT]
> この手順は**評価専用**の構成です。テストメールを SMTP クライアントから smtp-gateway（`:10024`）へ直接投入し、配送先を Mailpit に向けます。本番導入では受信 MTA（Postfix 等）を前段に置く必要があります。[MailShield 設定ガイド](./mailshield-config.md) と [MTA との連携](./mta-self-managed.md) を参照してください。

## 前提条件

| ソフトウェア | バージョン | 確認コマンド |
|------------|-----------|-------------|
| Docker Engine | 24.0 以上 | `docker --version` |
| Docker Compose | v2.20 以上 | `docker compose version` |
| GNU Make | — | `make --version` |
| Git | — | `git --version` |
| swaks（テストメール送信用） | — | `swaks --version` |

swaks が未インストールの場合:

```bash
# Debian / Ubuntu
sudo apt install swaks

# RHEL 系
sudo dnf install swaks
```

## 手順

### 1. リポジトリをクローンする

```bash
git clone https://github.com/koizumib/mailshield.git
cd mailshield
```

### 2. 環境変数ファイルを作成する

```bash
cp .env.example .env
```

`.env` を開き、以下のとおり編集する。

**(a) パスワード類** — `CHANGE_ME_` で始まる 5 箇所を任意の値に変更する。

```dotenv
MARIADB_ROOT_PASSWORD=<任意のパスワード>
DB_PASSWORD=<任意のパスワード>
MINIO_ACCESS_KEY=<8文字以上の任意の文字列>
MINIO_SECRET_KEY=<8文字以上の任意の文字列>
```

**(b) 配送先・通知先** — 評価環境では両方とも Mailpit に向ける。

```dotenv
# 処理済みメールの配送先（評価環境では Mailpit）
MAILSHIELD_REINJECT_HOST=mailpit
MAILSHIELD_REINJECT_PORT=1025

# 隔離通知等システムメールの送信先（評価環境では Mailpit）
MAILSHIELD_NOTIFICATION_SMTP_HOST=mailpit
MAILSHIELD_NOTIFICATION_SMTP_PORT=1025
```

> [!WARNING]
> `.env` は**初回起動より前に**設定してください。MariaDB は初回起動時のパスワードでボリュームを初期化するため、起動後に `.env` を変更すると認証エラーになります。間違えた場合は `make docker-clean`（全ボリューム削除）でやり直せます。

### 3. ホストからの SMTP 接続を許可する

smtp-gateway は `trusted_sources` に列挙した接続元以外からの SMTP を拒否する。ホストマシンから公開ポート `10024` に接続すると、接続元は Docker ブリッジネットワークのゲートウェイ IP（`172.x.x.1`）になるため、プライベートサブネットを許可しておく。

`config/mailshield.yaml` に以下を追記する。

```yaml
server:
  trusted_sources:
    - 127.0.0.1
    - 172.16.0.0/12    # Docker ブリッジネットワーク（評価環境用）
```

### 4. 起動する

```bash
make dev-up
```

初回はイメージのビルドが走るため数分かかる。完了後、全サービスが `running` / `healthy` になっていることを確認する。

```bash
docker compose -f docker/docker-compose.yml ps
```

期待される出力（抜粋）:

```
NAME                        STATUS
mailshield-smtp-gateway-1   Up (healthy)
mailshield-mariadb-1        Up (healthy)
mailshield-minio-1          Up (healthy)
mailshield-mailpit-1        Up (healthy)
```

ヘルスチェックエンドポイントで smtp-gateway の起動を確認する。

```bash
curl http://localhost:8080/healthz
```

期待される出力:

```
ok
```

### 5. テストメールを送信する

デフォルトの受信ルート（`config/routes.d/10-inbound/`）は宛先ドメイン `internal.test` にマッチするよう設定されている。まずは通常のメールを送る。

```bash
swaks --to test@internal.test \
      --from sender@external.test \
      --server localhost --port 10024 \
      --header "Subject: Hello MailShield" \
      --body "This is a test."
```

期待される出力（最終行）:

```
<-  250 2.0.0 OK: queued
```

ブラウザで Mailpit（<http://localhost:8025>）を開き、件名 **Hello MailShield** のメールが届いていることを確認する。

### 6. 変換パイプラインの動作を確認する

同梱の開発用 Lua ワーカー（`subject-virus-transformer`）は、件名に `virus` を含むメールの件名冒頭にプレフィックスを付加する。

```bash
swaks --to test@internal.test \
      --from sender@external.test \
      --server localhost --port 10024 \
      --header "Subject: virus test mail" \
      --body "This mail contains virus in subject."
```

Mailpit で件名が **`[迷惑メール注意] virus test mail`** に書き換わっていれば、検査ワーカー（検知）→ 変換ワーカー（件名書き換え）→ ポリシーエンジン（配送決定）のパイプラインが機能している。

### 7. 処理ログを確認する

```bash
docker compose -f docker/docker-compose.yml logs smtp-gateway --tail=30
```

メール 1 通につき `[1/7]`〜`[7/7]` のステップログが出力される。詳細は [ログ仕様](../specs/logging.md) を参照。

> [!NOTE]
> `av-worker`（ClamAV）は `scanners` プロファイルを有効にしていないため接続エラーの WARN ログが出るが、エラーのワーカーはスキップされ処理は続行される。ClamAV 込みで試す場合は `make scanners-up` で起動し直す（初回はウイルス定義 DB 約 300MB のダウンロードで数分かかる）。

## 環境の削除

```bash
# コンテナの停止・削除（データボリュームは保持）
make dev-down

# データボリュームも含めて完全に削除
make docker-clean
```

## 次のステップ

| 目的 | ドキュメント |
|------|------------|
| 本番導入（受信 MTA との連携） | [MTA との連携](./mta-self-managed.md) → [MailShield 設定ガイド](./mailshield-config.md) |
| 管理 Web UI・REST API を試す | `make api-up` で起動し、[プロファイルガイド](./profiles.md) を参照 |
| ClamAV / Tika 等の検査ワーカーを有効化する | [ワーカー設定](../guide/workers.md) |
| 配送・隔離・拒否のルールを書く | [ポリシー設定](../guide/policy.md) |
