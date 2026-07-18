# REST API リファレンス

api-server のデフォルトポートは 8090。

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

policy アクション `approval` で保留されたメールの承認 API。承認者はメールボックス割り当てで決まる:

- **メールボックス承認**（主・`mailbox_emails` が非空）: 対象メールボックス（1..n）のいずれかに role=approver で割り当てられたユーザー全員が決定できる（先に決定した人が有効）。受信メールで宛先が複数の場合、approver がいるすべての宛先メールボックスが対象になる
- **グローバルフォールバック**（`approver_id` が非 null）: メールボックスに承認者がいない場合のみ、`approval.global_approver_email` で指定した 1 名が承認者になる（任意・デフォルト無効）

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

> [!NOTE]
> 承認者はメールボックスの `role=approver` 割り当てで決まる。メールボックス管理 API
> （`/api/v1/mailboxes/{id}/assignments`）で設定する。ユーザー個人に承認者を指定する
> 方式（旧 `users.approver_id` / `/users/{id}/approver`）は廃止された。

---

## 送信ディレイ（送信待ち）

policy アクション `delay` で保留された送信メールの管理 API。保留時間を過ぎると自動送信されるが、それまでは取消・即時送信できる。

### `GET /api/v1/delayed`

送信待ち（pending）一覧。`viewer` は自分が送信者（送信元メールボックスの owner）のメールのみ。`operator` / `admin` は全件。

**レスポンス:**
```json
{
  "items": [
    {
      "id": "uuid",
      "message_id": "uuid",
      "release_at": "2026-07-15T10:05:00Z",
      "status": "pending",
      "from_address": "me@corp.example",
      "to_addresses": ["ext@example.com"],
      "subject": "見積書送付",
      "has_attachment": true
    }
  ]
}
```

### `POST /api/v1/delayed/{id}/send-now`

保留時間を待たずに今すぐ送信する。`viewer` は自分が送信者のもののみ。処理済みの場合は `409`。

### `POST /api/v1/delayed/{id}/cancel`

送信を取り消す（配送しない・メールは `rejected` になる）。権限は send-now と同じ。

> [!NOTE]
> 自動送信・即時送信・取消はすべて `delayed_releases.status='pending'` の CAS（条件付き更新）で
> 取り出すため、複数レプリカ構成でも二重配送・競合しない。

---

## ポリシー編集

ルート（`config/routes.d/<route>/policy.yaml`）のルールを閲覧・編集する API。
閲覧は `operator` 以上、更新は `admin` のみ。更新は smtp-gateway に反映され、
反映に失敗した場合は変更を巻き戻す。

### `GET /api/v1/policy/routes`

全ルートのルール一覧とルール別ヒット件数を返す。`operator` 以上。

**レスポンス:**
```json
{
  "routes": [
    {
      "dir": "10-inbound",
      "name": "inbound",
      "direction": "inbound",
      "policy": {
        "rules": [
          {"name": "av_detected", "condition": "av-worker.detected == true", "action": "quarantine"},
          {"name": "default_deliver", "condition": "true", "action": "deliver"}
        ]
      }
    }
  ],
  "hits": {"inbound": {"av_detected": 3, "default_deliver": 120}}
}
```

### `GET /api/v1/policy/routes/{route}`

単一ルート（`{route}` はディレクトリ名。例: `10-inbound`）を返す。`operator` 以上。

### `GET /api/v1/policy/stats`

ルート×ルール別のヒット件数（smtp-gateway 起動時からの累積）。`operator` 以上。

### `PUT /api/v1/policy/routes/{route}`

ルールを更新する。`admin` のみ。リクエストボディは `{"rules": [...], "lists": {...}}`。

処理順序: 構造検証 → 変更前スナップショットを履歴保存 → `policy.yaml` 書き込み →
smtp-gateway に `POST /reload`。リロードに失敗した場合は元の内容へ書き戻し、
`422 RELOAD_FAILED` と gateway のパースエラーを返す。成功時は監査ログに `policy.updated` を記録する。

### `GET /api/v1/policy/routes/{route}/versions`

ポリシー変更履歴（新しい順・最大 50 件）を返す。`operator` 以上。各要素は変更前スナップショットのメタ情報（`id` / `actor_email` / `created_at`）。

### `POST /api/v1/policy/routes/{route}/rollback`

指定バージョンの内容へ復元する。`admin` のみ。リクエストボディは `{"version_id": "..."}`。
復元前に現在の内容も履歴に保存する。書き込み → `POST /reload`、失敗時は巻き戻す。
監査ログに `policy.rolled_back` を記録する。

**エラー:**
- `422 VALIDATION_ERROR` — 構造検証エラー（アクション種別・デフォルトルール欠如など。書き込み前に拒否）
- `422 RELOAD_FAILED` — smtp-gateway が新ポリシーを拒否（条件式の構文エラーなど。変更は巻き戻し済み）

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

メールボックス一覧。各要素に `assignment_summary`（role 別の割り当て人数 + 先頭 3 人）を含む。

```json
{
  "data": [
    {
      "id": "uuid", "email_address": "team@example.com", "display_name": "チーム共有", "is_active": true,
      "assignment_summary": [
        {"role": "member", "count": 15, "sample": [{"email": "a@x", "display_name": "A"}]},
        {"role": "owner", "count": 2, "sample": [{"email": "b@x", "display_name": "B"}]},
        {"role": "approver", "count": 1, "sample": [{"email": "c@x", "display_name": "C"}]}
      ]
    }
  ]
}
```

### `POST /api/v1/mailboxes`

```json
{"email_address": "sales@example.com", "display_name": "Sales"}
```

### `PATCH /api/v1/mailboxes/{id}`
### `DELETE /api/v1/mailboxes/{id}`
### `GET /api/v1/mailboxes/{id}/assignments`

メールボックスに割り当てられたユーザー一覧（全件）。

### `POST /api/v1/mailboxes/{id}/assignments`

`role` は `member`（受信担当）/ `owner`（送信担当）/ `approver`（承認担当）のいずれか。

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
