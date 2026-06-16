# RabbitMQ キュー設計

## 現在の実装状況

現在（フェーズ1）では、RabbitMQ は `mail.received` イベントの発行のみ実装している。ワーカーは smtp-gateway 内でインプロセス実行されるため、ワーカー間のキュー通信は未実装。

| キュー | 状態 | 説明 |
|-------|------|------|
| `mail.received` | ✅ 実装済み | smtp-gateway が受信後に発行する |
| ワーカー間キュー | 📋 将来実装 | 現在はインプロセス実行 |

---

## Exchange 構成

| Exchange名 | 種別 | 用途 |
|-----------|------|------|
| `mail.received` | fanout | 受信イベントを外部コンシューマーへ配信 |
| `mail.dlx` | direct | Dead Letter メッセージの集約 |

---

## mail.received メッセージ仕様

smtp-gateway がメール受信後（MinIO 保存・DB 記録完了後）に発行するメッセージ。EML 本文は含まない。後続サービスは `eml_path` で MinIO から取得する。

```json
{
  "message_id":     "550e8400-e29b-41d4-a716-446655440000",
  "tenant_id":      "00000000-0000-0000-0000-000000000001",
  "eml_path":       "00000000-.../raw/2026/06/03/550e8400....eml",
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

`mail.received` の発行に失敗した場合はログに記録して続行する（MinIO に EML が保存されているため後から補完可能）。

---

## 将来のキュー設計（フェーズ2以降）

外部ワーカー（別プロセス・別サービス）を接続する場合に使用するキュー設計。

| キュー名 | パブリッシャー | コンシューマー | 説明 |
|---------|-------------|-------------|------|
| `mail.received` | smtp-gateway | 外部検査ワーカー | 受信イベント（fanout経由） |
| `mail.inspect_result` | 各外部検査ワーカー | result-aggregator | 検査結果 |
| `mail.transform` | result-aggregator | 変換パイプライン先頭 | 変換開始 |
| `mail.transform_done` | 変換パイプライン末尾 | policy-engine | 変換完了 |
| `mail.action` | policy-engine | action-executor | アクション実行指示 |
| `mail.archive` | smtp-gateway | archive-worker | アーカイブ（非同期） |
| `audit.log` | 全サービス | audit-worker | 監査ログ |
| `*.dlq` | Dead Letter Exchange | 管理者が手動処理 | 失敗メッセージ |

---

## Dead Letter 設計

全キューに `x-dead-letter-exchange: mail.dlx` を設定する。メッセージが以下の場合に DLX へルーティングされる。

- TTL 超過
- キューの最大長超過
- コンシューマーが `nack` を返した場合（`requeue=false`）

DLQ（`*.dlq`）に溜まったメッセージは管理者が手動で再処理またはドロップする。
