#!/bin/sh
set -e

if [ -z "$MAILSHIELD_SUBMISSION_HOSTNAME" ]; then
  echo "ERROR: MAILSHIELD_SUBMISSION_HOSTNAME is not set." >&2
  echo "  Example: MAILSHIELD_SUBMISSION_HOSTNAME=mail.example.com" >&2
  exit 1
fi

envsubst '${MAILSHIELD_SUBMISSION_HOSTNAME}' \
  < /etc/postfix/main.cf.template \
  > /etc/postfix/main.cf

echo "Postfix submission configuration:"
echo "  myhostname = $MAILSHIELD_SUBMISSION_HOSTNAME"

exec postfix start-fg
