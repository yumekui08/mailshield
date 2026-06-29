# RabbitMQ キュー設計

## 現在の実装状況

現在（フェーズ1）では、smtp-gateway は `mail.received` イベントの**発行のみ**実装している。ワーカーは smtp-gateway 内でインプロセス実行されるため、ワーカー間のキュー通信は未実装。

smtp-gateway はキューを**消費しない**。キューへの接続はすべて発行専用である。

| キュー | 状態 | 説明 |
|-------|------|------|
| `mail.received` | ✅ 実装済み | smtp-gateway が受信後に発行する |
| ワーカー間キュー | 📋 将来実装 | 現在はインプロセス実行 |

---

## Exchange 構成

| Exchange名 | 種別 | 用途 |
|-----------|------|------|
| `mail.received` | fanout | 受信イベントを外部コンシューマーへ配信 |

**注意**: `mail.received` Exchange は smtp-gateway が起動する前に RabbitMQ 上に存在していなければならない。smtp-gateway は起動時に passive declare（存在確認のみ）を行い、存在しない場合は起動に失敗する。Exchange の作成は `infra/rabbitmq/definitions.json` で行う。

---

## queue.backend 設定

`mailshield.yaml` の `queue.backend` で動作モードを切り替える。

| backend | 動作 |
|---------|------|
| `rabbitmq` | RabbitMQ に接続して `mail.received` を発行する（デフォルト） |
| `none` | 発行を無音でスキップする（noop モード）。RabbitMQ 不要 |

`none` モードは RabbitMQ が不要な単一ノード構成・開発環境向けである。発行はスキップされるが、処理は正常に続行する。

---

## 接続・再接続の動作

- 起動時に RabbitMQ へ接続し、`mail.received` Exchange の存在を passive declare で確認する
- 発行失敗（チャネルクローズ等）が発生した場合、**1回だけ**再接続を試みる
- 再接続にも失敗した場合はエラーを返して発行を諦め、ログに記録して処理を続行する
- 再接続処理は `reconnectMu` で直列化されており、複数 goroutine が同時に Dial することはない

---

## mail.received メッセージ仕様

smtp-gateway がメール受信後（MinIO 保存・DB 記録完了後）に発行するメッセージ。EML 本文は含まない。後続サービスは `eml_path` で MinIO から取得する。

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
| `eml_path` | string | MinIO 上の原本パス |
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

`mail.received` の発行に失敗した場合はログに記録して続行する（MinIO に EML が保存されているため後から補完可能）。

---

## 将来のキュー設計（フェーズ2以降）

外部ワーカー（別プロセス・別サービス）を接続する場合に使用するキュー設計。smtp-gateway が発行するのは `mail.received` のみであり、以下の残りのキューは外部システムが担う。

| キュー名 | パブリッシャー | コンシューマー | 説明 |
|---------|-------------|-------------|------|
| `mail.received` | smtp-gateway | 外部検査ワーカー | 受信イベント（fanout経由） |
| `mail.inspect_result` | 各外部検査ワーカー | result-aggregator | 検査結果 |
| `mail.transform` | result-aggregator | 変換パイプライン先頭 | 変換開始 |
| `mail.transform_done` | 変換パイプライン末尾 | policy-engine | 変換完了 |
| `mail.action` | policy-engine | action-executor | アクション実行指示 |
| `mail.archive` | smtp-gateway | archive-worker | アーカイブ（非同期） |
| `audit.log` | 全サービス | audit-worker | 監査ログ |
