FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    postfix \
    libsasl2-modules \
    gettext-base \
    && rm -rf /var/lib/apt/lists/*

COPY main.cf.template  /etc/postfix/main.cf.template
COPY master.cf         /etc/postfix/master.cf
COPY docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh

ENTRYPOINT ["/docker-entrypoint.sh"]
