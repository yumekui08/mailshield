# シグナルハンドリング

smtp-gateway および api-server が受け付けるシグナルと動作を定義する。

## シグナル一覧

| シグナル | 送信方法 | 動作 |
|---------|---------|------|
| `SIGTERM` | `docker stop` / `systemctl stop` / `kill <pid>` | グレースフルシャットダウン |
| `SIGINT` | `Ctrl+C` | グレースフルシャットダウン |
| `SIGHUP` | `kill -HUP <pid>` | グレースフルシャットダウン（将来: 設定リロード） |

## smtp-gateway のグレースフルシャットダウン手順

1. シグナルを受信する
2. `[INFO] シグナル受信・シャットダウン開始 signal=terminated` をログに出力する
3. SMTPサーバーが新規接続の受付を停止する
4. 処理中のメールセッションが完了するのを待つ（最大 **30秒**）
5. `archiveAsync` のゴルーチンが完了するのを待つ（WaitGroup）
6. HTTPヘルスチェックサーバーを停止する
7. MariaDB・RabbitMQ の接続をクローズする
8. `[INFO] シャットダウン完了` をログに出力してプロセスを終了する

タイムアウト（30秒）を超過した場合は強制終了する。処理中のメールセッションは Postfix のキューに残り、次回の再試行で再処理される（Postfix の retry 機構）。

## api-server のグレースフルシャットダウン手順

1. シグナルを受信する
2. HTTP サーバーが新規リクエストの受付を停止する
3. 処理中のリクエストが完了するのを待つ（最大 **30秒**）
4. Redis・MariaDB の接続をクローズする
5. プロセスを終了する

## Docker Compose でのシャットダウン

```bash
# グレースフルシャットダウン（SIGTERM を送信・デフォルト10秒待機）
docker compose stop smtp-gateway

# タイムアウトを伸ばす場合
docker compose stop --timeout 40 smtp-gateway

# 強制終了（SIGKILL）
docker compose kill smtp-gateway
```

`docker compose stop` のデフォルトタイムアウトは 10 秒であるため、`--timeout 35` 程度を指定することを推奨する。

## systemd での運用（将来）

```ini
[Service]
ExecStop=/bin/kill -s TERM $MAINPID
TimeoutStopSec=40
KillMode=mixed
```

## SIGHUP の将来計画

現時点では SIGHUP はグレースフルシャットダウンと同等に扱う。将来は設定ファイルのリロード（ポリシールール・ワーカー有効/無効の切替）に対応する予定。リロードを実装した際にはこのドキュメントを更新すること。
