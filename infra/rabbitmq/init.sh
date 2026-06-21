#!/bin/sh
set -e

HOST="${RABBITMQ_HOST:-rabbitmq}"
USER="${RABBITMQ_USER:-mailshield}"
PASS="${RABBITMQ_PASSWORD:-mailshield}"
BASE="http://${HOST}:15672/api"

echo "RabbitMQ management API の準備を待機しています..."
until wget -q -O /dev/null \
      --http-user="${USER}" \
      --http-password="${PASS}" \
      "${BASE}/overview" 2>/dev/null; do
  printf "."
  sleep 2
done
echo " 接続確認"

wget -q -O /dev/null \
  --http-user="${USER}" \
  --http-password="${PASS}" \
  --post-file=/definitions.json \
  --header="Content-Type: application/json" \
  "${BASE}/definitions"

echo "RabbitMQ definitions インポート完了"
