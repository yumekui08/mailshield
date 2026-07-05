# 設定リファレンス

設定は YAML ファイルと環境変数の2箇所から読み込まれる。環境変数は YAML の値を上書きする。

---

## smtp-gateway 設定（config/mailshield.yaml）

設定は**ディレクトリ単位**で読み込む。設定ディレクトリは `-c` / `--config` フラグまたは環境変数 `MAILSHIELD_CONFIG_DIR` で指定する（デフォルト: `config`）。

読み込み順序（後のものが前のものを上書きする）:

1. `<configDir>/mailshield.default.yaml` — 全パラメータのデフォルト値（任意）
2. `<configDir>/mailshield.yaml` — ユーザー設定
3. `<configDir>/mailshield.d/*.yaml` — フラグメント（任意・アルファベット順）
4. 環境変数 — 最優先
5. `<configDir>/routes.d/` — ルート定義（任意）

受信・送信は同一バイナリが処理する。`config/routes.d/` 配下のルートディレクトリで MAIL FROM / RCPT TO の正規表現によりルートを振り分け、ルートごとにワーカーとポリシーを切り替える。

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
| `health_port` | int | `8080` | ヘルスチェック HTTP エンドポイントのポート（`GET /healthz`、`POST /simulate`） |
| `shutdown_timeout_seconds` | int | `30` | グレースフルシャットダウンの最大待機時間（秒） |
| `http_shutdown_timeout_seconds` | int | `5` | HTTP ヘルスチェックサーバーのシャットダウンタイムアウト（秒） |
| `trusted_sources` | []string | - | SMTP 接続を許可するIPまたはホスト名のリスト。CIDR 表記（例: `172.17.0.0/16`）も使用可 |
| `dns_resolve_timeout_seconds` | int | `3` | `trusted_sources` のホスト名を DNS 解決する際のタイムアウト（秒） |
| `storage_save_timeout_seconds` | int | `15` | 原本 EML 保存（MinIO / FS）のタイムアウト（秒） |
| `db_save_timeout_seconds` | int | `5` | メタデータ DB 保存のタイムアウト（秒） |
| `queue_publish_timeout_seconds` | int | `5` | RabbitMQ イベント発行のタイムアウト（秒） |
| `simulate_timeout_seconds` | int | `30` | `POST /simulate` ハンドラ全体のタイムアウト（秒） |
| `archive_max_retries` | int | `3` | archiveAsync の最大リトライ回数 |
| `archive_retry_backoff_seconds` | int | `2` | archiveAsync リトライのバックオフ基準秒数（線形: `(attempt+1) × backoff`） |

**HTTP エンドポイント一覧（`health_port` で公開）:**

| パス | メソッド | 説明 |
|------|---------|------|
| `/healthz` | `GET` | ヘルスチェック。起動中は `200 OK` を返す |
| `/simulate` | `POST` | テスト用メール処理シミュレーション。リクエストボディに EML を渡すとパイプラインを実行して結果を返す |

### storage

| キー | 型 | 環境変数 | 説明 |
|-----|-----|---------|------|
| `backend` | string | - | `minio`、`s3`、または `filesystem` |
| `endpoint` | string | `MINIO_ENDPOINT` | MinIOエンドポイント（例: `minio:9000`）。`minio` / `s3` モード専用 |
| `access_key` | string | `MINIO_ACCESS_KEY` | アクセスキー。`minio` / `s3` モード専用 |
| `secret_key` | string | `MINIO_SECRET_KEY` | シークレットキー。`minio` / `s3` モード専用 |
| `bucket_eml` | string | - | EML保存バケット名。`minio` / `s3` モード専用 |
| `bucket_attachments` | string | - | 添付ファイル保存バケット名。`minio` / `s3` モード専用 |
| `use_ssl` | bool | `MINIO_USE_SSL` | TLS接続を使用するか。`minio` / `s3` モード専用 |
| `region` | string | - | S3 / MinIO リージョン（MinIO 単体では無視。AWS S3 では必須。省略時 `us-east-1`） |
| `local_dir` | string | - | ローカルFS保存先ディレクトリ（例: `/var/mailshield/eml`）。`filesystem` モード専用 |
| `public_base_url` | string | - | 署名付きURL生成時のベースURL（例: `http://api-server:8090`）。`filesystem` モード専用 |
| `public_path_prefix` | string | `/internal/files/` | `GetPresignedURL` が生成する URL の固定パスセグメント。api-server のルーティングと合わせること。`filesystem` モード専用 |

**`filesystem` モードについて:**
MinIO が不要なため、開発環境や単一ノード構成に適している。`local_dir` にメールが保存され、`public_base_url` を使って `GetPresignedURL` が URL を返す。

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
| `ping_timeout_seconds` | int | `5` | 起動時の DB 疎通確認タイムアウト（秒） |

### queue

| キー | 型 | 環境変数 | 説明 |
|-----|-----|---------|------|
| `backend` | string | - | `rabbitmq` または `none` |
| `host` | string | `RABBITMQ_HOST` | RabbitMQホスト名。`rabbitmq` モード専用 |
| `port` | int | `RABBITMQ_PORT` | RabbitMQポート番号。`rabbitmq` モード専用 |
| `user` | string | `RABBITMQ_USER` | RabbitMQユーザー名。`rabbitmq` モード専用 |
| `password` | string | `RABBITMQ_PASSWORD` | RabbitMQパスワード。`rabbitmq` モード専用 |

**`none` モードについて:**
イベント通知を発行しない（noop）。RabbitMQ が不要なため、開発環境や単一ノード構成に適している。

### log

| キー | 型 | デフォルト | 説明 |
|-----|-----|----------|------|
| `level` | string | `info` | ログレベル: `debug` / `info` / `warn` / `error` |
| `format` | string | `json` | 出力フォーマット: `json` / `text` |
| `output` | string | `stdout` | 出力先: `stdout` / `syslog` |
| `syslog_tag` | string | `smtp-gateway` | syslog 出力時のタグ名 |

### workers（グローバル設定）

Lua ワーカースクリプトとワーカー固有設定ファイルのディレクトリ。全ルートで共有する。
ルートごとの有効・無効・実行順序は `config/routes.d/<ルート名>/route.yaml` の `workers.inspect` / `transform` で設定する。

| キー | 型 | 説明 |
|-----|-----|------|
| `workers_dir` | string | Lua ワーカースクリプトのルートディレクトリ。配下の `<worker名>/init.lua` を自動ロードする |
| `worker_config_dir` | string | ワーカー固有設定ファイル（YAML）のディレクトリ。`<worker名>.yaml` が各ワーカーに渡される |

```yaml
workers:
  workers_dir: ./workers                  # Docker では /app/workers を env で上書き
  worker_config_dir: ./config/workers     # Docker では /app/config/workers を env で上書き
```

### notification

システムが送信するメール（隔離通知・添付ファイル分離通知等）の SMTP 設定。
`filesep-worker` の separate モードも `notification.smtp_host` / `smtp_port` を使用する。

| キー | 型 | 環境変数 | 説明 |
|-----|-----|---------|------|
| `smtp_host` | string | `MAILSHIELD_NOTIFICATION_SMTP_HOST` | 通知メール送信用 SMTP ホスト |
| `smtp_port` | int | `MAILSHIELD_NOTIFICATION_SMTP_PORT` | 通知メール送信用 SMTP ポート |
| `from_address` | string | - | 通知メールの送信元アドレス |
| `smtp_connect_timeout_seconds` | int | - | 通知メール SMTP の TCP 接続タイムアウト（秒・省略時 10） |
| `smtp_deadline_seconds` | int | - | 通知メール SMTP 操作全体のデッドライン（秒・省略時 30） |

**開発環境の推奨設定:**
```yaml
notification:
  smtp_host: mailpit
  smtp_port: 1025
  from_address: noreply@mailshield.internal
```

### reinject

`deliver` アクション時にメールを再インジェクトするデフォルト宛先。

| キー | 型 | 環境変数 | 説明 |
|-----|-----|---------|------|
| `reinject.host` | string | `MAILSHIELD_REINJECT_HOST` | 再インジェクト先の SMTP ホスト |
| `reinject.port` | int | `MAILSHIELD_REINJECT_PORT` | 再インジェクト先の SMTP ポート |

- policy ファイルのルールに `destination` が明示されている場合は、そちらが優先される
- `deliverers.default` が定義されている場合は、そちらが優先される（reinject は後方互換用）
- `api-server.yaml` の `notification.reinject_host/port` が未設定の場合、api-server は自動的にこの設定を継承する（SSOT）

```yaml
reinject:
  host: mailpit   # 本番環境: 下流 MTA の FQDN
  port: 1025      # 本番環境: Postfix の content_filter なしポート
```

**本番環境の注意点:**
通知メールを Postfix 経由で送ると content_filter ループが発生する恐れがある。Postfix を経由せず直接 SMTP リレーへ送るか、`mynetworks` / `check_client_access` で smtp-gateway からの接続を content_filter バイパスするよう設定すること。

### deliverers

名前付き配送トランスポート定義。policy ファイルの `rules[].destination` に deliverer 名を書くと、その deliverer 経由で配送される。ローカル MTA への平文再インジェクトのほか、SendGrid / Amazon SES などの外部 SMTP エンドポイント（STARTTLS + SMTP AUTH）を配送先にできる。

| キー | 型 | デフォルト | 説明 |
|-----|-----|----------|------|
| `deliverers.<名前>.type` | string | `smtp` | 配送方式（現在は `smtp` のみ） |
| `deliverers.<名前>.host` | string | —（必須） | 接続先ホスト |
| `deliverers.<名前>.port` | int | `25` | 接続先ポート |
| `deliverers.<名前>.tls` | string | `none` | `none`（平文）/ `starttls` / `tls`（SMTPS） |
| `deliverers.<名前>.auth.username` | string | — | SMTP AUTH ユーザー名。設定すると AUTH を行う（PLAIN / LOGIN をサーバー広告から自動選択） |
| `deliverers.<名前>.auth.password` | string | — | SMTP AUTH パスワード |
| `deliverers.<名前>.tls_skip_verify` | bool | `false` | TLS 証明書検証をスキップ（開発・テスト用） |

- **`default` は予約名。** `destination` 未指定の deliver ルールには `deliverers.default` が使われ、未定義なら `reinject.host:port`（平文 SMTP）にフォールバックする
- deliverer 名は英数字・ハイフン・アンダースコアのみ（`:` を含む名前は不可。`destination` の host:port 形式と区別するため）
- `destination` の解決順序: **deliverer 名 → host[:port] 形式**（名前が優先）
- パスワードは環境変数 `MAILSHIELD_DELIVERER_<大文字名>_PASSWORD` が YAML より優先される（名前のハイフンは `_` に変換。例: `ses-tokyo` → `MAILSHIELD_DELIVERER_SES_TOKYO_PASSWORD`）
- 平文接続（`tls: none`）でリモートホストに AUTH を設定した場合、送信時に資格情報の送出を拒否してエラーになる（localhost 宛は許可）

```yaml
deliverers:
  default:                        # destination 未指定のルールで使われる
    host: postfix
    port: 25
  sendgrid:
    host: smtp.sendgrid.net
    port: 587
    tls: starttls
    auth:
      username: apikey            # SendGrid は固定文字列 "apikey"
      password: ""                # ENV: MAILSHIELD_DELIVERER_SENDGRID_PASSWORD
  ses:
    host: email-smtp.ap-northeast-1.amazonaws.com
    port: 587
    tls: starttls
    auth:
      username: AKIAXXXXXXXXXXXXXXXX
      password: ""                # ENV: MAILSHIELD_DELIVERER_SES_PASSWORD
```

**SendGrid / SES 利用時の注意:** 両サービスとも送信元（エンベロープ FROM）のドメイン検証が必要なため、外部送信者の FROM をそのまま中継する inbound ルートの配送先には使えない。自組織ドメインが FROM になる outbound ルートの配送先として使用すること。

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

### routes（config/routes.d/）

ルートは `config/mailshield.yaml` には記述しない。
`config/routes.d/<NN-name>/` ディレクトリごとに `route.yaml` と `policy.yaml` を配置する。
ディレクトリ名の数字プレフィックスが評価順序を決定する（小さい方が先・first-match-wins）。

```
config/routes.d/
├── 00-bounce/    # バウンス（MAIL FROM:<>）← 変更不要
│   ├── route.yaml
│   └── policy.yaml
├── 10-inbound/   # 受信ルート
│   ├── route.yaml
│   └── policy.yaml
└── 20-outbound/  # 送信ルート
    ├── route.yaml
    └── policy.yaml
```

**route.yaml の形式:**

```yaml
# config/routes.d/10-inbound/route.yaml
name: inbound
direction: inbound          # 基準アドレス: RCPT TO のドメイン
match:
  to: "@example\\.com$"   # RCPT TO の正規表現
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
# policy は同ディレクトリの policy.yaml / policy.lua を自動参照する
```

**policy.yaml の形式:**

```yaml
# config/routes.d/10-inbound/policy.yaml
rules:
  - name: av_detected
    condition: "av-worker.detected == true"
    action: quarantine
  - name: default_deliver
    condition: "true"
    action: deliver
    destination: "mail.example.com:10025"
```

#### match フィールド

| キー | 型 | 説明 |
|-----|-----|------|
| `from` | string | MAIL FROM アドレスに適用する正規表現（省略時は全てマッチ） |
| `to` | string | RCPT TO アドレスに適用する正規表現（省略時は全てマッチ） |
| `to_match` | string | `any`（デフォルト）: 1つでもマッチ / `all`: 全宛先がマッチ |

#### direction の使い分け

| direction | 基準アドレス | 用途 |
|-----------|------------|------|
| `inbound` | `To:` の最初のドメイン | 受信フィルタ（誰宛てのメールか） |
| `outbound` | `From:` のドメイン | 送信フィルタ（誰が送るメールか） |

### workers（ルート内の設定）

`route.yaml` の `workers` セクションはルートごとの有効・無効・タイムアウト・実行順序を定義する。
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
timeout_seconds: 30
chunk_deadline_extension_seconds: 10   # チャンク転送ごとのデッドライン延長幅（秒）
```

#### dlp-worker.yaml（Apache Tika DLP）

```yaml
tika_url: "http://tika:9998"
timeout_seconds: 30
max_response_bytes: 10485760       # Tika レスポンス読み取り上限（省略時 10MB）
default_pattern_score: 50          # score 未指定パターンのデフォルトスコア
patterns:
  - name: credit_card
    regex: '\b(?:\d{4}[- ]?){3}\d{4}\b'
    score: 80
  - name: my_number_jp
    regex: '\b\d{4}[-\s]?\d{4}[-\s]?\d{4}\b'
    score: 80
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
    # destination: "mailpit:1025"   # 省略時は deliverers.default（未定義なら reinject 設定）が使われる
    # destination: "sendgrid"        # deliverer 名も指定できる（mailshield.yaml の deliverers 参照）
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

| キー | 型 | 環境変数 | 説明 |
|-----|-----|---------|------|
| `backend` | string | - | `redis` または `mariadb`。`mariadb` を選ぶと Redis 不要でセッション / OTP / パスワードリセットを MariaDB に保存する |
| `host` | string | `REDIS_HOST` | Redis ホスト名（`redis` バックエンド専用） |
| `port` | int | `REDIS_PORT` | Redis ポート番号（`redis` バックエンド専用） |
| `db` | int | - | 使用する DB インデックス（デフォルト: 1） |

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

認証は2つの独立した軸で決まる。

| 軸 | 値 | 意味 |
|-----|-----|------|
| `directory.source`（後述） | `none` / `ldap` / `scim` | **ユーザー情報の真実の源**とローカルログイン手段の選択 |
| `auth.sso_mode` | `disabled` / `optional` / `required` | **OIDC（SSO）の扱い** |

`directory.source` ごとの「ローカルログイン」の実体:

| `directory.source` | ローカルログイン手段 |
|---|---|
| `none`（デフォルト） | standalone（メール + bcrypt パスワード） |
| `ldap` | LDAP bind 認証（`directory.ldap` の接続設定を認証にも流用する） |
| `scim` | なし（SCIM はパスワード検証の仕組みを持たないため、`auth.sso_mode` を `optional`/`required` にすることが起動時に必須。`disabled` のままだと起動エラーになる） |

`auth.sso_mode` の意味:

| 値 | 動作 |
|---|---|
| `disabled`（デフォルト） | OIDC を使わない。`directory.source` が決めたローカルログインのみ |
| `optional` | ローカルログイン + OIDC の両方を提示する |
| `required` | OIDC のみ提示する。ローカルログイン（standalone/LDAP bind）は無効化される（`auth.providers.oidc.issuer` の設定が無いと起動時エラーになる） |

| キー | 型 | 説明 |
|-----|-----|------|
| `sso_mode` | string | 上表参照。省略時 `disabled` |
| `providers.oidc.issuer` | string | OIDC プロバイダーの issuer URL |
| `providers.oidc.client_id` | string | OIDC クライアント ID |
| `providers.oidc.client_secret` | string | OIDC クライアントシークレット（環境変数 `OIDC_CLIENT_SECRET` で設定推奨） |
| `providers.oidc.redirect_uri` | string | OIDC コールバック URI |
| `providers.oidc.scopes` | []string | 要求するスコープ |
| `group_mappings.admin` | string | admin ロールにマッピングする OIDC グループ名 |
| `group_mappings.operator` | string | operator ロールにマッピングする OIDC グループ名 |
| `group_mappings.viewer` | string | viewer ロールにマッピングする OIDC グループ名 |
| `session.ttl_minutes` | int | セッション有効期間（分） |
| `session.cookie_name` | string | セッション Cookie 名 |
| `session.cookie_secure` | bool | Secure 属性を付与するか（本番環境では `true` 推奨） |

```yaml
# 例1: デフォルト（standalone のみ）
auth:
  sso_mode: disabled

# 例2: standalone + OIDC の両方を提示（ユーザーが選べる）
auth:
  sso_mode: optional
  providers:
    oidc:
      issuer: "https://idp.example.com/application/o/mailshield/"
      # ...

# 例3: OIDC を強制（standalone/LDAP bind は無効化）
auth:
  sso_mode: required
  providers:
    oidc:
      issuer: "https://idp.example.com/application/o/mailshield/"
      # ...
```

### directory.ldap

LDAP ディレクトリ（Active Directory / OpenLDAP 等）を `directory.source: ldap` の真実の源として使う。この接続設定は次の2つの機能で共用される:

1. **定期同期**（`internal/directory/ldap.Syncer`）: バックグラウンドでユーザー・グループを `users` テーブルに反映する
2. **bind 認証**（`internal/auth.LDAPBindProvider`）: ログインのたびに LDAP へ bind してパスワードを検証し、JIT でユーザーを反映する（`auth.sso_mode` が `required` でなければ有効）

| キー | 型 | デフォルト | 説明 |
|-----|-----|----------|------|
| `directory.source` | string | `none` | `none` / `ldap` / `scim`。`ldap` のときのみ本セクションが使われる |
| `directory.ldap.host` | string | — | LDAP サーバーホスト |
| `directory.ldap.port` | int | `389` | LDAP サーバーポート |
| `directory.ldap.tls` | string | `none` | `none`（平文）/ `starttls` / `ldaps`（暗黙 TLS） |
| `directory.ldap.tls_skip_verify` | bool | `false` | TLS 証明書検証をスキップ（開発・テスト用） |
| `directory.ldap.bind_dn` | string | — | 検索用サービスアカウントの DN |
| `directory.ldap.bind_password` | string | — | サービスアカウントのパスワード。環境変数 `LDAP_BIND_PASSWORD` で設定推奨 |
| `directory.ldap.base_dn` | string | — | ユーザー検索の起点 DN |
| `directory.ldap.user_filter` | string | — | ユーザーを絞り込む LDAP 検索フィルタ（例: `(objectClass=person)`） |
| `directory.ldap.attributes.email` | string | — | メールアドレス属性名（例: `mail`） |
| `directory.ldap.attributes.display_name` | string | — | 表示名属性名（例: `displayName`） |
| `directory.ldap.attributes.groups` | string | — | グループ所属を表す属性名。Active Directory は標準で `memberOf` が使える。OpenLDAP は `memberof` overlay の有効化が必要 |
| `directory.ldap.group_mappings.admin/operator/viewer` | string | — | ロールにマッピングするグループの DN（`attributes.groups` で得られる値と同じ形式） |
| `directory.ldap.sync_interval_minutes` | int | `60` | 定期同期の間隔（分） |
| `directory.ldap.search_timeout_seconds` | int | `30` | 1 回の LDAP 検索のタイムアウト（秒） |
| `directory.ldap.page_size` | int | `500` | LDAP ページング検索の 1 ページあたり件数。AD 等のサーバー側件数上限（既定 1000 件）を超える規模のディレクトリでも全件取得するために使用 |
| `directory.ldap.deactivate_missing_users` | bool | `false` | 同期結果に含まれなくなった `provisioned_by=ldap` のユーザーを `is_active=0` にする。LDAP 検索が0件を返した場合（誤設定の可能性）は全ユーザー無効化を防ぐため何もしない |
**role の権威順位:** `manual > ldap/scim > oidc`。LDAP 同期で作成・更新されたユーザー（`provisioned_by=ldap`）の role は、その後の OIDC ログインでは上書きされない。詳細は [API 認証仕様](api-authentication.md) を参照。

#### directory.ldap.mailbox_provisioning

メールボックス割り当て（member/owner/admin）をディレクトリ構造から自動反映する（任意）。`rules` はルールのリストで、各ルールは `role`（`member` / `owner` / `admin`）と `method`（解決方式）を持つ。**同じ role に複数のルールを書ける**（全ルールの解決結果が合算される）。設定に書くのは「どこを見るか・属性名が何を意味するか」という構造情報だけで、個々のメールボックス名やグループ名は書かない。

**メールボックスは「すべての有効なメールアドレス」分が必要になる点に注意。** viewer ロールのユーザーが自分宛の隔離メールを閲覧・解放するには、そのユーザーの個人メールアドレス自体がメールボックスとして登録され、本人が割り当てられている必要がある。このため典型構成では「個人メールボックス（`source_attribute: mail` で自分のアドレスを自分に割り当てる）」と「共有メールボックス（memberOf やグループ検索から解決）」のルールを併記する。

| method | 概要 | 使いどころ |
|---|---|---|
| `user_attribute` | ユーザー自身の属性（`memberOf` 等）から解決する有界パイプライン | 汎用。将来の SCIM でも同じ考え方が使える |
| `group_search` | メールボックスを表すグループを一括検索し、グループの `member_attr`（DN 一覧）を対象ユーザーとする | Exchange 連携 AD（配布グループの `mail`/`managedBy` 属性をそのまま使える）。`memberOf` overlay が無い素の OpenLDAP でも動く |
| `fixed` | 列挙したユーザーへ、ldap 管理下の全メールボックスに対して当該ロールを一括付与 | 全メールボックスを見られる管理者の決め打ち |

**`user_attribute` のパイプライン**（`source_attribute` → `source_transform`? → `dereference`?（最大1回） → `target_attribute` → `target_transform`?）:

| キー | 説明 |
|-----|------|
| `source_attribute` | 必須。ユーザーエントリのどの属性を読むか。`mail` を指定すると自分のメールアドレス = 個人メールボックス（変換・再検索不要）、`memberOf` ならグループ経由の解決になる。複数値なら 1 件ずつ処理 |
| `source_transform` | 任意。値に適用する正規表現。**マッチしない値はスキップ**されるため、無関係なグループを除外するフィルタを兼ねる。抽出値は名前付きグループ `(?P<value>...)` > 最初のキャプチャ > マッチ全体の順 |
| `dereference.base_dn` / `dereference.filter` | 任意（最大1回の再検索）。`filter` の `{value}` プレースホルダに前段の値が埋め込まれる。**埋め込み値は必ず LDAP エスケープされる**（インジェクション対策・無効化不可）。同一クエリは同期サイクル内でキャッシュされ、N+1 を防ぐ |
| `target_attribute` | `dereference` 使用時は必須。再検索でヒットしたエントリから読む属性（例: `mail`）。`dereference` 無しでは指定不可（source の値がそのまま使われる） |
| `target_transform` | 任意。最終値に適用する正規表現 |
| `mailbox_domain` | 任意。最終値に `@` が無い場合 `値@mailbox_domain` を組み立てる |

**`group_search` のキー:** `base_dn`・`filter`（グループの検索条件）、`member_attr`（メンバー DN の属性。例: `member`、owner 用途なら `managedBy` 等）、`target_attribute`/`target_transform`/`mailbox_domain`（グループエントリ自身からメールボックスアドレスを取り出す）。member DN とユーザー DN の突き合わせは正規化（大文字小文字・空白ゆれ吸収）して行う。

**`fixed` のキー:** `fixed_value`（カンマまたはセミコロン区切りのユーザーメールアドレス。大文字小文字は無視して一致）。対象ユーザーには `provisioned_by=ldap` かつ有効な**全メールボックス**へ当該ロールが付与される。定期同期では第2パス（他ユーザーの反映でメールボックスが出揃った後）に処理される。

```yaml
directory:
  source: ldap
  ldap:
    host: ldap.corp.local
    port: 389
    tls: starttls
    bind_dn: "cn=svc-mailshield,ou=Service Accounts,dc=corp,dc=local"
    bind_password: ""              # ENV: LDAP_BIND_PASSWORD
    base_dn: "ou=Users,dc=corp,dc=local"
    user_filter: "(objectClass=person)"
    attributes:
      email: mail
      display_name: displayName
      groups: memberOf
    group_mappings:
      admin: "cn=MailShield-Admins,ou=Groups,dc=corp,dc=local"
      operator: "cn=MailShield-Operators,ou=Groups,dc=corp,dc=local"
      viewer: "cn=MailShield-Viewers,ou=Groups,dc=corp,dc=local"
    sync_interval_minutes: 60
    deactivate_missing_users: true
    mailbox_provisioning:
      rules:
        # 個人メールボックス: 各ユーザー自身の mail 属性 → 本人が owner
        # （user01 宛の隔離メールを user01 自身が閲覧・解放できるようにする基本設定）
        - role: owner
          method: user_attribute
          source_attribute: mail
        # 共有メールボックス: memberOf → グループ再検索 → グループの mail 属性 → member
        - role: member
          method: user_attribute
          source_attribute: memberOf
          source_transform: '^cn=(?P<value>[^,]+),ou=Groups.*$'
          dereference:
            base_dn: "ou=Groups,dc=corp,dc=local"
            filter: "(cn={value})"
          target_attribute: mail
        # mail 属性つきグループの owner 属性（AD なら managedBy）→ owner
        - role: owner
          method: group_search
          base_dn: "ou=Groups,dc=corp,dc=local"
          filter: "(mail=*)"
          member_attr: owner
          target_attribute: mail
        # 固定の管理者
        - role: admin
          method: fixed
          fixed_value: "admin@internal.dev"
```

`mailbox_provisioning` の詳細な設計思想（自動作成の可否・権威モデル・方式選択の考え方）は [API 認証仕様のメールボックス割り当て自動反映](api-authentication.md#メールボックス割り当ての自動反映ldap-mailbox_provisioning) を参照。

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
