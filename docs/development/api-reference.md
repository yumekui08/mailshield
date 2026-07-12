# REST API リファレンス

api-server のデフォルトポートは 8081。

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

メール + パスワードでログイン。認証不要。

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

ダッシュボード用の処理統計。`viewer` 以上（viewer はメールボックス可視性でフィルタされる）。

**レスポンス:**
```json
{
  "today": {"delivered": 56, "quarantined": 3, "rejected": 1, "total": 60},
  "week":  {"delivered": 1100, "quarantined": 120, "rejected": 14, "total": 1234}
}
```

### `GET /api/v1/stats/timeseries`

日別の処理件数（ダッシュボードのチャート用）。`viewer` 以上（viewer はメールボックス可視性でフィルタされる）。

**クエリパラメータ:**

| パラメータ | 説明 |
|-----------|------|
| `days` | 取得日数（デフォルト 14・最大 90）。当日を含む UTC 日付単位 |

**レスポンス:** 古い日付から順。メールがない日も件数 0 で含まれる。
```json
{
  "data": [
    {"date": "2026-07-05", "delivered": 42, "quarantined": 2, "rejected": 1, "total": 45},
    {"date": "2026-07-06", "delivered": 56, "quarantined": 3, "rejected": 0, "total": 59}
  ]
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

## 承認フロー

policy アクション `approval` で保留されたメールの承認 API。承認依頼には 2 方式ある:

- **メールボックス承認**（`mailbox_emails` が非空）: 対象メールボックス（1..n）のいずれかに role=admin で割り当てられたユーザー全員が決定できる（先に決定した人が有効）。受信メールで宛先が複数の場合、admin がいるすべての宛先メールボックスが対象になる
- **個人承認**（`approver_id` が非 null）: 指定された承認者本人のみ決定できる

依頼作成時、承認者全員へ通知メールが送られる。通知は宛先ごとに送信状態が管理され、一部の宛先だけ失敗した場合は失敗した宛先のみ再送される（30 秒間隔・最大 5 回）。

### `GET /api/v1/approvals`

承認依頼一覧。`viewer` は「自分が承認できる pending の依頼」（個人承認で自分が承認者のもの + 自分が admin のメールボックス宛のもの）のみ。`operator` / `admin` は全件。

**レスポンス:**
```json
{
  "items": [
    {
      "id": "uuid",
      "message_id": "uuid",
      "approver_id": null,
      "mailbox_emails": ["sales@example.com"],
      "status": "pending",
      "expires_at": "2026-07-10T00:00:00Z",
      "created_at": "2026-07-07T00:00:00Z"
    }
  ]
}
```

### `GET /api/v1/approvals/{id}`

承認依頼詳細（対象メールの情報を含む）。`viewer` は自分が承認できる依頼のみ。

### `POST /api/v1/approvals/{id}/approve`

承認して配送する（EML を再インジェクト）。`viewer` は自分が承認できる依頼のみ。処理済みの依頼には `409` を返す。

**リクエスト（任意）:**
```json
{"comment": "確認済み"}
```

### `POST /api/v1/approvals/{id}/reject`

却下する（配送しない）。権限・リクエスト形式は approve と同じ。

### `GET/PUT /api/v1/users/{id}/approver`

ユーザー個人の承認者（`users.approver_id`）の取得・設定。`admin` のみ。
メールボックス承認者（role=admin の割り当て）はメールボックス管理 API（`/api/v1/mailboxes/{id}/assignments`）で管理する。

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
