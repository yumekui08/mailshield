#!/usr/bin/env bash
# ARC 署名用 RSA 秘密鍵を生成するスクリプト
# 使い方: cd config/arc && ./generate-key.sh

set -euo pipefail

PRIVATE_KEY="private.pem"
PUBLIC_KEY="public.pem"

if [ -f "$PRIVATE_KEY" ]; then
  echo "秘密鍵がすでに存在します: $PRIVATE_KEY"
  echo "上書きする場合は既存ファイルを削除してから実行してください。"
  exit 1
fi

echo "RSA 2048 bit 秘密鍵を生成中..."
openssl genrsa -out "$PRIVATE_KEY" 2048
chmod 600 "$PRIVATE_KEY"

echo "公開鍵を生成中..."
openssl rsa -in "$PRIVATE_KEY" -pubout -out "$PUBLIC_KEY"

echo ""
echo "生成完了:"
echo "  秘密鍵: $PRIVATE_KEY"
echo "  公開鍵: $PUBLIC_KEY"
echo ""
echo "=== DNS TXT レコード用の公開鍵（base64） ==="
openssl rsa -in "$PRIVATE_KEY" -pubout 2>/dev/null | grep -v '^-' | tr -d '\n'
echo ""
echo ""
echo "TXT レコード名: <selector>._domainkey.<signing_domain>"
echo "TXT レコード値: v=DKIM1; k=rsa; p=<上記の base64>"
