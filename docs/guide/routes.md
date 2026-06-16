# ルーティング設定ガイド

MailShield は受信・送信の区別をルート定義で行います。
MAIL FROM / RCPT TO の正規表現で動的に判定し、ルートごとに異なるワーカーとポリシーを適用できます。

---

## 仕組み

```
SMTP 接続到着（port 10025）
    ↓
MAIL FROM, RCPT TO を解析
    ↓
routes を上から順に評価（first-match-wins）
    ↓
マッチしたルートのワーカー・ポリシーを適用
```

---

## 設定の場所

`config/mailshield.yaml` の `routes:` セクションで定義します。

```yaml
routes:
  - name: inbound
    direction: inbound
    match:
      to: "@example\\.com$"
      to_match: any
    workers:
      ...
    policy:
      rules_file: /app/config/policy-inbound.yaml

  - name: outbound
    direction: outbound
    match:
      from: "@example\\.com$"
    workers:
      ...
    policy:
      rules_file: /app/config/policy-outbound.yaml
```

---

## ルートパラメータ

### `name`

ルートの識別名。ログや統計に記録されます。

### `direction`

| 値 | 意味 |
|---|------|
| `inbound` | 外部 → 内部ドメイン（受信） |
| `outbound` | 内部ドメイン → 外部（送信） |
| `internal` | 内部 → 内部 |

添付ファイルのダウンロード認証方式（`attachment_download.flows`）は `direction` の値で振り分けます。

### `match`

| フィールド | 説明 |
|-----------|------|
| `from` | MAIL FROM を評価する正規表現（省略すると全アドレスにマッチ） |
| `to` | RCPT TO を評価する正規表現（省略すると全アドレスにマッチ） |
| `to_match` | `any`（いずれか 1 つが一致）または `all`（全員が一致）。デフォルトは `any` |

正規表現は Go の `regexp` パッケージ（RE2 構文）を使います。

```yaml
match:
  to: "@(example\\.com|example\\.org)$"   # 複数ドメイン
  from: "^noreply@"                         # 特定の送信者
  to_match: all                             # 宛先が全員内部ドメイン
```

### `policy`

```yaml
policy:
  rules_file: /app/config/policy-inbound.yaml
```

ルートごとに異なるポリシーファイルを指定できます。

---

## 典型的な構成例

### 受信のみ（シンプル）

```yaml
routes:
  - name: inbound
    direction: inbound
    match:
      to: "@example\\.com$"
    workers:
      inspect:
        - name: av-worker
          enabled: true
          timeout_seconds: 30
      transform:
        - name: sanitize-worker
          enabled: true
          order: 1
    policy:
      rules_file: /app/config/policy-inbound.yaml
```

### 受信 + 送信

```yaml
routes:
  # 内部ドメイン宛て（受信）
  - name: inbound
    direction: inbound
    match:
      to: "@example\\.com$"
    workers:
      inspect:
        - name: av-worker
          enabled: true
          timeout_seconds: 30
      transform:
        - name: sanitize-worker
          enabled: true
          order: 1
    policy:
      rules_file: /app/config/policy-inbound.yaml

  # 内部ドメイン発（送信）
  - name: outbound
    direction: outbound
    match:
      from: "@example\\.com$"
    workers:
      inspect:
        - name: dlp-worker
          enabled: true
          timeout_seconds: 60
      transform:
        - name: filesep-worker
          enabled: true
          order: 1
    policy:
      rules_file: /app/config/policy-outbound.yaml
```

### 複数ドメイン

```yaml
routes:
  - name: inbound
    direction: inbound
    match:
      to: "@(example\\.com|example\\.org|subsidiary\\.example\\.net)$"
    ...
```

---

## マッチしないメールの扱い

どのルートにもマッチしないメールは、smtp-gateway が `451 Try again later` を返して
送信元 MTA のキューに残します。設定漏れを防ぐため、必要に応じてキャッチオールを末尾に追加してください。

```yaml
routes:
  - name: inbound
    ...

  - name: catchall
    direction: inbound
    # match を省略すると全メールにマッチ
    policy:
      rules_file: /app/config/policy-catchall.yaml
```
