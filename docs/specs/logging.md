# ログ仕様

## 出力先と切替方法

デフォルトは標準出力（stdout）に JSON 形式で出力する。`config/mailshield.yaml` の `log` セクションで変更できる。

```yaml
log:
  level: info          # debug / info / warn / error
  format: json         # json / text
  output: stdout       # stdout / syslog
  syslog_tag: smtp-gateway
```

### syslog への切替

`output: syslog` に変更するとローカルの syslogd（`/dev/log`）へ出力される。facility は `LOG_MAIL` を使用する。systemd 環境では journald 経由で収集される。

```bash
# journald で確認する場合
journalctl -u smtp-gateway -f

# /var/log/mail.log で確認する場合（rsyslog 設定依存）
tail -f /var/log/mail.log
```

## ログレベル

| レベル | 用途 |
|-------|------|
| `DEBUG` | 処理の詳細（接続元IP・各ステップの入出力）。開発時のみ推奨 |
| `INFO` | 正常系の完了イベント（メール受信・保存完了・配送完了等） |
| `WARN` | リトライ・スキップ等の非致命的問題（DB保存失敗でも続行する場合等） |
| `ERROR` | 処理失敗でSMTPエラーを返す場合・起動失敗等 |

## JSON フォーマット

```json
{
  "time": "2026-06-03T10:00:00.123456789Z",
  "level": "INFO",
  "msg": "[2/7] EML 保存完了",
  "message_id": "550e8400-e29b-41d4-a716-446655440000",
  "tenant_id": "00000000-0000-0000-0000-000000000001",
  "from": "sender@external.test",
  "to": ["user@internal.test"],
  "size_bytes": 1024,
  "eml_path": "00000000-.../raw/2026/06/03/550e8400....eml"
}
```

## 共通フィールド

メール処理中は以下のフィールドが全ログに付与される。

| フィールド | 型 | 説明 |
|-----------|----|------|
| `message_id` | string | 内部UUID（受信時に採番） |
| `tenant_id` | string | テナントUUID |
| `from` | string | 送信者アドレス |
| `to` | []string | 宛先アドレスリスト |
| `size_bytes` | int | メールサイズ（バイト） |

ワーカー処理中はさらに以下が付与される。

| フィールド | 型 | 説明 |
|-----------|----|------|
| `worker` | string | ワーカー名 |
| `detected` | bool | 検知フラグ（検査ワーカーのみ） |
| `score` | int | スコア（検査ワーカーのみ） |

## メール処理フローのログ出力

メール1通を処理する際は `[N/7]` プレフィックスでステップを識別できる。

```
[1/7] メール受信          ← SMTPセッションからメールを受け取った
[2/7] EML 保存完了        ← MinIO への保存が完了した
[3/7] DB メタデータ記録   ← MariaDB への記録が完了した
[4/7] mail.received 発行  ← RabbitMQ へのイベント発行が完了した
[5/7] 検査結果            ← 各検査ワーカーの結果（ワーカー数分出力）
[6/7] 変換パイプライン    ← 変換の有無と結果
[7/7] ポリシー評価        ← マッチしたルールとアクション
     メール処理完了       ← 総処理時間（elapsed_ms）
```

`message_id` フィールドで grep することで1通のメールの全ログを追跡できる。

```bash
docker logs mailshield-smtp-gateway-1 | grep '"message_id":"<UUID>"'
```
