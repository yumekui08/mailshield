# メトリクス仕様

smtp-gateway はヘルスチェック用 HTTP ポート（`server.health_port`）で
Prometheus テキスト形式（text exposition format 0.0.4）のメトリクスを公開する。

外部ライブラリは使用せず自前実装のため、`prometheus/client_golang` への依存はない。
Prometheus / VictoriaMetrics / Grafana Agent 等からそのままスクレイプできる。

---

## エンドポイント

| パス | 用途 |
|------|------|
| `GET /healthz` | liveness（プロセス生存確認のみ・常に 200） |
| `GET /readyz` | readiness（MariaDB 疎通を確認・失敗時 503） |
| `GET /metrics` | Prometheus メトリクス |

`/readyz` は 5 秒タイムアウトで `SELECT` を伴わない DB ping を実行する。
Kubernetes 等では liveness probe に `/healthz`、readiness probe に `/readyz` を割り当てること。

---

## メトリクス一覧

| メトリクス名 | 型 | ラベル | 説明 |
|-------------|-----|--------|------|
| `mailshield_build_info` | gauge | `version` | ビルド情報（値は常に 1） |
| `mailshield_mail_received_total` | counter | `route` | ルート解決に成功した受信メール数 |
| `mailshield_mail_unrouted_total` | counter | — | マッチするルートがなく拒否したメール数 |
| `mailshield_mail_action_total` | counter | `route`, `action` | ポリシーアクション実行数（deliver / reject / quarantine / approval）。変換パイプライン失敗時の強制隔離も `quarantine` として数える |
| `mailshield_mail_errors_total` | counter | `stage` | 処理段階ごとの失敗数 |
| `mailshield_inspect_detected_total` | counter | `route`, `worker` | 検査ワーカーが `detected=true` を返した回数 |
| `mailshield_mail_processing_seconds` | histogram | — | メール1通の処理時間（受信〜アクション実行）。バケット: 0.1 / 0.5 / 1 / 2.5 / 5 / 10 / 30 / 60 秒 |

### `mailshield_mail_errors_total` の stage 値

| stage | 意味 | SMTP 応答 |
|-------|------|-----------|
| `storage_save` | 原本 EML の保存失敗 | 451（Postfix がリトライ） |
| `transform` | 変換パイプライン失敗（メールは隔離される） | 250 |
| `policy` | ポリシーエンジン実行エラー | 451 |
| `no_rule` | マッチするポリシールールなし | 550 |

---

## 注意事項

- カウンターはすべてプロセス内メモリ保持であり、再起動でリセットされる（Prometheus の `rate()`/`increase()` で扱う前提）。
- `/simulate` エンドポイントでのドライラン実行はメトリクスに計上されない。
- メトリクスポートは信頼できるネットワークにのみ公開すること（認証なし）。
