# 設定リファレンス

設定は YAML ファイルと環境変数の2箇所から読み込まれる。環境変数は YAML の値を上書きする。

---

## smtp-gateway 設定（config/mailshield.yaml）

設定ファイルのパスは `CONFIG_FILE` 環境変数で指定できる（デフォルト: `config/mailshield.yaml`）。

受信・送信は同一バイナリが処理する。`routes:` セクションで MAIL FROM / RCPT TO の正規表現によりルートを振り分け、ルートごとにワーカーとポリシーを切り替える。

### server

| キー | 型 | デフォルト | 説明 |
|-----|-----|----------|------|
| `smtp_port` | int | `10024` | SMTPサーバーの待受ポート |
| `smtp_hostname` | string | `smtp-gateway` | SMTP EHLO/HELO で返すホスト名 |
| `max_message_size_mb` | int | `50` | 受け付けるメールの最大サイズ（MB） |
| `smtp_max_recipients` | int | `100` | 1通あたりの最大宛先数 |
| `smtp_read_timeout_seconds` | int | `30` | SMTPコマンド読み取りタイムアウト（秒） |
| `smtp_write_timeout_seconds` | int | `30` | SMTPレスポンス書き込みタイムアウト（秒） |
| `handler_timeout_seconds` | int | `60` | メール1通の処理タイムアウト（検査・変換・ポリシー込み）（秒） |
| `health_port` | int | `8080` | ヘルスチェック HTTP エンドポイントのポート |
| `shutdown_timeout_seconds` | int | `30` | グレースフルシャットダウンの最大待機時間（秒） |
| `trusted_sources` | []string | - | SMTP 接続を許可するIPまたはホスト名のリスト。CIDR 表記（例: `172.17.0.0/16`）も使用可 |

### storage

| キー | 型 | 環境変数 | 説明 |
|-----|-----|---------|------|
| `backend` | string | - | `minio` または `s3` |
| `endpoint` | string | `MINIO_ENDPOINT` | MinIOエンドポイント（例: `minio:9000`） |
| `access_key` | string | `MINIO_ACCESS_KEY` | アクセスキー |
| `secret_key` | string | `MINIO_SECRET_KEY` | シークレットキー |
| `bucket_eml` | string | - | EML保存バケット名 |
| `bucket_attachments` | string | - | 添付ファイル保存バケット名 |
| `use_ssl` | bool | `MINIO_USE_SSL` | TLS接続を使用するか |

### database

| キー | 型 | 環境変数 | 説明 |
|-----|-----|---------|------|
| `driver` | string | - | `mariadb` |
| `host` | string | `DB_HOST` | DBホスト名 |
| `port` | int | `DB_PORT` | DBポート番号 |
| `name` | string | `DB_NAME` | データベース名 |
| `user` | string | `DB_USER` | DBユーザー名 |
| `password` | string | `DB_PASSWORD` | DBパスワード |
| `max_open_conns` | int | `10` | 接続プールの最大オープン接続数 |
| `max_idle_conns` | int | `5` | 接続プールの最大アイドル接続数 |
| `conn_max_lifetime_minutes` | int | `5` | 接続の最大生存時間（分） |

### queue

| キー | 型 | 環境変数 | 説明 |
|-----|-----|---------|------|
| `backend` | string | - | `rabbitmq` |
| `host` | string | `RABBITMQ_HOST` | RabbitMQホスト名 |
| `port` | int | `RABBITMQ_PORT` | RabbitMQポート番号 |
| `user` | string | `RABBITMQ_USER` | RabbitMQユーザー名 |
| `password` | string | `RABBITMQ_PASSWORD` | RabbitMQパスワード |

### log

| キー | 型 | デフォルト | 説明 |
|-----|-----|----------|------|
| `level` | string | `info` | ログレベル: `debug` / `info` / `warn` / `error` |
| `format` | string | `json` | 出力フォーマット: `json` / `text` |
| `output` | string | `stdout` | 出力先: `stdout` / `syslog` |
| `syslog_tag` | string | `smtp-gateway` | syslog 出力時のタグ名 |

### workers（グローバル設定）

Lua ワーカースクリプトとワーカー固有設定ファイルのディレクトリ。全ルートで共有する。
ルートごとの有効・無効・実行順序は `routes[].workers.inspect` / `transform` で設定する。

| キー | 型 | 説明 |
|-----|-----|------|
| `workers_dir` | string | Lua ワーカースクリプトのルートディレクトリ。配下の `<worker名>/init.lua` を自動ロードする |
| `worker_config_dir` | string | ワーカー固有設定ファイル（YAML）のディレクトリ。`<worker名>.yaml` が各ワーカーに渡される |

```yaml
workers:
  workers_dir: /app/workers
  worker_config_dir: /app/config/workers/conf
```

### notification

システムが送信するメール（隔離通知・添付ファイル分離通知等）の SMTP 設定。
`filesep-worker` の separate モードも `notification.smtp_host` / `smtp_port` を使用する。

| キー | 型 | 説明 |
|-----|-----|------|
| `smtp_host` | string | 通知メール送信用 SMTP ホスト |
| `smtp_port` | int | 通知メール送信用 SMTP ポート |
| `from_address` | string | 通知メールの送信元アドレス |

**開発環境の推奨設定:**
```yaml
notification:
  smtp_host: mailpit
  smtp_port: 1025
  from_address: noreply@mailshield.internal
```

### reinject

`deliver` アクション時にメールを再インジェクトするデフォルト宛先。

| キー | 型 | ENV | 説明 |
|-----|-----|-----|------|
| `reinject.host` | string | `MAILSHIELD_REINJECT_HOST` | 再インジェクト先の SMTP ホスト |
| `reinject.port` | int | `MAILSHIELD_REINJECT_PORT` | 再インジェクト先の SMTP ポート |

- policy ファイルのルールに `destination` が明示されている場合は、そちらが優先される
- `api-server.yaml` の `notification.reinject_host/port` が未設定の場合、api-server は自動的にこの設定を継承する（SSOT）

```yaml
reinject:
  host: mailpit   # 本番環境: 下流 MTA の FQDN
  port: 1025      # 本番環境: Postfix の content_filter なしポート
```

**本番環境の注意点:**
通知メールを Postfix 経由で送ると content_filter ループが発生する恐れがある。Postfix を経由せず直接 SMTP リレーへ送るか、`mynetworks` / `check_client_access` で smtp-gateway からの接続を content_filter バイパスするよう設定すること。

### quarantine_notification

| キー | 型 | デフォルト | 説明 |
|-----|-----|----------|------|
| `enabled` | bool | `false` | `quarantine` アクション時に受信者へ即時通知メールを送るか |
| `ui_base_url` | string | - | 通知メール内のログインリンクのベース URL（例: `http://localhost:3000`） |

### approval

policy アクションが `approval` を返したとき、smtp-gateway がデータベースに `approval_requests` レコードを作成する。承認者の解決は「送信者の approver_id → 受信者の approver_id → global_approver_email」の順で行われる。

| キー | 型 | デフォルト | 説明 |
|-----|-----|----------|------|
| `expiry_hours` | int | `72` | 承認依頼の有効期限（時間）。0 の場合は 72 時間が使用される |
| `global_approver_email` | string | `""` | 承認者が解決できなかった場合のフォールバック承認者メールアドレス。空の場合は警告ログのみ出力 |

```yaml
approval:
  expiry_hours: 72
  global_approver_email: ""
```

### attachment_download

添付ファイルダウンロードの認証フロー設定。ルートの `direction` でフローを切り替える。

```yaml
attachment_download:
  flows:
    - match: outbound     # 送信メールからの分離添付
      mode: otp           # OTP（ワンタイムパスワード）認証
    - match: inbound      # 受信メールからの分離添付
      mode: auth          # ログイン認証 + メールボックスロール確認
      allowed_roles: [member, owner, admin]
    - match: internal     # 内部送信メールからの分離添付
      mode: auth
      allowed_roles: [member, owner, admin]
```

| `mode` | 説明 |
|--------|------|
| `otp` | ダウンロードリンクに OTP コードを要求する（非ログインユーザー向け） |
| `auth` | ログイン済みユーザーのみ許可。`allowed_roles` でメールボックスロールを制限する |

### routes

ルート定義は `routes:` リストに記述する。設定順に評価し、最初にマッチしたルートが適用される（first-match-wins）。

```yaml
routes:
  - name: inbound
    direction: inbound          # テナント解決: RCPT TO のドメイン
    match:
      to: "@internal\\.test$"   # RCPT TO の正規表現
      to_match: any             # any: いずれか1つがマッチ / all: 全てマッチ
    workers:
      inspect:
        - name: av-worker
          enabled: true
          timeout_seconds: 30
        # ... 他のワーカー
      transform:
        - name: filesep-worker
          enabled: true
          order: 4
    policy:
      rules_file: /app/config/policy-inbound.yaml

  - name: outbound
    direction: outbound         # テナント解決: MAIL FROM のドメイン
    match:
      from: "@internal\\.test$" # MAIL FROM の正規表現
    workers:
      # ... outbound 用ワーカー設定
    policy:
      rules_file: /app/config/policy-outbound.yaml
```

#### match フィールド

| キー | 型 | 説明 |
|-----|-----|------|
| `from` | string | MAIL FROM アドレスに適用する正規表現（省略時は全てマッチ） |
| `to` | string | RCPT TO アドレスに適用する正規表現（省略時は全てマッチ） |
| `to_match` | string | `any`（デフォルト）: 1つでもマッチ / `all`: 全宛先がマッチ |

#### direction の使い分け

| direction | テナント解決に使うアドレス | 用途 |
|-----------|----------------------|------|
| `inbound` | `To:` の最初のドメイン | 受信フィルタ（誰宛てのメールか） |
| `outbound` | `From:` のドメイン | 送信フィルタ（誰が送るメールか） |

### workers（ルート内の設定）

`routes[].workers` はルートごとの有効・無効・タイムアウト・実行順序を定義する。
ワーカー実装（スクリプトや接続先）のパスはグローバルの `workers:` セクションで設定する。

| キー | 型 | 説明 |
|-----|-----|------|
| `inspect` | []WorkerEntry | 検査ワーカーのリスト。全ワーカーが並列に実行される |
| `transform` | []WorkerEntry | 変換ワーカーのリスト（`order` 昇順で実行） |

**WorkerEntry:**

| キー | 型 | 説明 |
|-----|-----|------|
| `name` | string | ワーカー名（Lua の場合はディレクトリ名） |
| `enabled` | bool | 有効・無効 |
| `timeout_seconds` | int | このワーカーのタイムアウト（秒） |
| `order` | int | 変換ワーカーの実行順序（小さい値が先） |

### workers（ワーカー固有設定ファイル）

`worker_config_dir` 配下の `<worker名>.yaml` に各ワーカー固有のパラメータを記述する。

#### av-worker.yaml（ClamAV ウイルス検査）

```yaml
host: clamav
port: 3310
timeout_seconds: 25
max_size_mb: 25
scores:
  virus_detected: 100
```

#### dlp-worker.yaml（Apache Tika DLP）

```yaml
endpoint: "http://tika:9998"
timeout_seconds: 55
patterns:
  - name: credit_card
    regex: '\b(?:\d{4}[\s-]?){3}\d{4}\b'
    score: 80
  - name: personal_number
    regex: '\b\d{3}-\d{4}-\d{4}\b'
    score: 60
```

#### header-inspector.yaml

```yaml
threshold: 60
scores:
  spf_fail: 30
  dkim_fail: 40
  dmarc_fail: 30
  reply_to_mismatch: 40
  brand_spoofing: 60
brand_names: [amazon, google, microsoft, paypal, apple]
```

#### url-worker.yaml

```yaml
max_urls: 20
deny_list: []
reputation_api:
  backend: none       # none | safe_browsing | web_risk
  api_key: ""
scores:
  deny_list_match: 100
  reputation_api_hit: 90
```

#### qr-worker.yaml

```yaml
max_images: 10
qr_decode:
  enabled: true
ocr:
  enabled: false
  endpoint: "http://tesseract:8884"
  timeout_seconds: 30
deny_list: []
reputation_api:
  backend: none
  api_key: ""
scores:
  deny_list_match: 100
  reputation_api_hit: 90
```

#### url-rewrite-worker.yaml

```yaml
proxy_base_url: ""
url_encode: base64
rewrite_html: true
rewrite_text: true
skip_domains: [internal.test, localhost]
```

### policy.yaml の形式

```yaml
rules:
  - name: virus_detected
    condition: "av-worker.detected == true"
    action: reject
  - name: dlp_quarantine
    condition: "dlp-worker.detected == true"
    action: quarantine
  - name: header_suspicious
    condition: "header-inspector.score >= 60"
    action: quarantine
  - name: default_deliver
    condition: "true"
    action: deliver
    # destination: "mailpit:1025"   # 省略時は mailshield.yaml の reinject 設定が使われる
```

| condition 形式 | 例 |
|---------------|-----|
| `true` / `false` | 常にマッチ / スキップ |
| `{key} == {value}` | `av-worker.detected == true` |
| `{key} >= {int}` | `header-inspector.score >= 60` |

---

## api-server 設定（config/api-server.yaml）

設定ファイルのパスは `CONFIG_FILE` 環境変数で指定できる（デフォルト: `config/api-server.yaml`）。

### server

| キー | 型 | デフォルト | 説明 |
|-----|-----|----------|------|
| `port` | int | `8090` | HTTPサーバーの待受ポート |
| `shutdown_timeout_seconds` | int | `30` | グレースフルシャットダウンの最大待機時間（秒） |
| `frontend_url` | string | `http://localhost:3000` | CORS 許可オリジン（Web UI の URL） |

### database

smtp-gateway の `database` セクションと同じキー構成。

### redis

| キー | 型 | 説明 |
|-----|-----|------|
| `host` | string | Redis ホスト名 |
| `port` | int | Redis ポート番号 |
| `db` | int | 使用する DB インデックス（デフォルト: 1） |

### storage（api-server 用）

| キー | 型 | 説明 |
|-----|-----|------|
| `endpoint` | string | api-server → MinIO の内部接続先（例: `minio:9000`） |
| `public_endpoint` | string | ブラウザからアクセスする際のエンドポイント（空なら `endpoint` をそのまま使用） |
| `access_key` | string | MinIO アクセスキー |
| `secret_key` | string | MinIO シークレットキー |
| `bucket_eml` | string | EML 保存バケット名 |
| `bucket_attachments` | string | 添付ファイル保存バケット名 |
| `use_ssl` | bool | 内部接続に TLS を使用するか |
| `public_use_ssl` | bool | 署名付き URL に HTTPS を使用するか |
| `presigned_url_expiry_hours` | int | 署名付き URL の有効期間（時間） |

### auth

| キー | 型 | 説明 |
|-----|-----|------|
| `providers.standalone.enabled` | bool | 組み込み認証を有効化（パスワード認証） |
| `providers.oidc.enabled` | bool | OIDC 認証を有効化 |
| `providers.oidc.issuer` | string | OIDC プロバイダーの issuer URL |
| `providers.oidc.client_id` | string | OIDC クライアント ID |
| `providers.oidc.client_secret` | string | OIDC クライアントシークレット（環境変数 `OIDC_CLIENT_SECRET` で設定推奨） |
| `providers.oidc.redirect_uri` | string | OIDC コールバック URI |
| `providers.oidc.scopes` | []string | 要求するスコープ |
| `default_tenant_id` | string | ログイン時にデフォルトで使用するテナント UUID |
| `group_mappings.admin` | string | admin ロールにマッピングする OIDC グループ名 |
| `group_mappings.operator` | string | operator ロールにマッピングする OIDC グループ名 |
| `group_mappings.viewer` | string | viewer ロールにマッピングする OIDC グループ名 |
| `session.ttl_minutes` | int | セッション有効期間（分） |
| `session.cookie_name` | string | セッション Cookie 名 |
| `session.cookie_secure` | bool | Secure 属性を付与するか（本番環境では `true` 推奨） |

### mailbox_policy

隔離メールの可視性と解放権限をメールボックスロールで制御する。

```yaml
mailbox_policy:
  inbound_quarantine:
    visible_to: [member]    # To=内部ドメイン → メールボックスの member が閲覧可
    release_by: [member]    # To=内部ドメイン → メールボックスの member が解放可
  outbound_quarantine:
    visible_to: [owner]     # From=内部ドメイン → メールボックスの owner が閲覧可
    release_by: [admin]     # From=内部ドメイン → メールボックスの admin が解放可
```

| ロール | 説明 |
|-------|------|
| `member` | メールボックスの受信者（一般ユーザー） |
| `owner` | メールボックスの管理者（部門長など） |
| `admin` | メールボックスの全権管理者 |

admin/operator ロールのユーザーはこのフィルタに関わらず全件閲覧・操作できる。

### attachment_download（api-server 用）

```yaml
attachment_download:
  auth_mode:
    # mode=auth のとき、どのメールボックスロールのユーザーにダウンロードを許可するか。
    # 空の場合は全ロールを許可する。
    allowed_roles: [member, owner, admin]
```

### notification

システムが送信するメール（OTP・隔離解放・パスワードリセット）の共通 SMTP 設定。

| キー | 型 | 説明 |
|-----|-----|------|
| `from_address` | string | システムメール共通の送信元アドレス |
| `smtp_host` | string | 通知メール送信用 SMTP ホスト |
| `smtp_port` | int | 通知メール送信用 SMTP ポート |
| `starttls` | bool | STARTTLS を使用するか |
| `auth_user` | string | SMTP 認証ユーザー名（任意） |
| `auth_pass` | string | SMTP 認証パスワード（環境変数 `NOTIFICATION_AUTH_PASS` で設定推奨） |
| `reinject_host` | string | 隔離解放時の配送先 SMTP ホスト（省略時は `mailshield.yaml` の `reinject.host` を自動継承） |
| `reinject_port` | int | 隔離解放時の配送先 SMTP ポート（省略時は `mailshield.yaml` の `reinject.port` を自動継承） |

**開発環境の設定例（reinject は自動継承されるため通常は省略可）:**
```yaml
notification:
  smtp_host: mailpit
  smtp_port: 1025
  from_address: noreply@mailshield.internal
  # reinject_host / reinject_port は mailshield.yaml の reinject 設定から自動継承される
```

### approval

承認フローのバックグラウンドサービス設定。api-server は 30 秒ごとに未送信の通知メールを送信し、5 分ごとに期限切れの承認依頼を `expired` に更新する。

| キー | 型 | デフォルト | 説明 |
|-----|-----|----------|------|
| `expiry_hours` | int | `72` | 承認依頼の有効期限（時間） |
| `global_approver_email` | string | `""` | 承認者フォールバック先メールアドレス（smtp-gateway の同設定と合わせること） |
| `base_url` | string | - | 承認画面 URL のベース（通知メール内リンク生成に使用。例: `https://mailshield.example.com`） |
| `notification.from_address` | string | `""` | 承認通知メール専用の送信元アドレス（空の場合は `notification.from_address` を使用） |
| `notification.from_name` | string | `"MailShield 承認システム"` | 送信者表示名 |
| `notification.request_enabled` | bool | `false` | 承認者へのメール通知を有効にするか |
| `notification.request_subject_template` | string | - | 承認依頼メール件名（Go `text/template` 形式） |
| `notification.request_body_template` | string | - | 承認依頼メール本文（Go `text/template` 形式） |
| `notification.result_enabled` | bool | `false` | 承認結果の送信者通知を有効にするか（内部ユーザーのみ） |
| `notification.approved_subject_template` | string | - | 承認済み通知メール件名 |
| `notification.approved_body_template` | string | - | 承認済み通知メール本文 |
| `notification.rejected_subject_template` | string | - | 却下通知メール件名 |
| `notification.rejected_body_template` | string | - | 却下通知メール本文 |

**テンプレート変数:**

| 変数 | 説明 |
|------|------|
| `{{.Subject}}` | メールの件名 |
| `{{.FromAddress}}` | 送信元メールアドレス |
| `{{.ToAddresses}}` | 宛先アドレスのスライス |
| `{{.ReceivedAt}}` | 受信日時（`2006-01-02 15:04:05` 形式） |
| `{{.ExpiresAt}}` | 承認期限（`2006-01-02 15:04:05` 形式） |
| `{{.ApprovalURL}}` | 承認画面の URL（`base_url` + `/approvals/{id}`） |
| `{{.Comment}}` | 承認者コメント（結果通知時のみ使用） |

**設定例:**
```yaml
approval:
  expiry_hours: 72
  global_approver_email: "manager@example.com"
  base_url: "https://mailshield.example.com"
  notification:
    from_name: "MailShield 承認システム"
    request_enabled: true
    request_subject_template: "【要承認】メール送信の承認申請: {{.Subject}}"
    request_body_template: |
      件名: {{.Subject}}
      送信元: {{.FromAddress}}
      承認期限: {{.ExpiresAt}}
      承認画面: {{.ApprovalURL}}
    result_enabled: true
    approved_subject_template: "【承認済み】{{.Subject}}"
    approved_body_template: "あなたのメールが承認されました。\nコメント: {{.Comment}}"
    rejected_subject_template: "【却下】{{.Subject}}"
    rejected_body_template: "あなたのメールは却下されました。\nコメント: {{.Comment}}"
```

### settings

api-server が管理する設定ファイルへのパス（設定画面からの GUI 編集に使用）。

| キー | 型 | 説明 |
|-----|-----|------|
| `policy_file` | string | ポリシー YAML のパス（docker-compose で smtp-gateway と共有ボリュームをマウント） |
| `smtp_gateway_config_file` | string | smtp-gateway の設定ファイルパス |

### log

| キー | 型 | デフォルト | 説明 |
|-----|-----|----------|------|
| `level` | string | `info` | ログレベル: `debug` / `info` / `warn` / `error` |
| `format` | string | `json` | 出力フォーマット: `json` / `text` |
