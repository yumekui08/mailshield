#!/bin/sh
set -e

HOST="${RABBITMQ_HOST:-rabbitmq}"
USER="${RABBITMQ_USER:-mailshield}"
PASS="${RABBITMQ_PASSWORD:-mailshield}"
BASE="http://${HOST}:15672/api"

echo "RabbitMQ management API の準備を待機しています..."
until curl -sf -u "${USER}:${PASS}" "${BASE}/overview" > /dev/null 2>&1; do
  printf "."
  sleep 2
done
echo " 接続確認"

curl -sf \
  -u "${USER}:${PASS}" \
  -X POST "${BASE}/definitions" \
  -H "Content-Type: application/json" \
  -d @/definitions.json \
  > /dev/null

echo "RabbitMQ definitions インポート完了"
