.PHONY: build build-linux dist install clean test lint \
        dev-up dev-down dev-logs core-up core-down \
        outbound-up outbound-down scanners-up scanners-down \
        api-up api-down full-up full-down full-logs docker-clean \
        test-simulate test-smtp test-api test-e2e \
        e2e-normal e2e-virus e2e-outbound-normal e2e-outbound-dlp

# ─── バイナリビルド ───────────────────────────────────────────────

VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS   := -s -w -X main.version=$(VERSION) -X main.buildDate=$(BUILD_DATE)

## カレントプラットフォーム向けにビルドして dist/ に出力
build:
	@mkdir -p dist
	go build -ldflags "$(LDFLAGS)" -o dist/smtp-gateway ./services/smtp-gateway/cmd/server/
	go build -ldflags "$(LDFLAGS)" -o dist/api-server  ./services/api-server/cmd/server/
	@echo "Built: dist/smtp-gateway  dist/api-server"

## Linux amd64 向けにクロスコンパイル
build-linux:
	@mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build -ldflags "$(LDFLAGS)" -o dist/smtp-gateway-linux-amd64 ./services/smtp-gateway/cmd/server/
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build -ldflags "$(LDFLAGS)" -o dist/api-server-linux-amd64  ./services/api-server/cmd/server/
	@echo "Built: dist/smtp-gateway-linux-amd64  dist/api-server-linux-amd64"

## 全プラットフォーム向けに dist/ へ配布物を生成
dist:
	@mkdir -p dist
	@for os in linux darwin; do \
		for arch in amd64 arm64; do \
			echo "Building $$os/$$arch ..."; \
			CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
				go build -ldflags "$(LDFLAGS)" \
				-o dist/smtp-gateway-$$os-$$arch \
				./services/smtp-gateway/cmd/server/; \
			CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
				go build -ldflags "$(LDFLAGS)" \
				-o dist/api-server-$$os-$$arch \
				./services/api-server/cmd/server/; \
		done; \
	done
	@echo "Distribution binaries are in dist/"

## /usr/local/bin にインストール
install: build
	install -m 0755 dist/smtp-gateway /usr/local/bin/smtp-gateway
	install -m 0755 dist/api-server   /usr/local/bin/api-server
	@echo "Installed to /usr/local/bin/"

## dist/ を削除
clean:
	rm -rf dist/

# ─── テスト・Lint ─────────────────────────────────────────────────

test:
	cd services/smtp-gateway && go test ./...
	cd services/api-server && go test ./...

lint:
	cd services/smtp-gateway && gofmt -l . && go vet ./...
	cd services/api-server && gofmt -l . && go vet ./...

# ─── E2E テスト ──────────────────────────────────────────────────
#
# 各テストはサービスが起動していない場合は自動スキップ。
# 環境変数で接続先を上書き可能:
#   MAILSHIELD_GATEWAY_URL    (default: http://localhost:8080)
#   MAILSHIELD_API_URL        (default: http://localhost:8090)
#   MAILSHIELD_MAILPIT_URL    (default: http://localhost:8025)
#   MAILSHIELD_SMTP_HOST      (default: localhost)
#   MAILSHIELD_SMTP_PORT      (default: 25)

## シミュレーターテスト（smtp-gateway のみ必要）
test-simulate:
	cd tests/e2e && GOWORK=off go test -v -tags e2e -run TestSimulate -timeout 60s .

## SMTP フローテスト（MTA + smtp-gateway + Mailpit が必要）
test-smtp:
	cd tests/e2e && GOWORK=off go test -v -tags e2e -run TestSMTP -timeout 60s .

## API テスト（api-server + MariaDB が必要）
test-api:
	cd tests/e2e && GOWORK=off go test -v -tags e2e -run TestAPI -timeout 60s .

## 全 E2E テスト
test-e2e:
	cd tests/e2e && GOWORK=off go test -v -tags e2e -timeout 120s .

# ─── Docker Compose ───────────────────────────────────────────────
#
# Docker は任意オプションです。ビルドには Docker は不要です（make build を使用）。
#
# プロファイルの組み合わせ:
#   （常時起動）: MariaDB のみ（必須サービス・プロファイル不要）
#   queue    : RabbitMQ（mail.received を外部システムに通知する場合）
#   storage  : MinIO（EML をオブジェクトストレージに保存する場合）
#   scanners : ClamAV・Tika・Tesseract
#   dev      : Mailpit（処理後メール確認用）
#   api      : api-server・Web UI・Redis

DC      = docker compose -f docker/docker-compose.yml
DCENV   = COMPOSE_PROFILES

PROFILES_CORE     =
PROFILES_DEV      = storage,queue,dev
PROFILES_OUTBOUND = storage,queue,outbound,dev
PROFILES_SCANNERS = storage,queue,dev,scanners
PROFILES_API      = storage,queue,dev,api
PROFILES_FULL     = storage,queue,dev,scanners,api

## smtp-gateway + インフラ + Mailpit（開発標準）
dev-up:
	$(DCENV)=$(PROFILES_DEV) $(DC) up -d

dev-down:
	$(DCENV)=$(PROFILES_DEV) $(DC) down

dev-logs:
	$(DCENV)=$(PROFILES_DEV) $(DC) logs -f

## smtp-gateway + MariaDB のみ（最小構成）
core-up:
	$(DCENV)=$(PROFILES_CORE) $(DC) up -d

core-down:
	$(DCENV)=$(PROFILES_CORE) $(DC) down

## 受信GW + 送信GW + Mailpit
outbound-up:
	$(DCENV)=$(PROFILES_OUTBOUND) $(DC) up -d

outbound-down:
	$(DCENV)=$(PROFILES_OUTBOUND) $(DC) down

## 全ワーカー有効（スキャナー含む）
scanners-up:
	$(DCENV)=$(PROFILES_SCANNERS) $(DC) up -d

scanners-down:
	$(DCENV)=$(PROFILES_SCANNERS) $(DC) down

## api-server + 開発標準構成
api-up:
	$(DCENV)=$(PROFILES_API) $(DC) up -d

api-down:
	$(DCENV)=$(PROFILES_API) $(DC) down

## 全サービス
full-up:
	$(DCENV)=$(PROFILES_FULL) $(DC) up -d

full-down:
	$(DCENV)=$(PROFILES_FULL) $(DC) down

full-logs:
	$(DCENV)=$(PROFILES_FULL) $(DC) logs -f

## Docker ボリュームごと削除（初期化やり直し時）
docker-clean:
	$(DCENV)=$(PROFILES_FULL) $(DC) down -v
	@echo "全ボリュームを削除しました。make dev-up で再起動してください。"

# ─── E2Eテスト（手動送信）────────────────────────────────────────

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
