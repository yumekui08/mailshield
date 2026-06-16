# MinIO オブジェクトパス仕様

## バケット構成

| バケット名 | 用途 |
|-----------|------|
| `mailshield-eml` | 原本EML・処理済みEML（単一バケット、パスで区別） |
| `mailshield-attachments` | 分離済み添付ファイル |

## オブジェクトキー命名規則

```
原本EML（受信直後・変更なし）:
  {tenant_id}/raw/{YYYY}/{MM}/{DD}/{message_uuid}.eml

処理済みEML（変換後・deliver または quarantine 時）:
  {tenant_id}/processed/{YYYY}/{MM}/{DD}/{message_uuid}.eml

分離済み添付ファイル:
  {tenant_id}/{message_uuid}/{original_filename}
```

### 例

```
00000000-0000-0000-0000-000000000001/raw/2026/06/03/550e8400-e29b-41d4-a716-446655440000.eml
00000000-0000-0000-0000-000000000001/processed/2026/06/03/550e8400-e29b-41d4-a716-446655440000.eml
```

## テナント分離

`tenant_id` をパスの先頭に置くことでテナント間のオブジェクトを分離する。
バケットポリシーで `/{tenant_id}/*` へのアクセスをテナントごとに制限できる。

## Content-Type

| オブジェクト種別 | Content-Type |
|----------------|-------------|
| EML | `message/rfc822` |
| 添付ファイル | 元ファイルの MIME タイプ（不明な場合は `application/octet-stream`） |

## 保存タイミング

| タイミング | ステップ | 対象バケット | パス |
|-----------|---------|------------|------|
| smtp-gateway 受信後 | [2/7] | `mailshield-eml` | `{tenant}/raw/YYYY/MM/DD/{uuid}.eml` |
| deliver アクション後（非同期） | archiveAsync | `mailshield-eml` | `{tenant}/processed/YYYY/MM/DD/{uuid}.eml` |
| quarantine アクション実行時 | [7/7] | `mailshield-eml` | `{tenant}/processed/YYYY/MM/DD/{uuid}.eml` |
| filesep-worker が実行された場合 | [6/7] | `mailshield-attachments` | `{tenant}/{message_uuid}/{filename}` |
| 隔離解放後に再配送される場合 | api-server が MinIO から取得 | `mailshield-eml` | `{tenant}/processed/YYYY/MM/DD/{uuid}.eml`（読み取り） |

## 外部 S3 への切り替え

```bash
# .env で以下を設定する
MINIO_ENDPOINT=s3.amazonaws.com
MINIO_ACCESS_KEY=AKIAXXXXXXXXXXXXXXXX
MINIO_SECRET_KEY=your-secret-key
MINIO_USE_SSL=true
```

api-server の `config/api-server.yaml` で `storage.public_endpoint` を S3 のエンドポイントに設定することで、署名付き URL の配信元を変更できる。
