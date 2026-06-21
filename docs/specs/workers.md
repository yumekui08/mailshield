# ワーカー仕様

ワーカーは2種類の実装形態をとる。

| 種別 | 実装言語 | 用途 |
|-----|---------|------|
| **組み込みワーカー** | Go | ClamAV・Tika・添付ファイル分離など外部サービス連携・バイナリ処理 |
| **Lua ワーカー** | Lua | ルール・条件判定・テナント固有ロジックなど軽量なカスタム処理 |

どちらも `domain.InspectWorker` / `domain.TransformWorker` インターフェースを実装するため、パイプラインは実装形態を意識しない。`mailshield.yaml` の `routes[].workers` 設定で有効化・無効化・実行順序を制御する。

## ディレクトリ構造

```
/app/workers/                           ← workers_dir（Lua ワーカーのみ）
├── subject-virus-inspector/
│   └── init.lua
└── subject-virus-transformer/
    └── init.lua

/app/config/workers/conf/               ← worker_config_dir（全ワーカー共通）
├── av-worker.yaml
├── dlp-worker.yaml
├── header-inspector.yaml
├── url-worker.yaml
├── qr-worker.yaml
├── url-rewrite-worker.yaml
├── filesep-worker.yaml
```

---

## 組み込みワーカー一覧

### av-worker（ClamAV ウイルス検査）

- **パッケージ**: `internal/worker/builtin/clamav/`
- **種別**: inspect
- **依存外部サービス**: ClamAV daemon（`clamav:3310`）
- **用途**: inbound

**設定** (`config/workers/conf/av-worker.yaml`):
```yaml
host: clamav
port: 3310
timeout_seconds: 25
max_size_mb: 25
scores:
  virus_detected: 100
```

---

### dlp-worker（Apache Tika DLP 検査）

- **パッケージ**: `internal/worker/builtin/tika/`
- **種別**: inspect
- **依存外部サービス**: Apache Tika REST API（`tika:9998`）
- **用途**: outbound（送信情報漏洩防止）

**設定** (`config/workers/conf/dlp-worker.yaml`):
```yaml
endpoint: "http://tika:9998"
timeout_seconds: 55
patterns:
  - name: credit_card
    regex: '\b(?:\d{4}[\s-]?){3}\d{4}\b'
    score: 80
  - name: personal_number
    regex: '\b\d{3}-\d{4}-\d{4}\b'
    score: 60
```

---

### header-inspector（ヘッダーなりすまし検査）

- **パッケージ**: `internal/worker/builtin/headerinspector/`
- **種別**: inspect
- **依存外部サービス**: なし
- **用途**: inbound

**設定** (`config/workers/conf/header-inspector.yaml`):
```yaml
threshold: 60
scores:
  spf_fail: 30
  dkim_fail: 40
  dmarc_fail: 30
  reply_to_mismatch: 40
  brand_spoofing: 60
brand_names: [amazon, google, microsoft, paypal, apple]
```

---

### url-worker（URL レピュテーション検査）

- **パッケージ**: `internal/worker/builtin/urlworker/`
- **種別**: inspect
- **依存外部サービス**: Google Safe Browsing / Web Risk API（オプション）
- **用途**: inbound

**設定** (`config/workers/conf/url-worker.yaml`):
```yaml
max_urls: 20
deny_list: []
reputation_api:
  backend: none       # none | safe_browsing | web_risk
  api_key: ""
scores:
  deny_list_match: 100
  reputation_api_hit: 90
```

---

### qr-worker（QR コード検査）

- **パッケージ**: `internal/worker/builtin/qrworker/`
- **種別**: inspect
- **依存外部サービス**: Tesseract OCR REST API（オプション）
- **用途**: inbound

**設定** (`config/workers/conf/qr-worker.yaml`):
```yaml
max_images: 10
qr_decode:
  enabled: true
ocr:
  enabled: false
  endpoint: "http://tesseract:8884"
  timeout_seconds: 30
deny_list: []
reputation_api:
  backend: none
  api_key: ""
scores:
  deny_list_match: 100
  reputation_api_hit: 90
```

---

### sanitize-worker（HTML 無害化）

- **パッケージ**: `internal/worker/builtin/sanitize/`
- **種別**: transform
- **依存外部サービス**: なし
- **用途**: inbound

`<script>`, `<object>`, `<embed>` タグの除去、JavaScript イベントハンドラー属性の除去、`javascript:` スキームの無効化を行う。

---

### url-rewrite-worker（URL プロキシ書き換え）

- **パッケージ**: `internal/worker/builtin/urlrewrite/`
- **種別**: transform
- **用途**: inbound

**設定** (`config/workers/conf/url-rewrite-worker.yaml`):
```yaml
proxy_base_url: ""      # 空の場合はノーオペレーション
url_encode: base64
rewrite_html: true
rewrite_text: true
skip_domains: [internal.test, localhost]
```

---

### filesep-worker（添付ファイル分離）

- **パッケージ**: `internal/worker/builtin/filesep/`
- **種別**: transform
- **依存外部サービス**: なし（MinIO・MariaDB は DI で注入）
- **用途**: inbound / outbound

添付ファイルを MinIO に保存し、EML 本体のパートをダウンロードリンクに差し替える。ルートの `attachment_download.flows` 設定に応じた認証モード（OTP / auth）で通知メールを送信する。

---

### disclaimer-worker（フッター付与）

- **パッケージ**: `internal/worker/builtin/disclaimer/`
- **種別**: transform
- **依存外部サービス**: なし
- **用途**: outbound（inbound でも使用可）

テキスト / HTML 本文の末尾に組織フッターを付与する。二重付与防止のためマーカー文字列を検索し、既にフッターが含まれる場合はスキップする。

**設定ファイル** `config/workers/conf/disclaimer-worker.yaml`:

```yaml
marker: "mailshield-disclaimer"
text_footer: |
  --
  このメールは組織のメールフィルタリングシステムを通じて送信されました。
html_footer: |
  <div style="margin-top:16px; font-size:12px; color:#666; border-top:1px solid #eee; padding-top:8px;">
  このメールは組織のメールフィルタリングシステムを通じて送信されました。
  </div>
```

| 設定項目 | 説明 |
|---------|------|
| `marker` | 二重付与防止用のマーカー文字列。テキストパートには `\r\n\r\n<marker>\r\n` の形式で先頭に挿入する |
| `text_footer` | テキストパートの末尾に追加するフッター文字列 |
| `html_footer` | HTML パートの `</body>` 直前（なければ末尾）に追加する HTML フラグメント |

---

### arcsealer-worker（ARC 署名）

- **パッケージ**: `internal/worker/builtin/arcsealer/`
- **種別**: transform
- **依存外部サービス**: なし
- **用途**: inbound / outbound（通常は全ルートで有効化）

処理済みメールに ARC（Authenticated Received Chain）署名ヘッダー（`ARC-Seal`, `ARC-Message-Signature`, `ARC-Authentication-Results`）を付与する。他の変換ワーカーよりも後ろの order を指定すること。

**設定ファイル** `config/workers/conf/arcsealer-worker.yaml`:

```yaml
selector: mailshield
signing_domain: example.com
private_key_path: /app/config/arc/private.pem
```

| 設定項目 | 説明 |
|---------|------|
| `selector` | DKIM/ARC セレクター。DNS TXT レコード名は `<selector>._domainkey.<signing_domain>` |
| `signing_domain` | ARC 署名に使用するドメイン |
| `private_key_path` | RSA 秘密鍵ファイルのパス。`config/arc/generate-key.sh` で生成する |

Exchange Online と Google Workspace への登録手順は [ARC 署名統合ガイド](../setup/arc-integration.md) を参照。

---

## Lua ワーカー

### インターフェース

```lua
local M = {}
M.name = "my-worker"
M.type = "inspect"     -- "inspect" または "transform"

function M.inspect(mail, config)
    return { detected = false, score = 0, details = {} }
end

-- transform の場合
function M.transform(mail, config)
    mail.subject = "[PREFIX] " .. mail.subject
    return mail
end

return M
```

### mail オブジェクトのフィールド

| フィールド | 型 | 説明 |
|-----------|-----|------|
| `mail.subject` | string | 件名 |
| `mail.from` | string | 送信者アドレス |
| `mail.to` | []string | 宛先アドレスリスト |
| `mail.text_body` | string | テキスト本文 |
| `mail.html_body` | string | HTML 本文 |
| `mail.auth_results.spf` | string | `"pass"` / `"fail"` / `"none"` |
| `mail.auth_results.dkim` | string | `"pass"` / `"fail"` / `"none"` |
| `mail.auth_results.dmarc` | string | `"pass"` / `"fail"` / `"none"` |

### 同梱 Lua ワーカー（開発・テスト用）

#### subject-virus-inspector

件名に `virus` が含まれるメールをウイルスとして検知する（大文字小文字不問）。

#### subject-virus-transformer

件名に `virus` が含まれるメールの件名冒頭に `[迷惑メール注意] ` を付加する。

---

## ワーカーの追加方法

### 組み込みワーカー（Go）

1. `services/smtp-gateway/internal/worker/builtin/<name>/` にパッケージを作成する
2. `domain.InspectWorker` または `domain.TransformWorker` インターフェースを実装する
3. `cmd/server/main.go` の `builtinInspect` / `builtinTransform` スライスに追加する
4. `config/workers/conf/<name>.yaml` に設定ファイルを追加する
5. `config/mailshield.yaml` の該当ルートの `workers` リストに追加する

### Lua ワーカー

1. `workers/<name>/init.lua` を作成する（Go のビルド不要）
2. `config/mailshield.yaml` の該当ルートの `workers` リストに追加する

```yaml
workers:
  workers_dir: /app/workers
  inspect:
    - name: my-worker
      enabled: true
      timeout_seconds: 10
```
