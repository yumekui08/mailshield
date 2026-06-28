# MailShield 設定ガイド

MTA（Postfix + Rspamd）のセットアップが完了している前提で進めます。
まだの場合は先に [MTA セットアップガイド](./mta-self-managed.md) を参照してください。

---

## このガイドで使う変数

以下の値はお使いの環境に合わせて読み替えてください。
ガイドを通じて `${変数名}` 形式で参照します。

| 変数名 | 説明 | 例 |
|-------|------|---|
| `DOMAIN` | 組織のメールドメイン | `example.com` |
| `MAILSHIELD_HOST` | MailShield サーバの IP または FQDN | `192.168.1.100` |
| `MTA_HOST` | 受信 MTA のホスト名（Docker ネットワーク内または外部） | `mail.example.com` |
| `REINJECT_PORT` | MTA の再インジェクト受付ポート（content_filter なし） | `10025` |
| `NOTIFY_SMTP_HOST` | 通知メールを送る SMTP リレーのホスト名 | `mail.example.com` |
| `NOTIFY_SMTP_PORT` | 通知 SMTP ポート | `25` |
| `NOTIFY_FROM` | システムメールの送信元アドレス | `noreply@example.com` |
| `WEB_UI_URL` | Web UI の公開 URL（ブラウザでアクセスする URL） | `http://192.168.1.100:3000` |
| `MINIO_PUBLIC_ENDPOINT` | ブラウザから MinIO へアクセスするエンドポイント | `192.168.1.100:9000` |

シェルに変数を定義しておくと以下の手順でコマンドをそのままコピー＆ペーストできます。

```bash
export DOMAIN=example.com
export MAILSHIELD_HOST=192.168.1.100
export MTA_HOST=mail.example.com
export REINJECT_PORT=10025
export NOTIFY_SMTP_HOST=mail.example.com
export NOTIFY_SMTP_PORT=25
export NOTIFY_FROM=noreply@example.com
export WEB_UI_URL=http://192.168.1.100:3000
export MINIO_PUBLIC_ENDPOINT=192.168.1.100:9000
```

---

## Step 1: リポジトリのクローン

```bash
git clone https://github.com/koizumib/mailshield.git
cd mailshield
```

---

## Step 2: `.env` の作成

```bash
cp .env.example .env
```

### 2-1. パスワード類（必須）

`.env` を開いて以下の5箇所を変更します。

```bash
# MARIADB_ROOT_PASSWORD, DB_PASSWORD, MINIO_ACCESS_KEY, MINIO_SECRET_KEY, RABBITMQ_PASSWORD
# をそれぞれ安全な値に変更する
vi .env
```

```dotenv
MARIADB_ROOT_PASSWORD=（任意のパスワード）
DB_PASSWORD=（任意のパスワード）
MINIO_ACCESS_KEY=（8文字以上の任意の文字列）
MINIO_SECRET_KEY=（8文字以上の任意の文字列）
RABBITMQ_PASSWORD=（任意のパスワード）
```

> **重要**: `make dev-up` より前に `.env` を設定してください。
> 先に起動すると MariaDB がデフォルトパスワードで初期化され、
> 後から `.env` を変更してもパスワード不一致で起動しません。
> 間違えた場合は `make clean` でボリュームを削除してやり直してください。

### 2-2. MTA 接続先

```bash
# .env の STEP 2 セクションを環境に合わせて編集する
```

```dotenv
# smtp-gateway が処理完了後にメールを戻す先の MTA
# MTA 側の content_filter なしポート（通常 10025）を指定する
MAILSHIELD_REINJECT_HOST=${MTA_HOST}
MAILSHIELD_REINJECT_PORT=${REINJECT_PORT}

# 隔離通知・OTP メールを送る SMTP リレー
# content_filter と同じ MTA ポートに送るとループするため、別ポート or 別ホストを指定する
MAILSHIELD_NOTIFICATION_SMTP_HOST=${NOTIFY_SMTP_HOST}
MAILSHIELD_NOTIFICATION_SMTP_PORT=${NOTIFY_SMTP_PORT}
```

> **ループに注意**: どちらも MTA の `content_filter` が設定されたポートに送ると
> smtp-gateway → MTA → smtp-gateway のループが発生します。
> 再インジェクトには content_filter なしのポート（通常 10025）を使い、
> 通知メールには別ポートまたは別の SMTP リレーを使ってください。

---

## Step 3: `config/mailshield.yaml` の設定

smtp-gateway のメイン設定ファイルです。

### 3-1. trusted_sources（MTA からの接続を許可）

smtp-gateway は `trusted_sources` に記載されたホスト・IP 帯からの接続のみ受け付けます。
**受信 MTA の IP またはホスト名を必ず追加してください。**

```yaml
server:
  trusted_sources:
    - ${MTA_HOST}          # 受信 MTA（ホスト名 or IP）
    - 127.0.0.0/8          # ローカルループバック
    # Docker ブリッジネットワーク経由で接続する場合は必要に応じてサブネットを追加
    # - 172.16.0.0/12
    # - 192.168.0.0/16
```

CIDR 表記も使用できます。ホスト名は起動時と 30 秒ごとに DNS 解決されます。

### 3-2. ルート定義（受信・送信ドメイン）

`routes:` ブロックは配列のため、ファイル全体を置換します。
受信・送信の両ルートをまとめて定義してください。

```yaml
routes:
  - name: inbound
    direction: inbound
    match:
      to: "@${DOMAIN}$"      # 受信ドメインの正規表現（バックスラッシュはYAML内で \\. と書く）
      to_match: any           # any: 宛先のいずれか1つがマッチすればこのルートを適用
    workers:
      inspect:
        - name: av-worker
          enabled: true        # ClamAV（scanners プロファイルが必要）
          timeout_seconds: 30
        - name: header-inspector
          enabled: true
          timeout_seconds: 5
        - name: url-worker
          enabled: true
          timeout_seconds: 15
        - name: qr-worker
          enabled: true
          timeout_seconds: 15
        - name: dlp-worker
          enabled: false       # 受信時は通常 false
          timeout_seconds: 60
      transform:
        - name: sanitize-worker
          enabled: true        # HTML 無害化（推奨）
          order: 1
        - name: url-rewrite-worker
          enabled: true        # URL をプロキシ経由に書き換え
          order: 2
        - name: disclaimer-worker
          enabled: false       # 受信時は通常 false
          order: 3
        - name: filesep-worker
          enabled: true        # 添付ファイル分離
          order: 4
        - name: arc-sealer
          enabled: false       # ARC 署名（別途鍵設定が必要）
          order: 5
    policy:
      rules_file: /app/config/policy-inbound.yaml

  - name: outbound
    direction: outbound
    match:
      from: "@${DOMAIN}$"     # 送信ドメインの正規表現
    workers:
      inspect:
        - name: dlp-worker
          enabled: true        # DLP 情報漏洩検査（送信時に推奨）
          timeout_seconds: 60
        - name: av-worker
          enabled: false       # 送信時は通常 false
          timeout_seconds: 30
        - name: header-inspector
          enabled: false
          timeout_seconds: 5
        - name: url-worker
          enabled: false
          timeout_seconds: 15
        - name: qr-worker
          enabled: false
          timeout_seconds: 15
      transform:
        - name: sanitize-worker
          enabled: false       # 送信時は通常 false
          order: 1
        - name: url-rewrite-worker
          enabled: false
          order: 2
        - name: disclaimer-worker
          enabled: true        # 送信フッター追加
          order: 3
        - name: filesep-worker
          enabled: true        # 添付ファイル分離（OTP ダウンロードリンクに置換）
          order: 4
        - name: arc-sealer
          enabled: false
          order: 5
    policy:
      rules_file: /app/config/policy-outbound.yaml
```

ドメインの正規表現について：YAML ファイル内ではバックスラッシュをエスケープする必要があります。

```yaml
# NG: @example.com$  （ドットが任意文字になる）
# OK: "@example\\.com$"
```

### 3-3. 通知メール SMTP

隔離通知・OTP メールなどシステムが送信するメールの設定です。

```yaml
notification:
  smtp_host: ${NOTIFY_SMTP_HOST}
  smtp_port: ${NOTIFY_SMTP_PORT}
  from_address: ${NOTIFY_FROM}
```

SMTP 認証が必要な場合は `.env` で設定します。

```dotenv
# .env
NOTIFICATION_AUTH_PASS=（SMTP 認証パスワード）
```

### 3-4. 隔離通知の Web UI リンク

隔離通知メールに埋め込む「確認はこちら」リンクのベース URL です。
**ブラウザからアクセスできる URL を指定してください。**

```yaml
quarantine_notification:
  enabled: true
  ui_base_url: "${WEB_UI_URL}"
```

### 3-5. ログレベル（任意）

初期構築時は `debug` にすると処理の詳細を確認できます。
動作確認後は `info` に戻してください。

```yaml
log:
  level: debug    # debug / info / warn / error
  format: json
```

### 設定例まとめ

実際の `config/mailshield.yaml` の構成例です。

```yaml
server:
  trusted_sources:
    - ${MTA_HOST}
    - 127.0.0.0/8

notification:
  smtp_host: ${NOTIFY_SMTP_HOST}
  smtp_port: ${NOTIFY_SMTP_PORT}
  from_address: ${NOTIFY_FROM}

quarantine_notification:
  enabled: true
  ui_base_url: "${WEB_UI_URL}"

log:
  level: info

routes:
  - name: inbound
    direction: inbound
    match:
      to: "@${DOMAIN}$"
      to_match: any
    workers:
      # ... (上記 3-2 の workers ブロックを記載)
    policy:
      rules_file: /app/config/policy-inbound.yaml

  - name: outbound
    direction: outbound
    match:
      from: "@${DOMAIN}$"
    workers:
      # ... (上記 3-2 の workers ブロックを記載)
    policy:
      rules_file: /app/config/policy-outbound.yaml
```

---

## Step 4: `config/policy-inbound.yaml` の設定

検査ワーカーの結果に応じた配送先を定義します。
**`destination` にはループを回避できる MTA エンドポイントを指定してください。**

```yaml
rules:
  # ウイルス検知 → 隔離
  - name: av_detected
    condition: "av-worker.detected == true"
    action: quarantine

  # その他すべて → 配送
  - name: default_deliver
    condition: "true"
    action: deliver
    destination: "${MTA_HOST}:${REINJECT_PORT}"
```

> **destination の選択肢:**
>
> | パターン | 指定例 | 説明 |
> |---------|-------|------|
> | MTA の再インジェクトポート | `mail.example.com:10025` | content_filter なしのポートに戻す（本番標準） |
> | 内部 SMTP リレー直指定 | `exchange.internal:25` | Exchange 等の内部MTA へ直接配送 |
> | Mailpit（開発確認用） | `localhost:1025` | dev プロファイルの Mailpit で受信確認 |

送信ポリシーは `config/policy-outbound.yaml` に同様の形式で定義します。

---

## Step 5: プロファイルの選択と起動

### プロファイル一覧

| プロファイル | 追加されるサービス | 用途 |
|------------|-----------------|------|
| _(なし)_ | smtp-gateway + MariaDB のみ | 最小構成 |
| `queue` | RabbitMQ | 外部システムへイベント通知する場合 |
| `storage` | MinIO | EML をオブジェクトストレージに保存する場合 |
| `scanners` | ClamAV / Tika / Tesseract | ウイルス検査・DLP・QR コード検査 |
| `dev` | Mailpit | 処理後メールをキャッチして確認する場合 |
| `api` | api-server + Web UI + Redis | 管理画面・REST API |

### 推奨構成

**最小構成（filesystem + キューなし）**

```bash
# config/mailshield.yaml で以下を設定してから起動する
# storage.backend: filesystem
# queue.backend: none
docker compose up -d
```

**標準構成（MinIO + RabbitMQ）**

```bash
COMPOSE_PROFILES=storage,queue docker compose up -d
```

**開発確認あり（Mailpit で処理後メールをキャッチ）**

```bash
COMPOSE_PROFILES=storage,queue,dev docker compose up -d
# または
make dev-up
```

**スキャナー有効（ClamAV 等）**

```bash
COMPOSE_PROFILES=storage,queue,dev,scanners docker compose up -d
# または
make scanners-up
```

**Web UI + API 込み**

```bash
COMPOSE_PROFILES=storage,queue,dev,api docker compose up -d
# または
make api-up
```

> ClamAV は初回起動時にウイルスDB（約300MB）をダウンロードするため
> `start_period: 180s` が設定されています。最初は数分待ってください。

### 停止

```bash
make dev-down
# または
COMPOSE_PROFILES=storage,queue,dev docker compose down
```

**ボリュームごと初期化する場合（パスワード変更後など）**

```bash
make clean
```

---

## Step 6: 動作確認

### ヘルスチェック

```bash
curl http://${MAILSHIELD_HOST}:8080/healthz
# → "ok" が返れば smtp-gateway が起動している
```

### テストメール送信

MTA 経由でメールを送って処理フローを確認します。

```bash
# 受信テスト（inbound ルートの確認）
swaks --to test@${DOMAIN} \
      --from sender@external.example \
      --server ${MTA_HOST} --port 25 \
      --header "Subject: MailShield 動作確認"

# 送信テスト（outbound ルートの確認）
swaks --to recipient@external.example \
      --from user@${DOMAIN} \
      --server ${MTA_HOST} --port 587 \
      --header "Subject: 送信テスト"
```

### ログで処理フローを確認

```bash
# smtp-gateway のログをリアルタイム表示
docker compose logs -f smtp-gateway

# 正常時のログ例（log.level: info の場合）
# [1/7] メール受信  route=inbound from=sender@external.example
# [2/7] EML 保存完了  eml_path=raw/2024/01/15/xxxx.eml
# [3/7] DB メタデータ記録完了
# [4/7] mail.received 発行完了
# [5/7] 検査パイプライン開始
# [6/7] 変換パイプライン開始
# [7/7] ポリシー評価開始
# メール処理完了  action=deliver elapsed_ms=234
```

### ポリシーシミュレーター

実際にメールを流さずにパイプラインの動作を確認できます。

```bash
# EML ファイルを POST して処理結果を確認する
curl -s -X POST http://${MAILSHIELD_HOST}:8080/simulate \
  --data-binary @docs/test-mails/sanitize-test.eml | jq .

# レスポンス例
# {
#   "route_name": "inbound",
#   "inspect_results": [...],
#   "original_subject": "...",
#   "transformed_subject": "...",
#   "action": "deliver",
#   "matched_rule": "default_deliver",
#   "processing_ms": 45
# }
```

---

## Step 7: api-server の設定（`api` プロファイルを使う場合）

管理 Web UI と REST API を使う場合は `config/api-server.yaml` を設定します。

### 7-1. frontend_url（CORS オリジン）

api-server が許可する Web UI のオリジンです。
**ブラウザから実際にアクセスする URL と一致させてください。**

```yaml
server:
  frontend_url: "${WEB_UI_URL}"
```

### 7-2. storage.public_endpoint（MinIO の公開 URL）

ブラウザが署名付き URL 経由で MinIO に直接アクセスする際のエンドポイントです。
**ブラウザからアクセスできるホスト名または IP を指定してください。**

```yaml
storage:
  endpoint: minio:9000                        # api-server → MinIO（Docker 内部通信）
  public_endpoint: ${MINIO_PUBLIC_ENDPOINT}   # ブラウザ → MinIO（外部アクセス）
```

`endpoint` は api-server と MinIO 間の内部通信に使います（同じ Docker ネットワーク内なら `minio:9000` のまま）。
`public_endpoint` はブラウザが EML・添付ファイルをダウンロードする URL に埋め込まれます。

### 7-3. notification（通知と再インジェクト）

```yaml
notification:
  from_address: ${NOTIFY_FROM}
  smtp_host: ${NOTIFY_SMTP_HOST}
  smtp_port: ${NOTIFY_SMTP_PORT}

  # 隔離解放時に処理済み EML を再配送する MTA
  # smtp-gateway の policy destination と同じ宛先を指定する
  reinject_host: ${MTA_HOST}
  reinject_port: ${REINJECT_PORT}
```

### 7-4. approval.base_url（承認通知リンク）

承認フローを使う場合、承認通知メール内のリンクに埋め込まれます。

```yaml
approval:
  base_url: "${WEB_UI_URL}"
```

### Web UI の初期ログイン

```bash
# api-server が起動したら Web UI を開く
open ${WEB_UI_URL}

# デフォルトの管理者アカウント（初回ログイン後は必ずパスワードを変更すること）
# Email:    admin@${DOMAIN}
# Password: password
```

---

## Step 8: filesep-worker の設定（添付ファイル分離機能を使う場合）

`filesep-worker` は添付ファイルを MinIO に分離保存し、ダウンロードリンクをメール本文に挿入します。
設定ファイルは `config/workers/conf/filesep-worker.yaml` です。

```yaml
# ダウンロードリンクのベース URL
# メール本文に挿入されるリンクの先頭に付与される
# 例: ${WEB_UI_URL}/files/{download_token}
frontend_url: ${WEB_UI_URL}

# 署名付き URL の有効期限（時間）
link_expiry_hours: 72

# 配送モード
# inline   : 元のメール本文冒頭にダウンロードリンクを挿入して配送
# separate : 添付ファイルを除いた本文と、リンクのみの通知メールを別送
mode: inline

# 分離する添付ファイルのフィルタ
min_size_bytes: 0     # 0 = サイズ制限なし
extensions: []        # [] = すべての拡張子が対象
# 特定拡張子のみ分離する場合の例:
# extensions: [.exe, .zip, .pdf, .docx, .xlsm]
```

`frontend_url` は Web UI の URL と同じ値を設定します。
ブラウザがこの URL にアクセスしてファイルをダウンロードします。

---

## 設定ファイルの変更を反映する

`config/` 以下のファイルは Docker ボリュームとしてマウントされているため、
変更後は smtp-gateway を再起動するだけで反映されます。

```bash
docker compose restart smtp-gateway

# api-server.yaml を変更した場合
docker compose restart api-server
```

---

## チェックリスト

- [ ] `.env` のパスワード類（`CHANGE_ME_` のままになっていないか）
- [ ] `server.trusted_sources` に自前 MTA の IP を追加した
- [ ] `routes[].match.to/from` のドメインを自組織ドメインに変更した
- [ ] `policy-inbound.yaml` / `policy-outbound.yaml` の `destination` を設定した
- [ ] `notification.smtp_host` をループしない SMTP リレーに設定した
- [ ] `quarantine_notification.ui_base_url` をブラウザからアクセスできる URL に設定した
- [ ] api プロファイルを使う場合: `api-server.yaml` の `frontend_url` / `storage.public_endpoint` / `reinject_host` を設定した
- [ ] filesep-worker を使う場合: `filesep-worker.yaml` の `frontend_url` を設定した
- [ ] ヘルスチェック `curl http://${MAILSHIELD_HOST}:8080/healthz` が `ok` を返す
- [ ] テストメールが MTA 経由で届いた（または Mailpit で確認できた）

---

## 次のステップ

- [ワーカー設定](../guide/workers.md) — av-worker / dlp-worker 等を有効化する
- [ポリシー設定](../guide/policy.md) — 配送・隔離・拒否のルールを詳細に定義する
- [隔離メール管理](../guide/quarantine.md) — 管理 Web UI での隔離メール操作
- [ARC 署名統合](./arc-integration.md) — Exchange Online / Google Workspace への ARC 登録
