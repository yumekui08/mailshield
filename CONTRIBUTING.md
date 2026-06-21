# Contributing to MailShield

MailShield への貢献を歓迎します。バグ報告・機能提案・ドキュメント改善・コードのいずれも受け付けています。

## 目次

- [行動規範](#行動規範)
- [Issue の報告](#issue-の報告)
- [開発環境のセットアップ](#開発環境のセットアップ)
- [変更の提出](#変更の提出)
- [コーディング規約](#コーディング規約)
- [テスト](#テスト)

---

## 行動規範

このプロジェクトは [Contributor Covenant](https://www.contributor-covenant.org/version/2/1/code_of_conduct/) を採用しています。
プロジェクトへの参加にあたりこれに従うことを求めます。

---

## Issue の報告

### バグ報告

バグを発見した場合は [GitHub Issues](https://github.com/koizumib/mailshield/issues) から報告してください。
テンプレートに従って以下の情報を含めてください:

- 再現手順（具体的なコマンドや設定ファイル）
- 期待する動作と実際の動作
- 環境（OS・Docker Compose バージョン・MailShield バージョン）
- ログ出力（`docker compose logs smtp-gateway` の関連部分）

### 機能提案

新機能を提案する場合は Issue を作成し "enhancement" ラベルを付けてください。
実装を始める前に Issue で方針を議論することを強く推奨します。特にアーキテクチャに関わる変更は
`docs/decisions/` に ADR を作成してから実装してください。

---

## 開発環境のセットアップ

### 必要なツール

- Go 1.24 以上
- Docker Engine 24.0 以上・Docker Compose v2.20 以上
- `make`

### 手順

```bash
# 1. フォークしてクローン
git clone https://github.com/<your-fork>/mailshield.git
cd mailshield

# 2. 最小構成で起動（MariaDB のみ）
cp .env.example .env
make core-up

# 3. 開発標準構成（Postfix + Mailpit 追加）
make dev-up

# 4. ビルド確認
cd services/smtp-gateway && go build ./cmd/server/
cd ../api-server && go build ./cmd/server/
```

### 最小構成（MariaDB のみ）で動かす

RabbitMQ・MinIO なしで smtp-gateway を動かすには `config/mailshield.yaml` を編集します:

```yaml
storage:
  backend: filesystem
  local_dir: /tmp/mailshield-eml

queue:
  backend: none
```

---

## 変更の提出

1. `main` ブランチから feature ブランチを作成する
   ```bash
   git checkout -b feat/your-feature-name
   ```

2. 変更を実装し、テストを追加する

3. フォーマットと lint を確認する
   ```bash
   cd services/smtp-gateway
   gofmt -l .
   go vet ./...
   go test ./...
   ```

4. コミットメッセージは変更内容を明確に説明する（英語・日本語どちらでも可）

5. プルリクエストを作成する。PR テンプレートに従って変更の概要・テスト方法を記述する

### PR の基準

- 既存テストがすべてパスすること
- 新機能にはテストが含まれていること
- `gofmt` / `goimports` のフォーマットに従っていること
- `internal/domain/` に外部ライブラリの import がないこと（CLAUDE.md 参照）
- ドキュメント更新が必要な変更には対応する `docs/` の更新が含まれていること

---

## コーディング規約

詳細は [CLAUDE.md](CLAUDE.md) を参照してください。要点:

- エラーは `fmt.Errorf("context: %w", err)` でラップする
- goroutine は必ず context で lifetime を制御する
- interface はコンシューマー側で定義する
- `internal/domain/` に外部ライブラリのimportを入れない
- 全クエリに `WHERE tenant_id = ?` を含める（マルチテナント分離）
- ログは `log/slog` でJSON形式。`tenant_id`・`message_id` を必ず含める

---

## テスト

```bash
# smtp-gateway の全テスト
cd services/smtp-gateway
go test ./...

# 特定パッケージのみ
go test ./internal/smtp/

# 特定テスト関数のみ
go test ./internal/smtp/ -run TestXxx -v

# api-server の全テスト
cd services/api-server
go test ./...
```

### E2Eテスト

開発環境（`make dev-up`）が起動している状態で:

```bash
# 通常メール
make e2e-normal

# ウイルス件名メール（変換確認）
make e2e-virus
```

---

## ワーカーの追加

新しいワーカーを追加する場合は [docs/development/custom-worker-go.md](docs/development/custom-worker-go.md) または
[docs/development/custom-worker-lua.md](docs/development/custom-worker-lua.md) を参照してください。

`internal/domain/worker.go` のインターフェースを変更する必要がある場合は、必ず Issue で事前に相談してください。

---

## ドキュメントのみの変更

ドキュメント修正だけの PR も歓迎します。誤字・リンク切れ・古い情報の修正はコードの変更と同様に価値があります。
