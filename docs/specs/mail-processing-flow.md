# メール処理フロー

最終更新: 2026-06-15

---

## ポート・コンポーネント対応表

| コンポーネント | ポート | 役割 | 外部公開 |
|-------------|-------|------|--------|
| Postfix | :25 | 外部からの受信 | ✓ |
| Postfix | :10025 | after-queue content filter 用再インジェクト受付（現在未使用） | ✗（Docker 内） |
| Postfix-submission | :587 | 内部ユーザーからの送信 | ✓ |
| smtp-gateway | :10024 | Postfix の inbound content filter 受付 | ✗（Docker 内） |
| smtp-gateway | :10024 | Postfix-submission の outbound content filter 受付 | ✗（Docker 内） |
| smtp-gateway | :8080 | ヘルスチェック | ✓ |
| api-server | :8090 | REST API | ✓ |
| Web UI | :3000 | 管理画面 | ✓ |

---

## 受信メールフロー（inbound ルート）

```mermaid
flowchart TD
    Sender([外部送信者]) -->|SMTP :25| Postfix

    Postfix -->|"SMTP :10024\ncontent filter"| GW

    subgraph GW[smtp-gateway]
        direction TB
        T1["① テナント解決\nRCPT TO ドメインで tenants テーブルを検索"]
        T2["② MinIO: 原本 EML 保存\nmailshield-eml/{tenant}/raw/YYYY/MM/DD/{uuid}.eml\nDB: eml_path を記録\n★ 失敗時 → 451 を返して Postfix にリトライ"]
        T3["③ MariaDB: メタデータ記録\nstatus = received"]
        T4["④ RabbitMQ: mail.received 発行\n（EML 本文なし・eml_path のみ）"]
        T5["⑤ 検査パイプライン（並列）\nav-worker / header-inspector / url-worker\nqr-worker / dlp-worker / subject-virus-inspector"]
        T6["⑥ 変換パイプライン（直列・order 順）\nsanitize-worker → url-rewrite-worker\n→ subject-virus-transformer → filesep-worker"]
        T7["⑦ ポリシーエンジン\npolicy-inbound.yaml ルール評価"]

        T1 --> T2 --> T3 --> T4 --> T5 --> T6 --> T7
    end

    T6 -->|添付分離時のみ| Filesep["filesep-worker 副作用\nMinIO: mailshield-attachments/{tenant}/{msg_id}/{file}\nDB: mail_attachments に記録\n添付分離通知メール送信（OTP / auth モード）"]

    T7 -->|deliver| Del["policy-inbound.yaml の destination へ直接 SMTP\n（開発: mailpit:1025）"]
    T7 -->|quarantine| Qua["DB: status = quarantined\nMinIO: processed/ に処理済み EML 保存\nDB: processed_eml_path を記録\n★ quarantine_notification.enabled=true なら受信者に通知メール送信"]
    T7 -->|reject| Rej["DB: status = rejected\n（バウンス通知は未実装）"]

    Del -.->|"archiveAsync（非同期・最大3回リトライ）"| Archive["MinIO: mailshield-eml/{tenant}/processed/YYYY/MM/DD/{uuid}.eml\nDB: processed_eml_path を記録"]

    Del --> Mailpit([Mailpit / 配送先 MTA])
```

---

## 送信メールフロー（outbound ルート）

```mermaid
flowchart TD
    MUA([内部ユーザー MUA]) -->|"SMTP :587\nSMTP AUTH"| PostfixSub[Postfix-submission]

    PostfixSub -->|"SMTP :10024\ncontent filter"| OutGW

    subgraph OutGW[smtp-gateway（outbound ルート）]
        direction TB
        O1["① テナント解決\nFrom ドメインで tenants テーブルを検索"]
        O2["② MinIO: 原本 EML 保存\nmailshield-eml/{tenant}/raw/YYYY/MM/DD/{uuid}.eml"]
        O3["③〜⑦ 受信フローと同じ処理\nメインワーカー: dlp-worker（送信情報漏洩防止）\n添付ファイル分離（OTP モード）"]

        O1 --> O2 --> O3
    end

    O3 -->|deliver| ExtMTA([外部 MTA へ直接 SMTP])
```

---

## 隔離解放フロー

```mermaid
flowchart TD
    Admin([管理者ブラウザ]) -->|"POST /api/v1/quarantine/{id}/release"| API[api-server]

    API -->|"① DB: status を delivered に更新（先に更新・重複配送防止）"| DB[(MariaDB)]
    API -->|"② MinIO から処理済み EML を取得\n（processed_eml_path が空なら 409 NOT_READY）"| MinIO[(mailshield-eml)]
    API -->|"③ 最終配送先 MTA へ直接 SMTP\n（config/api-server.yaml の reinject_host:reinject_port）\n開発: mailpit:1025"| Dest([最終宛先 MTA])

    API -->|"EML 取得失敗・SMTP 失敗時\nstatus を quarantined にロールバック"| DB
    API -->|"④ 成功時: 200 OK を返す\n（DB はすでに delivered）"| Admin
```

**重複配送防止:** DB を先に `delivered` に更新することで、解放リクエストが二重に来た場合も2回目は `GetQuarantine`（`status=quarantined` のみ返す）が 404 を返して防止できる。

---

## 隔離即時通知フロー

```mermaid
flowchart LR
    GW["smtp-gateway\n⑦ ポリシー評価→quarantine"] -->|"ActionQuarantine 確定後\n（非同期・goroutine）"| Notifier["notify.QuarantineNotifier\nSMTP with net.DialTimeout"]
    Notifier --> SMTP["notification.smtp_host:smtp_port\n（開発: mailpit:1025）"]
    SMTP --> Recipient([受信者])
```

- `quarantine_notification.enabled: false` の場合は送信しない
- 各受信者（To: アドレス）に1通ずつ送信する
- 送信失敗はログに記録して無視する（best-effort）
- 通知メールには `{ui_base_url}/quarantine` へのログインリンクを含む

---

## 通知メールフロー（OTP・パスワードリセットなど）

api-server が新規生成するメール（OTP コード・パスワードリセット）は `notification.smtp_host:smtp_port` へ直接 SMTP 送信する。

```mermaid
flowchart LR
    API[api-server] -->|"SMTP\nnotification.smtp_host:smtp_port\n（デフォルト: mailpit:1025）"| SMTP[SMTP サーバー]
    SMTP --> Dest([宛先])
```

---

## ストレージ対応表

| 用途 | バケット | パス |
|------|---------|------|
| 原本 EML | `mailshield-eml` | `{tenant}/raw/YYYY/MM/DD/{uuid}.eml` |
| 処理済み EML（deliver・quarantine 共通） | `mailshield-eml` | `{tenant}/processed/YYYY/MM/DD/{uuid}.eml` |
| 分離済み添付ファイル | `mailshield-attachments` | `{tenant}/{message_uuid}/{filename}` |

---

## エラー時の挙動

| ステップ | エラー時の動作 |
|---------|-------------|
| ② MinIO 原本保存失敗 | `451` を返して Postfix にリトライさせる |
| ③ DB 記録失敗 | ログを出力して続行 |
| ④ RabbitMQ 発行失敗 | ログを出力して続行 |
| ⑤ 検査ワーカーエラー | そのワーカーをスキップして続行 |
| ⑥ 変換ワーカーエラー | 変換前のメールで続行 |
| ⑦ ポリシー実行失敗 | `451` を返して Postfix にリトライさせる |
| archiveAsync 失敗 | 最大3回リトライ（2s/4s バックオフ）。全失敗時は ERROR ログで手動対応を促す |
| 隔離即時通知送信失敗 | WARN ログに記録して無視（best-effort） |
| 隔離解放: MinIO 取得失敗 | `rollbackToQuarantined` で DB を元に戻す。409 NOT_READY を返す |
| 隔離解放: SMTP 送信失敗 | `rollbackToQuarantined` で DB を元に戻す。500 を返す |
