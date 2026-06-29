# ストレージ仕様

最終更新: 2026-06-30

---

## バックエンド種別

`mailshield.yaml` の `storage.backend` で以下から選択する。

| バックエンド | 用途 |
|------------|------|
| `minio` | MinIO（デフォルト）または S3 互換ストレージ |
| `s3` | AWS S3（外部） |
| `filesystem` | ローカルファイルシステム（MinIO 不要・開発・最小構成向け） |

---

## バケット構成（minio / s3）

| バケット名 | 用途 |
|-----------|------|
| `mailshield-eml` | 原本 EML・処理済み EML（単一バケット、パスで区別） |
| `mailshield-attachments` | 分離済み添付ファイル |

> **注意:** MinIO のバケットおよび Exchange は passive 宣言（起動時に既存を確認するだけで作成しない）。バケットは `definitions.json` または `minio/init.sh` であらかじめ作成しておく必要がある。

---

## オブジェクトキー命名規則

```
原本 EML（受信直後・変更なし）:
  raw/{YYYY}/{MM}/{DD}/{message_uuid}.eml

処理済み EML（変換後・deliver または quarantine 時）:
  processed/{YYYY}/{MM}/{DD}/{message_uuid}.eml

分離済み添付ファイル:
  attachments/{message_uuid}/{original_filename}
```

### 例

```
raw/2026/06/03/550e8400-e29b-41d4-a716-446655440000.eml
processed/2026/06/03/550e8400-e29b-41d4-a716-446655440000.eml
attachments/550e8400-e29b-41d4-a716-446655440000/report.pdf
```

パスに `tenant_id` プレフィックスは含まない。

---

## filesystem バックエンド

MinIO を使わずにローカルファイルシステムに保存するバックエンド。最小構成や CI 環境向け。

### 設定

```yaml
storage:
  backend: filesystem
  local_dir: /var/mailshield/eml
  public_base_url: http://localhost:8080
```

| 設定項目 | 説明 |
|---------|------|
| `storage.local_dir` | EML ファイルを保存するルートディレクトリ |
| `storage.public_base_url` | 署名付き URL の代わりに返す URL のベース |

### 署名付き URL の代替

filesystem バックエンドでは `GetPresignedURL` が以下の形式の URL を返す。

```
{public_base_url}/internal/files/{path}
```

例:
```
http://localhost:8080/internal/files/raw/2026/06/03/550e8400-e29b-41d4-a716-446655440000.eml
```

---

## Content-Type

| オブジェクト種別 | Content-Type |
|----------------|-------------|
| EML | `message/rfc822` |
| 添付ファイル | 元ファイルの MIME タイプ（不明な場合は `application/octet-stream`） |

---

## 保存タイミング

| タイミング | ステップ | バケット / ディレクトリ | パス |
|-----------|---------|----------------------|------|
| smtp-gateway 受信後 | [2/7] | `mailshield-eml` | `raw/YYYY/MM/DD/{uuid}.eml` |
| deliver アクション後（非同期） | archiveAsync | `mailshield-eml` | `processed/YYYY/MM/DD/{uuid}.eml` |
| quarantine アクション実行時 | [7/7] | `mailshield-eml` | `processed/YYYY/MM/DD/{uuid}.eml` |
| 変換パイプライン失敗時（隔離） | [6/7] | `mailshield-eml` | `processed/YYYY/MM/DD/{uuid}.eml` |
| filesep-worker が実行された場合 | [6/7] | `mailshield-attachments` | `attachments/{message_uuid}/{filename}` |
| 隔離解放後の再配送時 | api-server が読み取り | `mailshield-eml` | `processed/YYYY/MM/DD/{uuid}.eml`（読み取りのみ） |

---

## 外部 S3 への切り替え

```yaml
storage:
  backend: s3
  endpoint: s3.amazonaws.com
  use_ssl: true
  bucket_eml: mailshield-eml
  bucket_attachments: mailshield-attachments
```

または環境変数で上書きする。

```bash
MINIO_ENDPOINT=s3.amazonaws.com
MINIO_ACCESS_KEY=AKIAXXXXXXXXXXXXXXXX
MINIO_SECRET_KEY=your-secret-key
MINIO_USE_SSL=true
```

api-server の `config/api-server.yaml` で `storage.public_endpoint` を S3 のエンドポイントに設定することで、署名付き URL の配信元を変更できる。
