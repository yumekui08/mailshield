#!/bin/sh
set -e

HOST="${RABBITMQ_HOST:-rabbitmq}"
USER="${RABBITMQ_USER:-mailshield}"
PASS="${RABBITMQ_PASSWORD:-mailshield}"

echo "RabbitMQ management API の準備を待機しています..."
until rabbitmqadmin \
      --host="${HOST}" \
      --username="${USER}" \
      --password="${PASS}" \
      show overview > /dev/null 2>&1; do
  printf "."
  sleep 2
done
echo " 接続確認"

rabbitmqadmin \
  --host="${HOST}" \
  --username="${USER}" \
  --password="${PASS}" \
  import /definitions.json

echo "RabbitMQ definitions インポート完了"
