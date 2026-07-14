# 統合イベント仕様（webhook）

smtp-gateway は統合イベントを **webhook（HTTP POST）** で外部システムへ通知できる。
SIEM・アーカイブ連携・自作コンシューマー向けのオプション機能であり、
**メールフローには一切影響しない**（発行に失敗してもログに記録して処理を続行する）。

イベントは2種類あり、`X-MailShield-Event` ヘッダーで区別する:

| イベント | 発行タイミング | 用途 |
|---------|--------------|------|
| `mail.received` | メール受信直後（EML 保存・DB 記録後） | 受信の記録・アーカイブ連携 |
| `mail.processed` | ポリシー評価後（配送/隔離/拒否/承認の決定後） | 最終アクション・ワーカースコアでの相関分析 |

> [!NOTE]
> 以前は RabbitMQ への発行をサポートしていたが、ADR 005 の方針（キューをメールフローに
> 置かない・導入障壁の最小化）に基づき削除し、webhook に置き換えた。

---

## 設定（mailshield.yaml）

```yaml
events:
  backend: webhook            # webhook | none（デフォルト: none）
  webhook:
    url: "https://siem.example.com/hooks/mailshield"   # ENV: MAILSHIELD_WEBHOOK_URL
    secret: ""                # ENV: MAILSHIELD_WEBHOOK_SECRET（署名検証用。推奨）
    timeout_seconds: 10       # 1 リクエストあたりの HTTP タイムアウト
    max_retries: 3            # 最大試行回数（4xx はリトライしない）
    retry_backoff_seconds: 1  # リトライ間隔
```

| backend | 動作 |
|---------|------|
| `webhook` | `mail.received` を `url` へ HTTP POST する |
| `none` | 発行をスキップする（デフォルト） |

---

## HTTP リクエスト仕様

- メソッド: `POST`
- ヘッダー:
  - `Content-Type: application/json`
  - `X-MailShield-Event: mail.received` または `mail.processed`
  - `X-MailShield-Signature: sha256=<hex>` — `secret` 設定時のみ。リクエストボディの
    HMAC-SHA256（鍵は `secret`）。受信側は同じ計算をして一致を検証すること
- レスポンス: `2xx` で成功。`5xx` とネットワークエラーは `max_retries` までリトライ、
  `4xx` は設定誤り・恒久的拒否とみなし即座に諦める
- 1 イベントのリトライを含む発行全体は `server.event_publish_timeout_seconds`
  （デフォルト 5 秒）で打ち切られる

> [!WARNING]
> 配信は at-most-once（ベストエフォート）である。イベントの欠落が許容できない用途では、
> `mail_messages` テーブルまたは REST API（`GET /api/v1/messages`）を突き合わせて補完すること。

---

## mail.received ペイロード仕様

EML 本文は含まない。後続サービスは `eml_path` でオブジェクトストレージから取得する。

```json
{
  "message_id":     "550e8400-e29b-41d4-a716-446655440000",
  "eml_path":       "raw/2026/06/03/550e8400....eml",
  "received_at":    "2026-06-03T10:00:00Z",
  "from_address":   "sender@external.test",
  "to_addresses":   ["user@internal.test"],
  "subject":        "Hello World",
  "size_bytes":     1024,
  "has_attachment": false,
  "rspamd_score":   0.0,
  "auth_results": {
    "spf":   "pass",
    "dkim":  "none",
    "dmarc": "none"
  }
}
```

### フィールド一覧

| フィールド | 型 | 説明 |
|-----------|-----|------|
| `message_id` | string | 内部 UUID |
| `eml_path` | string | オブジェクトストレージ上の原本パス |
| `received_at` | string | RFC 3339 形式の受信日時 |
| `from_address` | string | 送信者アドレス |
| `to_addresses` | string[] | 宛先アドレスリスト |
| `subject` | string | 件名 |
| `size_bytes` | number | EML サイズ（バイト） |
| `has_attachment` | bool | 添付ファイルの有無 |
| `rspamd_score` | number | Rspamd のスパムスコア（ヘッダーから取得） |
| `auth_results.spf` | string | `"pass"` / `"fail"` / `"none"` |
| `auth_results.dkim` | string | `"pass"` / `"fail"` / `"none"` |
| `auth_results.dmarc` | string | `"pass"` / `"fail"` / `"none"` |

---

## mail.processed ペイロード仕様

ポリシー評価後（最終アクション決定後）に発行される。SIEM 側でアクションと
検査スコアを突き合わせて相関分析するためのイベント。EML 本文は含まない。

```json
{
  "message_id":    "550e8400-e29b-41d4-a716-446655440000",
  "route":         "10-inbound",
  "direction":     "inbound",
  "action":        "quarantine",
  "from_address":  "sender@external.test",
  "to_addresses":  ["user@internal.test"],
  "subject":       "Hello World",
  "total_score":   130,
  "inspect_scores": [
    {"worker": "av-worker", "score": 100, "detected": true},
    {"worker": "url-worker", "score": 30, "detected": false}
  ],
  "processed_at":  "2026-06-03T10:00:01Z"
}
```

| フィールド | 型 | 説明 |
|-----------|-----|------|
| `message_id` | string | 内部 UUID（mail.received と共通） |
| `route` | string | マッチしたルート名 |
| `direction` | string | `inbound` / `outbound` |
| `action` | string | 最終アクション（`deliver` / `reject` / `quarantine` / `approval`） |
| `total_score` | number | 全検査ワーカーの score 合計 |
| `inspect_scores` | array | ワーカーごとの `{worker, score, detected}` |
| `processed_at` | string | RFC 3339 形式の処理完了日時 |

> [!NOTE]
> 変換パイプライン失敗時の強制隔離など、一部の早期リターン経路では
> mail.processed は発行されない場合がある（mail.received は常に発行を試みる）。

---

## 署名検証の例（受信側）

```bash
# body: 受信したリクエストボディ、secret: 共有シークレット
echo -n "$body" | openssl dgst -sha256 -hmac "$secret" -hex
# → X-MailShield-Signature の "sha256=" 以降と一致すれば正当
```
