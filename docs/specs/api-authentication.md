# API 認証仕様

api-server は2種類の認証方式をサポートする。Cookie セッションを優先し、Cookie がなければ Bearer API キーで認証する。

---

## 認証の2軸（directory.source × auth.sso_mode）

どのログイン手段が有効かは、独立した2つの設定軸で決まる（設定リファレンスは [configuration.md#auth](configuration.md#auth) を参照）。

|  | 意味 |
|---|---|
| `directory.source`（`none`\|`ldap`\|`scim`） | **ユーザー情報の真実の源**。同時に「ローカルログイン」の実体も決める |
| `auth.sso_mode`（`disabled`\|`optional`\|`required`） | **OIDC（SSO）の扱い** |

`directory.source` ごとの「ローカルログイン」:

| `directory.source` | ローカルログイン | 備考 |
|---|---|---|
| `none` | standalone（bcrypt） | 手動でユーザー作成。パスワードリセット可 |
| `ldap` | LDAP bind 認証 | `directory.ldap` の接続設定を流用。パスワードの真実の源は LDAP 側 |
| `scim` | なし | SCIM はパスワード検証の仕組みを持たないため、`auth.sso_mode` が `disabled` だとログイン手段が0になり起動時エラーになる |

`auth.sso_mode` は上記のローカルログインに OIDC を足す/置き換える:
- `disabled`: ローカルログインのみ
- `optional`: ローカルログイン + OIDC の両方を提示
- `required`: OIDC のみ（ローカルログインは無効化）

---

## 認証フロー

```mermaid
flowchart TD
    A(["リクエスト受信"]) --> B{"Cookie に\nmailshield_session\nが存在するか？"}
    B -->|Yes| C["Redis でセッション検索"]
    C -->|有効| Pass(["✓ 認証通過"])
    C -->|"無効・期限切れ"| E

    B -->|No| E{"Authorization: Bearer\nヘッダが存在するか？"}
    E -->|Yes| F["SHA-256(key) で DB を検索"]
    F -->|"IsActive() = true"| G["last_used_at を非同期更新"]
    G --> Pass
    F -->|"無効・失効・期限切れ"| Fail

    E -->|No| Fail(["401 UNAUTHORIZED"])
```

---

## セッション認証（ブラウザ向け）

### メール・パスワードでのログイン（ローカルログイン）

`POST /api/v1/auth/login` は1つのエンドポイントだが、裏側の検証方式は `directory.source` によって standalone（bcrypt）と LDAP bind 認証のどちらか一方に決まる（両方が同時に有効になることはないため、フロントエンドはどちらが動いているか意識しなくてよい）。

```mermaid
sequenceDiagram
    participant Browser as ブラウザ
    participant API as api-server
    participant Redis

    Browser->>API: POST /api/v1/auth/login<br>{ "email": "...", "password": "..." }
    API->>Redis: セッション保存
    API-->>Browser: 200 OK + Set-Cookie: mailshield_session=&lt;session_id&gt;
```

**`directory.source: ldap` のときの内部動作（search+bind パターン）:**

```mermaid
sequenceDiagram
    participant API as api-server
    participant LDAP as LDAP サーバー

    API->>LDAP: ① サービスアカウントで bind
    API->>LDAP: ② base_dn 配下を email で検索
    LDAP-->>API: エントリ 1 件（DN・属性）
    API->>LDAP: ③ 見つかった DN + 入力パスワードで bind
    alt bind 成功
        LDAP-->>API: 成功
        API->>API: ④ memberOf → role 解決 → Provisioner で JIT
    else bind 失敗 / 検索 0件・複数件
        LDAP-->>API: 失敗
        API-->>API: 統一エラー（ユーザー列挙を防ぐ）
    end
```

パスワードそのものは api-server 側で保持・比較しない。検証は常に LDAP サーバーの bind 処理に委ねる。

### ログインフロー（OIDC）

```mermaid
sequenceDiagram
    participant Browser as ブラウザ
    participant API as api-server
    participant IDP as OIDC プロバイダー

    Browser->>API: GET /api/v1/auth/login/oidc
    API-->>Browser: 302 Redirect → OIDC プロバイダー
    Browser->>IDP: 認証
    IDP-->>Browser: 302 Redirect → /api/v1/auth/callback
    Browser->>API: GET /api/v1/auth/callback
    API-->>Browser: 200 OK + Set-Cookie: mailshield_session=&lt;session_id&gt;
```

### OIDC ログイン時の JIT プロビジョニング

OIDC でのログイン成功時、`users` テーブルへユーザー行を作成・更新する（`internal/directory.Provisioner` 経由）。これにより OIDC ユーザーもメールボックス割り当て・承認者設定の対象になる（`mailbox_assignments.user_id` / `approval_requests.approver_id` は `users.id` への外部キーのため、DB 行が存在しないと対象にできない）。この `Provisioner` は LDAP 同期・SCIM push とも共通の入口であり、グループ→ロールの解決も `GroupRoleMapper` として共通化されている（詳細は `internal/directory/` を参照）。

**役割分担の考え方:** OIDC の groups claim は「本人確認(認証)のついでに得られる簡易な権限情報」であり、LDAP/SCIM ディレクトリ同期が無い環境向けのフォールバックと位置付ける。`manager` 属性のような、OIDC の claim には通常載らない情報（承認者の自動解決に使う想定。`docs/PLAN.md` フェーズ4参照）は LDAP/SCIM 側が真実の源になる。このため role の上書き可否は権威の優先順位で決まる:

```
manual（Web UI 手動作成・編集） > ldap / scim（ディレクトリ同期） > oidc（groups claim。フォールバック）
```

既存行が上位または同格の権威で管理されている場合、下位の source からの role 上書きは行わない（例: `provisioned_by=ldap` の行に対する OIDC ログインは role を変更しない）。

| 項目 | 内容 |
|-----|------|
| 既存ユーザー判定 | メールアドレスで一致（`users.email` は UNIQUE） |
| 新規ユーザー | `role` は OIDC グループのマッピング結果、`provisioned_by = oidc` で作成 |
| 既存ユーザー（`provisioned_by` が上位権威: manual・ldap・scim） | `role`・`provisioned_by` は上書きしない。`display_name` のみ IdP の最新値に更新 |
| 既存ユーザー（`provisioned_by = oidc`） | `role` を OIDC グループのマッピング結果で更新（毎回最新の claim を反映） |
| 無効化ユーザー（`is_active = 0`） | ログイン拒否（`403 FORBIDDEN`）。OIDC ログインだけでは再有効化されない |
| `session.User.Sub` | OIDC トークンの `sub` クレームではなく、解決した `users.id` を格納する |

### LDAP ディレクトリ同期

`directory.ldap.enabled: true` の場合、api-server 起動時に `internal/directory/ldap.Syncer` がバックグラウンドで起動し、起動直後に1回・以後 `sync_interval_minutes` 間隔で LDAP ディレクトリと `users` テーブルを同期する。認証（OIDC/standalone ログイン）とは独立した処理であり、LDAP は「role・manager 等の権限属性の真実の源」としてのみ働く。

処理の流れ:

1. LDAP サーバーへ bind し、`base_dn` 配下で `user_filter` にマッチするエントリをページング検索（`SearchWithPaging`）で全件取得する。AD 等のサーバー側件数上限（既定 1000 件）を超えるディレクトリでも取りこぼさない
2. 各エントリの `attributes.groups`（例: `memberOf`）を `GroupRoleMapper` で role に解決し、OIDC と同じ `Provisioner.Provision` を呼んで `users` 行を作成・更新する（`provisioned_by = ldap`）
3. メールアドレス属性が空のエントリはスキップする
4. `deactivate_missing_users: true` の場合、今回の同期結果に含まれなかった `provisioned_by = ldap` の既存ユーザーを `is_active = 0` にする。ただし検索結果が0件（誤設定の可能性が高い）の場合は全ユーザー無効化を防ぐため何もしない

設定項目の詳細は [設定リファレンスの directory.ldap](configuration.md#directoryldap) を参照。

### セッション仕様

| 項目 | 内容 |
|-----|------|
| セッション ID | UUID（Redis のキー） |
| 保存先 | Redis |
| TTL | `auth.session.ttl_minutes`（デフォルト: 480分 = 8時間） |
| Cookie 名 | `auth.session.cookie_name`（デフォルト: `mailshield_session`） |
| Cookie Secure | `auth.session.cookie_secure`（本番では `true` 推奨） |

### ロール

| ロール | 説明 |
|-------|------|
| `admin` | 全操作可能。ユーザー管理・API キー管理・監査ログ閲覧を含む |
| `operator` | メールボックス管理・隔離操作（閲覧・解放・削除）が可能 |
| `viewer` | 自分のメールボックスに関連する隔離メールの閲覧・解放のみ可能 |

---

## API キー認証（機械間向け）

### 概要

外部システム（CI/CD・SIEM・SOAR・自動化スクリプト）が API を呼び出すために使用する。Cookie セッションは不要。

### キーの形式

```
mailshield_sk_<32バイトのランダム16進数>
```

例: `mailshield_sk_39b86cb373497b2e35f45e6313145785df90eb9fd61db3cadbe24c2659da3e01`

### 使い方

```bash
curl https://your-api-server/api/v1/stats \
  -H "Authorization: Bearer mailshield_sk_xxxxxxxxxxxx"
```

### セキュリティ設計

| 項目 | 仕様 |
|-----|------|
| DB 保存形式 | SHA-256 ハッシュのみ（平文は保存しない） |
| 平文の確認 | 発行時のレスポンスに一度だけ含まれる。以降は参照不可 |
| 失効 | `DELETE /api/v1/api-keys/{id}` で即時失効。失効後は認証不可 |
| 有効期限 | `expires_at`（ISO 8601）で設定可能。省略時は無期限 |
| ロール | `admin` / `operator` / `viewer` のいずれかを指定 |
| 最終使用日時 | 認証成功ごとに `last_used_at` を非同期更新 |

### API キー管理エンドポイント

すべて admin ロールが必要。

```
POST   /api/v1/api-keys         # 新規発行（平文キーを一度だけ返す）
GET    /api/v1/api-keys         # 一覧取得（失効済み含む）
DELETE /api/v1/api-keys/{id}    # 即時失効
```

#### POST /api/v1/api-keys リクエスト

```json
{
  "name": "CI/CD用",
  "role": "viewer",
  "expires_at": "2027-01-01T00:00:00Z"   // 任意
}
```

#### POST /api/v1/api-keys レスポンス

```json
{
  "id": "890cb6b3-...",
  "name": "CI/CD用",
  "role": "viewer",
  "created_by": "00000000-...",
  "expires_at": null,
  "created_at": "2026-06-15T14:13:19Z",
  "key": "mailshield_sk_xxxx"   // ← この項目はこのレスポンスにのみ含まれる
}
```

#### GET /api/v1/api-keys レスポンス

```json
{
  "data": [
    {
      "id": "890cb6b3-...",
      "name": "CI/CD用",
      "role": "viewer",
      "created_by": "00000000-...",
      "last_used_at": "2026-06-15T14:13:27Z",
      "expires_at": null,
      "revoked_at": null,
      "created_at": "2026-06-15T14:13:19Z"
    }
  ],
  "meta": { "total": 1 }
}
```

---

## エンドポイント別必要ロール

| エンドポイント | 最低必要ロール |
|-------------|------------|
| `GET /api/v1/stats` | viewer |
| `GET /api/v1/messages/*` | viewer |
| `GET /api/v1/quarantine/*` | viewer（メールボックス可視性フィルタあり） |
| `POST /api/v1/quarantine/{id}/release` | viewer（メールボックスロール確認あり） |
| `DELETE /api/v1/quarantine/*` | operator |
| `POST /api/v1/quarantine/bulk-*` | operator |
| `GET /api/v1/attachments/*` | viewer |
| `PATCH/DELETE /api/v1/attachments/*` | operator |
| `GET/POST/PATCH/DELETE /api/v1/mailboxes/*` | operator |
| `GET/POST/PATCH/DELETE /api/v1/users/*` | admin |
| `GET /api/v1/audit-logs` | admin |
| `GET/POST/DELETE /api/v1/api-keys/*` | admin |

### 認証不要のエンドポイント

| エンドポイント | 説明 |
|-------------|------|
| `GET /healthz` | ヘルスチェック |
| `GET /api/v1/auth/providers` | 認証プロバイダー一覧 |
| `POST /api/v1/auth/login` | メール・パスワードでのログイン |
| `GET /api/v1/auth/login/oidc` | OIDC ログイン開始 |
| `GET /api/v1/auth/callback` | OIDC コールバック |
| `POST /api/v1/auth/setup` | 初期管理者作成 |
| `POST /api/v1/auth/forgot-password` | パスワードリセット申請 |
| `POST /api/v1/auth/reset-password` | パスワードリセット実行 |
| `GET /api/v1/public/attachments/*` | 添付ファイルダウンロード（OTP 認証） |
| `POST /api/v1/public/attachments/*/otp/*` | OTP 申請・検証 |
