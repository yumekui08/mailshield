# シグナルハンドリング

smtp-gateway および api-server が受け付けるシグナルと動作を定義する。

## シグナル一覧

| シグナル | 送信方法 | 動作 |
|---------|---------|------|
| `SIGTERM` | `docker stop` / `systemctl stop` / `kill <pid>` | グレースフルシャットダウン |
| `SIGINT` | `Ctrl+C` | グレースフルシャットダウン |
| `SIGHUP` | `kill -HUP <pid>` | グレースフルシャットダウン（SIGTERM と同等） |

## smtp-gateway のグレースフルシャットダウン手順

シャットダウンは **シグナル受信** と **SMTPサーバー自体のエラー**（`serverErr`）の2つのケースで起動する。どちらの場合も同じ手順が実行される。

1. シグナルを受信する（または SMTPサーバーがエラーを返す）
2. `[INFO] シグナル受信・シャットダウン開始 signal=terminated` をログに出力する
3. SMTPサーバーが新規接続の受付を停止する
4. 処理中のメールセッションが完了するのを待つ（最大 **`shutdown_timeout_seconds`** 秒。デフォルト: **30秒**）
5. `archiveAsync` のゴルーチンが完了するのを待つ（WaitGroup）
6. HTTPヘルスチェックサーバーを停止する（タイムアウト: **`http_shutdown_timeout_seconds`** 秒。デフォルト: **5秒**）
7. MariaDB の接続は `defer` で自動クローズされる
8. `[INFO] シャットダウン完了` をログに出力してプロセスを終了する

タイムアウト（`shutdown_timeout_seconds`）を超過した場合は強制終了する。処理中のメールセッションは Postfix のキューに残り、次回の再試行で再処理される（Postfix の retry 機構）。

## api-server のグレースフルシャットダウン手順

1. シグナルを受信する
2. HTTP サーバーが新規リクエストの受付を停止する
3. 処理中のリクエストが完了するのを待つ（最大 **30秒**）
4. Redis・MariaDB の接続をクローズする
5. プロセスを終了する

## Docker Compose でのシャットダウン

```bash
# グレースフルシャットダウン（SIGTERM を送信・デフォルト10秒待機）
docker compose -f docker/docker-compose.yml stop smtp-gateway

# タイムアウトを伸ばす場合
docker compose -f docker/docker-compose.yml stop --timeout 40 smtp-gateway

# 強制終了（SIGKILL）
docker compose -f docker/docker-compose.yml kill smtp-gateway
```

`docker compose stop` のデフォルトタイムアウトは 10 秒であるため、`--timeout 35` 程度を指定することを推奨する。

## systemd での設定例

```ini
[Service]
ExecStop=/bin/kill -s TERM $MAINPID
TimeoutStopSec=40
KillMode=mixed
```
