# API 認証仕様

api-server は2種類の認証方式をサポートする。Cookie セッションを優先し、Cookie がなければ Bearer API キーで認証する。

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

### ログインフロー（スタンドアロン認証）

```mermaid
sequenceDiagram
    participant Browser as ブラウザ
    participant API as api-server
    participant Redis

    Browser->>API: POST /api/v1/auth/login<br>{ "email": "...", "password": "..." }
    API->>Redis: セッション保存
    API-->>Browser: 200 OK + Set-Cookie: mailshield_session=&lt;session_id&gt;
```

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
| `POST /api/v1/auth/login` | スタンドアロンログイン |
| `GET /api/v1/auth/login/oidc` | OIDC ログイン開始 |
| `GET /api/v1/auth/callback` | OIDC コールバック |
| `POST /api/v1/auth/setup` | 初期管理者作成 |
| `POST /api/v1/auth/forgot-password` | パスワードリセット申請 |
| `POST /api/v1/auth/reset-password` | パスワードリセット実行 |
| `GET /api/v1/public/attachments/*` | 添付ファイルダウンロード（OTP 認証） |
| `POST /api/v1/public/attachments/*/otp/*` | OTP 申請・検証 |
