# REST API リファレンス

api-server（デフォルト port 8081）が提供する REST API の全エンドポイントを説明します。

## 認証

| 方式 | ヘッダ / Cookie | 説明 |
|-----|----------------|------|
| セッション Cookie | `mailshield_session=...` | Web UI 経由でのログイン後に発行 |
| API キー | `Authorization: Bearer <api_key>` | 管理画面で発行。機械間連携向け |

セッション Cookie が優先されます。API キーはセッションがない場合にフォールバックします。

## ロール

| ロール | 権限 |
|-------|------|
| `viewer` | 閲覧のみ |
| `operator` | 閲覧 + 隔離解放・削除・一括操作 |
| `admin` | 全操作（ユーザー管理・API キー発行等を含む） |

---

## ヘルスチェック

### `GET /healthz`

認証不要。サービスの死活確認。

**レスポンス:**
```json
{"status": "ok"}
```

---

## 認証

### `GET /api/v1/auth/providers`

利用可能な認証プロバイダー一覧を返す。認証不要。

**レスポンス:**
```json
[
  {"type": "standalone"},
  {"type": "oidc", "name": "Google", "login_url": "/api/v1/auth/login/oidc"}
]
```

### `POST /api/v1/auth/login`

スタンドアロン認証（メール + パスワード）。認証不要。

**リクエスト:**
```json
{"email": "admin@example.com", "password": "secret"}
```

**レスポンス:** セッション Cookie を発行。`200 OK` または `401 Unauthorized`。

### `POST /api/v1/auth/setup`

初回セットアップ（管理者アカウント作成）。ユーザーが0人の場合のみ有効。

**リクエスト:**
```json
{"email": "admin@example.com", "password": "secret", "name": "Admin"}
```

### `POST /api/v1/auth/logout`

セッション破棄。認証必要。

### `GET /api/v1/auth/me`

ログイン中ユーザーの情報を返す。認証必要。

**レスポンス:**
```json
{"id": "uuid", "email": "admin@example.com", "name": "Admin", "role": "admin"}
```

### `POST /api/v1/auth/forgot-password`

パスワードリセットメールを送信。認証不要。ユーザーが存在しない場合も `200 OK`（列挙防止）。

**リクエスト:**
```json
{"email": "user@example.com"}
```

### `POST /api/v1/auth/reset-password`

ワンタイムトークンでパスワードをリセット。認証不要。

**リクエスト:**
```json
{"token": "xxx", "password": "new_password"}
```

---

## 統計

### `GET /api/v1/stats`

ダッシュボード用の処理統計。`viewer` 以上。

**レスポンス:**
```json
{
  "total": 1234,
  "delivered": 1100,
  "quarantined": 120,
  "rejected": 14,
  "last_24h": {"total": 56, "quarantined": 3}
}
```

---

## メッセージ

### `GET /api/v1/messages`

メール処理ログ一覧。`viewer` 以上。

**クエリパラメータ:**

| パラメータ | 説明 |
|-----------|------|
| `page` | ページ番号（デフォルト 1） |
| `limit` | 件数（デフォルト 20） |
| `status` | `delivered` / `quarantined` / `rejected` 等でフィルタ |
| `direction` | `inbound` / `outbound` |

### `GET /api/v1/messages/{id}`

メッセージ詳細（検査結果・変換結果・ポリシー判定を含む）。`viewer` 以上。

### `GET /api/v1/messages/{id}/attachments`

分離された添付ファイルの一覧。`viewer` 以上。

### `GET /api/v1/messages/{id}/eml`

原本 EML のダウンロード URL（MinIO presigned URL）。`operator` 以上。

---

## 隔離

### `GET /api/v1/quarantine`

隔離中メール一覧。`viewer` 以上。可視性フィルタ適用。

**クエリパラメータ:** `messages` と同様。

### `GET /api/v1/quarantine/{id}`

隔離メール詳細。`viewer` 以上。

### `POST /api/v1/quarantine/{id}/release`

1件解放。`viewer` 以上（解放権限はメールボックスポリシーによる）。

**レスポンス:**
```json
{"status": "delivered"}
```

### `DELETE /api/v1/quarantine/{id}`

1件削除。`operator` 以上。

### `POST /api/v1/quarantine/bulk-release`

一括解放。`operator` 以上。最大 100 件。

**リクエスト:**
```json
{"ids": ["uuid1", "uuid2"]}
```

**レスポンス:**
```json
{"succeeded": ["uuid1"], "failed": [{"id": "uuid2", "error": "not found"}]}
```

### `DELETE /api/v1/quarantine/bulk`

一括削除。`operator` 以上。最大 100 件。

**リクエスト:**
```json
{"ids": ["uuid1", "uuid2"]}
```

---

## 添付ファイル（認証済みユーザー向け）

### `GET /api/v1/attachments/{token}`

ダウンロードトークンに紐づく添付ファイル一覧。`viewer` 以上。

### `GET /api/v1/attachments/{token}/{filename}`

添付ファイルダウンロード。`viewer` 以上。

### `PATCH /api/v1/attachments/{id}/disable`

ダウンロードを無効化。`operator` 以上。

### `DELETE /api/v1/attachments/{id}`

添付ファイルを削除（MinIO から削除）。`operator` 以上。

---

## 添付ファイル（ゲスト・OTP 認証）

送信メールの添付ファイルを外部受信者がダウンロードするためのエンドポイント。認証不要。

### `GET /api/v1/public/attachments/{token}`

添付ファイル一覧（OTP 検証後のセッションが必要な場合あり）。

### `GET /api/v1/public/attachments/{token}/{filename}`

添付ファイルダウンロード。

### `POST /api/v1/public/attachments/{token}/otp/request`

OTP コードをメールで送信。

**リクエスト:**
```json
{"email": "recipient@external.example"}
```

### `POST /api/v1/public/attachments/{token}/otp/verify`

OTP コードを検証してダウンロードセッションを発行。

**リクエスト:**
```json
{"email": "recipient@external.example", "code": "123456"}
```

**レスポンス:**
```json
{"otp_session": "session_token"}
```

`?otp_session=<token>` クエリパラメータを付けてダウンロード URL にアクセスします。

---

## ユーザー管理（admin のみ）

### `GET /api/v1/users`
### `POST /api/v1/users`

**リクエスト:**
```json
{"email": "user@example.com", "name": "User Name", "role": "operator", "password": "secret"}
```

### `PATCH /api/v1/users/{id}`

```json
{"name": "New Name", "role": "admin"}
```

### `DELETE /api/v1/users/{id}`

---

## メールボックス管理（operator / admin）

### `GET /api/v1/mailboxes`
### `POST /api/v1/mailboxes`

```json
{"address": "sales@example.com", "display_name": "Sales"}
```

### `PATCH /api/v1/mailboxes/{id}`
### `DELETE /api/v1/mailboxes/{id}`
### `GET /api/v1/mailboxes/{id}/assignments`

メールボックスに割り当てられたユーザー一覧。

### `POST /api/v1/mailboxes/{id}/assignments`

```json
{"user_id": "uuid", "role": "member"}
```

### `DELETE /api/v1/mailboxes/{id}/assignments`

```json
{"user_id": "uuid"}
```

---

## 監査ログ（admin のみ）

### `GET /api/v1/audit-logs`

**クエリパラメータ:**

| パラメータ | 説明 |
|-----------|------|
| `event_type` | 前方一致フィルタ（例: `quarantine.`） |
| `actor_id` | 操作ユーザー UUID |
| `from` | 開始日時（ISO 8601） |
| `to` | 終了日時（ISO 8601） |

---

## API キー管理（admin のみ）

### `GET /api/v1/api-keys`

発行済み API キー一覧（ハッシュ値のみ。平文は返らない）。

### `POST /api/v1/api-keys`

新しい API キーを発行。レスポンスの `key` は **この一度きり** 表示される。

**リクエスト:**
```json
{"name": "SIEM Integration", "role": "viewer", "expires_at": "2026-12-31T00:00:00Z"}
```

**レスポンス:**
```json
{"id": "uuid", "name": "SIEM Integration", "key": "ms_xxxxxxxxxxxx"}
```

### `DELETE /api/v1/api-keys/{id}`

API キーを失効させる。
