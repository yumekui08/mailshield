#!/bin/sh
set -e

rabbitmqadmin \
  -H rabbitmq \
  -u "${RABBITMQ_USER}" \
  -p "${RABBITMQ_PASSWORD}" \
  import /definitions.json

echo "RabbitMQ definitions imported"
