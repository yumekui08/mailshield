# ワーカー設定ガイド

カスタムワーカー（Lua / Go）の作成方法は [開発者向けガイド](../development/custom-worker-lua.md) を参照してください。

---

## ワーカーの有効化

ワーカーはルートごとに `config/routes.d/<ルート名>/route.yaml` で有効化します。

```yaml
# config/routes.d/10-inbound/route.yaml
name: inbound
direction: inbound
match:
  to: "@example\.com$"
workers:
  inspect:                           # 検査ワーカー（並列実行）
    - name: av-worker
      enabled: true
      timeout_seconds: 30
  transform:                         # 変換ワーカー（order 順に直列実行）
    - name: sanitize-worker
      enabled: true
      order: 1
```

各ワーカーの詳細設定は `config/workers/conf/{name}.yaml` に記述します。

Lua ワーカーの配置ディレクトリは `config/mailshield.yaml` の `workers.workers_dir` で指定します。
デフォルトは `./workers`（バイナリ直接実行時）または `/app/workers`（Docker 実行時）です。

---

## 検査ワーカー（Inspect Workers）

メールを読むだけで内容を変更しません。有効なワーカーは全て並列で実行されます。
結果（score, detected, details）はポリシーエンジンに渡されます。

---

### av-worker（ウイルス検査）

ClamAV を使ってウイルスを検査します。`scanners` プロファイルで ClamAV が起動している必要があります。

**有効化:** `profile: scanners`

**設定（`config/workers/conf/av-worker.yaml`）:**
```yaml
host: clamav    # ClamAV ホスト名
port: 3310      # clamd ポート
timeout_seconds: 30
```

**ポリシーで使えるキー:**
| キー | 型 | 説明 |
|-----|---|------|
| `av-worker.detected` | bool | ウイルス検知フラグ |
| `av-worker.score` | int | 100（検知時）/ 0（正常時） |
| `av-worker.virus_name` | string | 検知したウイルス名 |

---

### dlp-worker（情報漏洩検査）

Apache Tika でテキストを抽出し、正規表現パターンでスキャンします。送信ルートでの利用を推奨します。

**有効化:** `profile: scanners`（Tika が必要）

**設定（`config/workers/conf/dlp-worker.yaml`）:**
```yaml
tika_url: http://tika:9998
timeout_seconds: 30

patterns:
  - name: credit_card
    regex: '\b(?:\d{4}[- ]?){3}\d{4}\b'
    score: 80               # マッチ時に加算されるスコア（複数マッチは合算、最大100）
  - name: my_number_jp
    regex: '\b\d{4}[-\s]?\d{4}[-\s]?\d{4}\b'
    score: 80
  - name: phone_jp
    regex: '\b0\d{1,4}[-\s]?\d{1,4}[-\s]?\d{4}\b'
    score: 30
```

**ポリシーで使えるキー:**
| キー | 型 | 説明 |
|-----|---|------|
| `dlp-worker.detected` | bool | 閾値以上で true |
| `dlp-worker.score` | int | 合算スコア（0–100） |
| `dlp-worker.matched_pattern` | string | 最初にマッチしたパターン名 |

---

### header-inspector（ヘッダーなりすまし検査）

SPF/DKIM/DMARC の認証結果と From ヘッダーを突合してなりすましを検出します。
Authentication-Results ヘッダ（Rspamd が付与）を読みます。

**有効化:** 外部サービス不要（常に使用可能）

**設定（`config/workers/conf/header-inspector.yaml`）:**
```yaml
threshold: 60        # このスコア以上で detected=true

scores:
  spf_fail: 30       # SPF fail 時の加算スコア
  dkim_fail: 40      # DKIM fail 時の加算スコア
  dmarc_fail: 30     # DMARC fail 時の加算スコア
  reply_to_mismatch: 40   # Reply-To と From のドメインが異なる
  brand_spoofing: 60      # ブランド名なりすまし

brand_names:
  - amazon
  - google
  - microsoft
  - paypal
  - apple
```

**ポリシーで使えるキー:**
| キー | 型 | 説明 |
|-----|---|------|
| `header-inspector.detected` | bool | threshold 以上で true |
| `header-inspector.score` | int | 合算スコア |
| `header-inspector.reason` | string | 検知理由 |

---

### url-worker（URL レピュテーション検査）

メール本文から URL を抽出し、deny リストまたは外部 API で検査します。

**有効化:** 外部サービス不要（deny リストのみの場合）

**設定（`config/workers/conf/url-worker.yaml`）:**
```yaml
max_urls: 20          # 1通あたりの検査 URL 上限

deny_list:
  - malware.example.com
  - phishing.example.net

reputation_api:
  backend: none         # none | safe_browsing | web_risk
  # api_key: ""

scores:
  deny_list_match: 100
  reputation_api_hit: 90
```

`backend: safe_browsing` にすると Google Safe Browsing API を使います（無料・非商用向け）。
`backend: web_risk` にすると Google Cloud Web Risk API を使います（商用・従量課金）。

**ポリシーで使えるキー:**
| キー | 型 | 説明 |
|-----|---|------|
| `url-worker.detected` | bool | 検知フラグ |
| `url-worker.score` | int | スコア |
| `url-worker.matched_url` | string | マッチした URL |

---

### qr-worker（QR コード検査）

メール添付画像の QR コードをデコードし、URL を url-worker と同様の方法で検査します。
OCR オプションを有効にすると、画像内のテキスト URL も抽出します。

**有効化:** 基本機能は不要。OCR は `profile: scanners`（Tesseract が必要）

**設定（`config/workers/conf/qr-worker.yaml`）:**
```yaml
max_images: 10

qr_decode:
  enabled: true     # gozxing（外部サービス不要）

ocr:
  enabled: false
  endpoint: "http://tesseract:8884"
  timeout_seconds: 30

deny_list: []
reputation_api:
  backend: none
```

---

## 変換ワーカー（Transform Workers）

メールの内容を書き換えます。`order` の小さい順に直列で実行されます。

---

### sanitize-worker（HTML 無害化）

HTML メール本文の危険なタグ・属性を除去します。

**有効化:** 外部サービス不要

**設定（`config/workers/conf/sanitize-worker.yaml`）:**
```yaml
policy: standard    # standard（安全なタグのみ許可）| strict（HTML をすべて除去）
```

- `standard`: `<b>`, `<i>`, `<a>`, `<p>`, `<ul>` 等の安全なタグと属性を許可。`<script>`, `<iframe>`, `javascript:` 等を除去
- `strict`: HTML タグをすべて除去してプレーンテキストにする

---

### url-rewrite-worker（URL 書き換え）

メール本文内の URL をプロキシ経由の URL に書き換えます。

**有効化:** 外部サービス不要（プロキシは別途用意が必要）

**設定（`config/workers/conf/url-rewrite-worker.yaml`）:**
```yaml
proxy_base_url: "https://safelink.example.com/check?url="
url_encode: base64    # base64 | rawurl | none
rewrite_html: true
rewrite_text: true
skip_domains:
  - example.com       # 内部ドメインは書き換えない
```

---

### filesep-worker（添付ファイル分離）

添付ファイルを MinIO に保存し、メール本文にダウンロードリンクを挿入します。
ダウンロード認証方式は `mailshield.yaml` の `attachment_download.flows` で設定します。

**有効化:** 外部サービス不要（MinIO が必要）

**設定（`config/workers/conf/filesep-worker.yaml`）:**
```yaml
mode: inline          # inline（本文冒頭にリンク挿入）| separate（リンクのみのメールを送信）

inline_template:    /app/config/workers/conf/filesep_inline_template.txt
separate_template:  /app/config/workers/conf/filesep_separate_template.txt

link_expiry_hours: 72    # ダウンロードリンクの有効期限

min_size_bytes: 0        # 0 = すべてのサイズを分離
extensions: []           # [] = すべての拡張子を分離
# 特定拡張子のみ分離する場合:
# extensions: [.exe, .zip, .pdf, .docx]

frontend_url: http://localhost:3000
separate_from: mailshield-noreply@example.com  # separate モード時のみ使用
```

---

## Lua ワーカー（カスタム）

`workers_dir` に配置した Lua スクリプトを有効化できます。
書き方は [Lua ワーカー開発ガイド](../development/custom-worker-lua.md) を参照してください。

```yaml
# config/routes.d/10-inbound/route.yaml
workers:
  inspect:
    - name: my-custom-worker    # workers/my-custom-worker/init.lua が自動検出される
      enabled: true
      timeout_seconds: 5
```
