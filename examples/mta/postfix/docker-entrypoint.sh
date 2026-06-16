#!/bin/sh
set -e

# 必須環境変数チェック
if [ -z "$MAILSHIELD_RELAY_DOMAINS" ]; then
  echo "ERROR: MAILSHIELD_RELAY_DOMAINS is not set." >&2
  echo "  Example: MAILSHIELD_RELAY_DOMAINS=example.com" >&2
  echo "  Multiple domains: MAILSHIELD_RELAY_DOMAINS=example.com,example.org" >&2
  exit 1
fi

if [ -z "$MAILSHIELD_MTA_HOSTNAME" ]; then
  echo "ERROR: MAILSHIELD_MTA_HOSTNAME is not set." >&2
  echo "  Example: MAILSHIELD_MTA_HOSTNAME=mail.example.com" >&2
  exit 1
fi

# テンプレートから main.cf を生成
envsubst '${MAILSHIELD_RELAY_DOMAINS} ${MAILSHIELD_MTA_HOSTNAME}' \
  < /etc/postfix/main.cf.template \
  > /etc/postfix/main.cf

echo "Postfix configuration:"
echo "  myhostname    = $MAILSHIELD_MTA_HOSTNAME"
echo "  relay_domains = $MAILSHIELD_RELAY_DOMAINS"

exec postfix start-fg
