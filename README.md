# MailShield OSS

受信メールゲートウェイのミドルウェア。外部MTAから届いたメールを検査・変換・ポリシー評価したうえで配送・隔離・拒否を行う。

## 特徴

- **プラグイン型ワーカー**: 検査ワーカー（並列）と変換ワーカー（直列）を設定ファイルで有効化・無効化できる
- **マルチルート**: `config/routes.d/` 配下のディレクトリで受信・送信ルートを個別定義。正規表現でルートを振り分ける
- **MTA非依存**: Postfix・Sendmail・外部MTAを問わず、SMTP after-queue content filter として動作する
- **MariaDB のみ必須**: RabbitMQ・MinIO・Redis はすべてオプション。`queue.backend: none` / `storage.backend: filesystem` / `redis.backend: mariadb` で外部サービスなしの単一ノード構成にできる
- **管理 Web UI**: メール一覧・隔離管理・添付ファイル分離・ユーザー管理・監査ログ・API キー管理を Web ブラウザで操作できる
- **API キー認証**: `Authorization: Bearer <key>` ヘッダで機械間認証。CI/CD・SIEM 連携に使用できる

## クイックスタート

詳細な手順は [クイックスタートガイド](docs/setup/quick-start.md) を参照してください。

### バイナリで動かす

MariaDB が用意できていれば Docker なしで動作します。

```bash
# 1. クローン
git clone https://github.com/koizumib/mailshield.git
cd mailshield

# 2. ビルド
make build          # → dist/smtp-gateway  dist/api-server

# 3. MariaDB にスキーマを適用
mysql -u root mailshield < schema/mariadb/001_schema.sql
mysql -u root mailshield < schema/mariadb/002_seed.sql

# 4. config/mailshield.yaml で DB 接続先・ルートドメインを設定

# 5. 起動
./dist/smtp-gateway

# 6. テストメール送信（別ターミナル）
swaks --to test@internal.test --from sender@external.test \
      --server localhost --port 10024 \
      --header "Subject: Hello MailShield"
```

デフォルト設定（`config/mailshield.default.yaml`）:
- ストレージ: ローカルファイルシステム（`./data/eml/`）
- キュー: なし（RabbitMQ 不要）
- DB: `localhost:3306`

### Docker Compose で動かす

外部 MTA なしで評価する詳細な手順は [クイックスタート](docs/setup/quick-start.md) を参照してください。

```bash
# 1. クローン・.env 作成（パスワード・配送先を設定）
git clone https://github.com/koizumib/mailshield.git
cd mailshield
cp .env.example .env
# .env のパスワード類を変更し、評価環境では配送先を Mailpit に向ける:
#   MAILSHIELD_REINJECT_HOST=mailpit
#   MAILSHIELD_REINJECT_PORT=1025

# 2. ホストからの SMTP 接続を許可（config/mailshield.yaml）
#   server:
#     trusted_sources: [127.0.0.1, 172.16.0.0/12]

# 3. 起動（MariaDB + MinIO + RabbitMQ + Mailpit）
make dev-up

# 4. テストメール送信（smtp-gateway に直接投入）
swaks --to test@internal.test --from sender@external.test \
      --server localhost --port 10024 \
      --header "Subject: Hello MailShield"

# Mailpit でメールを確認
open http://localhost:8025
```

デフォルトの管理者アカウント（`schema/mariadb/002_seed.sql` で設定）:
- メールアドレス: `admin@internal.test`
- パスワード: `password`

## ドキュメント

全ドキュメントの索引は [docs/README.md](docs/README.md) を参照。

### セットアップ
| ドキュメント | 内容 |
|------------|------|
| [システム概要と前提アーキテクチャ](docs/setup/overview.md) | **まず読む** — 必要な MTA・インフラ要件 |
| [クイックスタート](docs/setup/quick-start.md) | 外部 MTA なしで評価環境を構築（約 15 分） |
| [プロファイルガイド](docs/setup/profiles.md) | Docker Compose プロファイルの組み合わせ |
| [自前 MTA との連携](docs/setup/mta-self-managed.md) | Postfix 等の既存 MTA への組み込み方法 |
| [バイナリインストール](docs/setup/binary-install.md) | Docker を使わないセットアップ |
| [アップグレード](docs/setup/upgrade.md) | バージョンアップ手順 |

### ユーザーガイド
| ドキュメント | 内容 |
|------------|------|
| [ルーティング設定](docs/guide/routes.md) | inbound / outbound ルートの定義方法 |
| [ポリシー設定](docs/guide/policy.md) | ポリシーエンジンのルール記述方法 |
| [ワーカー設定](docs/guide/workers.md) | 組み込みワーカーの設定・有効化 |
| [隔離メール管理](docs/guide/quarantine.md) | 隔離の解放・削除・一括操作 |

### 開発者向け
| ドキュメント | 内容 |
|------------|------|
| [Lua カスタムワーカー](docs/development/custom-worker-lua.md) | Lua でワーカーを実装する方法 |
| [Go 組み込みワーカー](docs/development/custom-worker-go.md) | Go でワーカーをビルドインする方法 |
| [REST API リファレンス](docs/development/api-reference.md) | 全エンドポイントの仕様 |
| [テストガイド](docs/development/testing.md) | ユニットテスト・E2E テストの実行方法 |

### 運用
| ドキュメント | 内容 |
|------------|------|
| [トラブルシューティング](docs/operations/troubleshooting.md) | よくある問題と解決策 |
| [バックアップ・リストア](docs/operations/backup.md) | MariaDB と MinIO のバックアップ手順 |

### 技術仕様
| ドキュメント | 内容 |
|------------|------|
| [アーキテクチャ概要](docs/architecture.md) | システム全体の構成とデータフロー |
| [設定リファレンス](docs/specs/configuration.md) | mailshield.yaml / api-server.yaml / 環境変数の全設定項目 |
| [ログ仕様](docs/specs/logging.md) | ログフォーマット・フィールド定義・syslog 切替方法 |
| [シグナルハンドリング](docs/specs/signals.md) | SIGTERM / SIGINT / SIGHUP の動作 |
| [メール処理フロー](docs/specs/mail-processing-flow.md) | メール1通が辿るステップの詳細・隔離解放フロー |
| [ワーカー仕様](docs/specs/workers.md) | 組み込みワーカー・Lua ワーカーの実装仕様 |
| [ストレージ仕様](docs/specs/storage.md) | MinIO オブジェクトパス命名規則 |
| [キュー仕様](docs/specs/queues.md) | RabbitMQ Exchange・キュー設計 |
| [API 認証仕様](docs/specs/api-authentication.md) | セッション Cookie / API キー認証の詳細 |
| [設計判断記録](docs/decisions/) | ADR（Architecture Decision Records） |

## 開発コマンド

```bash
# ─── バイナリビルド ───────────────────────────────────────────────
make build          # dist/smtp-gateway + dist/api-server（カレントOS向け）
make build-linux    # Linux amd64 向けクロスコンパイル
make dist           # 全プラットフォーム（linux/darwin × amd64/arm64）
make install        # /usr/local/bin/ にインストール

# ─── テスト ──────────────────────────────────────────────────────
make test           # ユニットテスト（全パッケージ）
make test-simulate  # E2E シミュレーターテスト（smtp-gateway のみ必要）
make test-e2e       # 全 E2E テスト

# ─── Docker（任意） ──────────────────────────────────────────────
make dev-up     # フル構成起動（Postfix + Mailpit + Web UI）
make core-up    # コアのみ起動（smtp-gateway + MariaDB）
make dev-down   # 停止
```

## コンポーネント構成

```mermaid
flowchart LR
    Sender([外部送信者]) -->|SMTP| MTA["既存 MTA"]
    MTA -->|"SMTP :10024"| GW["smtp-gateway"]

    GW -->|必須| DB[("MariaDB\nメタデータ")]
    GW -.->|"optional\nstorage.backend: minio"| MinIO[("MinIO\nEML 保存")]
    GW -.->|"optional\nqueue.backend: rabbitmq"| MQ[("RabbitMQ\nイベント")]
    GW -->|"検査・変換・ポリシー評価"| Dest(["配送先 MTA"])

    Admin([管理者ブラウザ]) -->|HTTPS| WebUI["Web UI"]
    WebUI -->|REST API| API["api-server"]
    API --> DB
    API -.->|optional| MinIO
    API -.->|"optional\nredis.backend: redis"| Redis[("Redis\nセッション")]
```

詳細は [アーキテクチャ概要](docs/architecture.md) を参照。
