# Lua カスタムワーカー開発ガイド

Lua スクリプトで検査ワーカー・変換ワーカーを実装できます。
Go のビルドは不要で、スクリプトを置くだけで有効化できます。

---

## ファイルの配置

```
workers/
└── my-worker/
    └── init.lua         ← このファイルが読み込まれる
```

`workers.workers_dir`（デフォルト: `./workers`）で指定したディレクトリ以下に、
ワーカー名と同じディレクトリを作成し `init.lua` を置きます。

```yaml
# config/routes.d/10-inbound/route.yaml
workers:
  inspect:
    - name: my-worker
      enabled: true
      timeout_seconds: 10
```

---

## 検査ワーカー（inspect）

```lua
local M = {}

M.name = "my-worker"   -- ドキュメント用（ワーカー名はディレクトリ名が正）
M.type = "inspect"

function M.inspect(mail, config)
    -- mail テーブルのフィールドを参照して検査する
    local subject = mail.subject or ""

    if string.find(string.lower(subject), "confidential", 1, true) then
        return {
            detected = true,
            score    = 80,
            details  = { reason = "subject contains 'confidential'" },
        }
    end

    return { detected = false, score = 0, details = {} }
end

return M
```

### 戻り値

| フィールド | 型 | 必須 | 説明 |
|-----------|---|------|------|
| `detected` | bool | ✓ | 検知フラグ。ポリシーの `{name}.detected` に対応 |
| `score` | int (0–100) | ✓ | スコア。ポリシーの `{name}.score` に対応 |
| `details` | table | ✓ | 任意の詳細情報。ポリシーの `{name}.{key}` でアクセス可 |

---

## 変換ワーカー（transform）

```lua
local M = {}

M.name = "my-transformer"
M.type = "transform"

function M.transform(mail, config)
    -- mail テーブルのフィールドを変更して返す
    -- subject を変更すると EML の Subject ヘッダも自動的に書き換えられる
    local prefix = config.prefix or "[NOTICE] "
    local subject = mail.subject or ""

    if string.find(string.lower(subject), "urgent", 1, true) then
        mail.subject = prefix .. subject
    end

    return mail
end

return M
```

### 戻り値

変更後の `mail` テーブルをそのまま返します。
`subject` フィールドを変更すると EML の `Subject:` ヘッダが自動的に書き換えられます。

---

## `mail` テーブルのフィールド

```lua
mail.message_id      -- string: 内部 UUID
mail.subject         -- string: メール件名（変更可）
mail.from            -- string: 送信者アドレス
mail.to              -- table:  宛先アドレスの配列 (1-indexed)
mail.size_bytes      -- number: EML サイズ（バイト）
mail.has_attachment  -- bool:   添付ファイルあり
mail.rspamd_score    -- number: Rspamd スパムスコア
mail.auth_results    -- table:
  mail.auth_results.spf   -- string: "pass" | "fail" | "softfail" | "neutral" | "none"
  mail.auth_results.dkim  -- string: "pass" | "fail" | "none"
  mail.auth_results.dmarc -- string: "pass" | "fail" | "none"
```

---

## `config` テーブル

`config/workers/conf/{name}.yaml` の内容が Lua テーブルとして渡されます。

```yaml
# config/workers/conf/my-worker.yaml
prefix: "[NOTICE] "
keywords:
  - confidential
  - urgent
threshold: 80
```

```lua
function M.inspect(mail, config)
    local keywords = config.keywords or {}  -- table（Lua の配列）
    local threshold = config.threshold or 50  -- number
    local prefix = config.prefix or ""         -- string
    ...
end
```

設定ファイルが存在しない場合、`config` は空テーブル `{}` になります。

---

## エラーハンドリング

ワーカーが Lua エラーを起こした場合、smtp-gateway はそのワーカーをスキップして処理を継続します（WARN ログ）。
タイムアウトは `mailshield.yaml` の `timeout_seconds` で設定します。

---

## 実装例：SPF fail を検知する検査ワーカー

```lua
-- workers/spf-fail-inspector/init.lua
local M = {}
M.name = "spf-fail-inspector"
M.type = "inspect"

function M.inspect(mail, config)
    local spf = mail.auth_results and mail.auth_results.spf or "none"

    if spf == "fail" then
        return {
            detected = true,
            score    = config.score or 50,
            details  = { spf_result = spf },
        }
    end

    return { detected = false, score = 0, details = {} }
end

return M
```

```yaml
# config/workers/conf/spf-fail-inspector.yaml
score: 50
```

```yaml
# config/routes.d/10-inbound/route.yaml
workers:
  inspect:
    - name: spf-fail-inspector
      enabled: true
      timeout_seconds: 5
```

```yaml
# config/routes.d/10-inbound/policy.yaml
rules:
  - name: spf_fail
    condition: "spf-fail-inspector.detected == true"
    action: quarantine
```

---

## デバッグ

```bash
# smtp-gateway のログを確認（ワーカー名でフィルタ）
docker compose -f docker/docker-compose.yml logs smtp-gateway | grep "my-worker"

# ログレベルを debug にして詳細を確認
# config/mailshield.yaml
# log:
#   level: debug
```
