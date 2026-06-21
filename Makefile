.PHONY: dev-up dev-down dev-logs core-up core-down \
        outbound-up outbound-down scanners-up scanners-down \
        api-up api-down \
        full-up full-down full-logs \
        build test lint \
        e2e-normal e2e-virus e2e-outbound-normal e2e-outbound-dlp

# ─── profile の組み合わせ ─────────────────────────────────────────
#
# （常時起動）: MariaDB のみ（必須サービス・プロファイル不要）
# queue    : RabbitMQ（mail.received を外部システムに通知する場合）
# storage  : MinIO（EML をオブジェクトストレージに保存する場合）
# outbound : smtp-outbound（送信フィルタ）
# scanners : ClamAV・Tika・Tesseract（スキャナー）
# dev      : Mailpit + 開発用 MTA（Postfix + Rspamd）
# api      : api-server・Web UI・Redis（api-server 用）
# traefik  : Traefik reverse proxy（将来）
#
# 最小構成: make core-up（MariaDB + smtp-gateway のみ）
# 標準開発: make dev-up（+ MinIO + RabbitMQ + Mailpit + Postfix）
# 外部サービスに切り替える場合は .env で接続先を上書きしてプロファイルを除外する

PROFILES_CORE     = storage,queue
PROFILES_DEV      = storage,queue,dev
PROFILES_OUTBOUND = storage,queue,outbound,dev
PROFILES_SCANNERS = storage,queue,outbound,scanners,dev
PROFILES_API      = storage,queue,dev,api
PROFILES_FULL     = storage,queue,outbound,scanners,dev,api,traefik

DC = COMPOSE_PROFILES

# ─── 起動・停止 ──────────────────────────────────────────────────

## smtp-gateway + インフラ + Postfix + Mailpit（開発標準）
dev-up:
	$(DC)=$(PROFILES_DEV) docker compose up -d

dev-down:
	$(DC)=$(PROFILES_DEV) docker compose down

dev-logs:
	$(DC)=$(PROFILES_DEV) docker compose logs -f

## smtp-gateway + インフラのみ（最小構成）
core-up:
	$(DC)=$(PROFILES_CORE) docker compose up -d

core-down:
	$(DC)=$(PROFILES_CORE) docker compose down

## 受信GW + 送信GW + Postfix + Mailpit
outbound-up:
	$(DC)=$(PROFILES_OUTBOUND) docker compose up -d

outbound-down:
	$(DC)=$(PROFILES_OUTBOUND) docker compose down

## 全ワーカー有効（スキャナー含む）
scanners-up:
	$(DC)=$(PROFILES_SCANNERS) docker compose up -d

scanners-down:
	$(DC)=$(PROFILES_SCANNERS) docker compose down

## api-server + 開発標準構成
api-up:
	$(DC)=$(PROFILES_API) docker compose up -d

api-down:
	$(DC)=$(PROFILES_API) docker compose down

## 全サービス（Traefik 含む）
full-up:
	$(DC)=$(PROFILES_FULL) docker compose up -d

full-down:
	$(DC)=$(PROFILES_FULL) docker compose down

full-logs:
	$(DC)=$(PROFILES_FULL) docker compose logs -f

# ─── ビルド・テスト ──────────────────────────────────────────────

build:
	cd services/smtp-gateway && go build ./cmd/server/
	cd services/api-server && go build ./cmd/server/
	cd web && npm run build

test:
	cd services/smtp-gateway && go test ./...
	cd services/api-server && go test ./...

lint:
	cd services/smtp-gateway && gofmt -l . && go vet ./...
	cd services/api-server && gofmt -l . && go vet ./...
	cd web && npx tsc --noEmit

# ─── E2Eテスト（受信） ───────────────────────────────────────────

e2e-normal:
	python3 -c "\
import smtplib, email.mime.text; \
msg = email.mime.text.MIMEText('Normal mail'); \
msg['Subject'] = 'Hello World'; msg['From'] = 'sender@external.test'; msg['To'] = 'test@internal.test'; \
s = smtplib.SMTP('localhost', 25); s.sendmail('sender@external.test', ['test@internal.test'], msg.as_string()); s.quit(); \
print('Sent: Hello World')"

e2e-virus:
	python3 -c "\
import smtplib, email.mime.text; \
msg = email.mime.text.MIMEText('This mail contains virus in subject'); \
msg['Subject'] = 'virus test mail'; msg['From'] = 'sender@external.test'; msg['To'] = 'test@internal.test'; \
s = smtplib.SMTP('localhost', 25); s.sendmail('sender@external.test', ['test@internal.test'], msg.as_string()); s.quit(); \
print('Sent: virus test mail')"

# ─── E2Eテスト（送信） ───────────────────────────────────────────

e2e-outbound-normal:
	python3 -c "\
import smtplib, email.mime.text; \
msg = email.mime.text.MIMEText('Normal outbound mail'); \
msg['Subject'] = 'Outbound normal mail'; msg['From'] = 'user@internal.test'; msg['To'] = 'recipient@external.test'; \
s = smtplib.SMTP('localhost', 587); s.sendmail('user@internal.test', ['recipient@external.test'], msg.as_string()); s.quit(); \
print('Sent: Outbound normal mail')"

e2e-outbound-dlp:
	python3 -c "\
import smtplib, email.mime.text; \
msg = email.mime.text.MIMEText('My-Number: 123456789012'); \
msg['Subject'] = 'Confidential document'; msg['From'] = 'user@internal.test'; msg['To'] = 'recipient@external.test'; \
s = smtplib.SMTP('localhost', 587); s.sendmail('user@internal.test', ['recipient@external.test'], msg.as_string()); s.quit(); \
print('Sent: Confidential document')"
