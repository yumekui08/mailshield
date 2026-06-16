#!/bin/sh
# MinIO バケット初期化スクリプト
# Docker Compose の mc (MinIO Client) コンテナから実行される

set -e

MC="mc"
ALIAS="local"
ENDPOINT="${MINIO_ENDPOINT:-minio:9000}"
ACCESS_KEY="${MINIO_ROOT_USER:-mailshield}"
SECRET_KEY="${MINIO_ROOT_PASSWORD:-mailshield}"

echo "MinIO: エンドポイントへの接続を待機します..."
until $MC alias set "$ALIAS" "http://$ENDPOINT" "$ACCESS_KEY" "$SECRET_KEY" 2>/dev/null; do
  echo "MinIO: 接続待機中..."
  sleep 2
done
echo "MinIO: 接続成功"

# バケット作成
for BUCKET in mailshield-eml mailshield-attachments; do
  if $MC ls "$ALIAS/$BUCKET" > /dev/null 2>&1; then
    echo "MinIO: バケット $BUCKET は既に存在します"
  else
    $MC mb "$ALIAS/$BUCKET"
    echo "MinIO: バケット $BUCKET を作成しました"
  fi
done

echo "MinIO: 初期化完了"
