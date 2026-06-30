# ポリシー設定ガイド

MailShield のポリシーエンジンは、検査ワーカーの結果をもとにメールの処理を決定します。
ルールは YAML ファイルで定義し、ルートごとに別々のファイルを指定できます。

---

## 仕組み

```mermaid
flowchart TD
    A["検査ワーカー群\n（並列実行）"] -->|"InspectResult\nscore / detected / details"| B["変換ワーカー群\n（直列実行）"]
    B --> C["ポリシーエンジン\nrules を上から順に評価\n最初にマッチしたルールを実行"]
    C -->|deliver| D(["配送先 MTA へ SMTP 送信"])
    C -->|quarantine| E(["隔離\nMinIO + DB"])
    C -->|reject| F(["バウンス返却"])
    C -->|approval| G(["承認キュー保留"])
```

---

## ファイルの場所

ポリシーファイルはルートディレクトリに配置します。
`route.yaml` と同じディレクトリの `policy.yaml`（および `policy.lua`）が自動的に読み込まれます。

```
config/routes.d/
├── 10-inbound/
│   ├── route.yaml
│   └── policy.yaml   ← 受信ルートのポリシー
└── 20-outbound/
    ├── route.yaml
    └── policy.yaml   ← 送信ルートのポリシー
```

---

## ルールの書き方

```yaml
# config/routes.d/10-inbound/policy.yaml
rules:
  - name: av_detected          # ルール名（ログに記録される）
    condition: "av-worker.detected == true"
    action: quarantine

  - name: dlp_high_score
    condition: "dlp-worker.score >= 80"
    action: quarantine

  - name: default_deliver      # フォールバック（必ず最後に置く）
    condition: "true"
    action: deliver
    destination: "postfix:10025"
```

---

## アクション

| アクション | 説明 |
|-----------|------|
| `deliver` | `destination` で指定した MTA へ SMTP 送信する |
| `quarantine` | メールを隔離する。受信者に即時通知メールを送信（設定による） |
| `reject` | 送信者にバウンスを返す |
| `approval` | 承認キューに保留する（フェーズ4実装予定） |

### `deliver` の `destination`

```yaml
action: deliver
destination: "postfix:10025"     # ホスト:ポート
destination: "mailpit:1025"      # 開発環境（Mailpit）
destination: "10.0.0.1:25"       # IP 指定
destination: "postfix"           # ポート省略時は :25
```

---

## 条件式（condition）

ポリシーエンジンは現時点でシンプルな条件式をサポートしています。

### 定数

```yaml
condition: "true"    # 常にマッチ（デフォルトルールに使う）
condition: "false"   # 常にスキップ
```

### 等値比較（== ）

```yaml
condition: "av-worker.detected == true"
condition: "header-inspector.detected == false"
```

### スコア比較（>=）

```yaml
condition: "dlp-worker.score >= 80"
condition: "header-inspector.score >= 60"
```

---

## 条件式のキー

キーは `{ワーカー名}.{フィールド}` の形式です。

### 全ワーカー共通

| キー | 型 | 説明 |
|-----|---|------|
| `{worker}.detected` | bool | 検知フラグ |
| `{worker}.score` | int (0–100) | スコア |

### ワーカー別の `details` キー

| キー | ワーカー | 説明 |
|-----|---------|------|
| `av-worker.virus_name` | av-worker | 検知ウイルス名 |
| `header-inspector.reason` | header-inspector | 検知理由 |
| `url-worker.matched_url` | url-worker | マッチした URL |
| `dlp-worker.matched_pattern` | dlp-worker | マッチしたパターン名 |
| `subject-virus-inspector.reason` | subject-virus-inspector | 検知理由（Lua） |

---

## ルート別ポリシーの使い方

受信と送信で異なるポリシーを設定する典型例:

```yaml
# config/routes.d/10-inbound/policy.yaml（受信）
rules:
  - name: virus
    condition: "av-worker.detected == true"
    action: quarantine

  - name: default
    condition: "true"
    action: deliver
    destination: "mail.example.com:10025"
```

```yaml
# config/routes.d/20-outbound/policy.yaml（送信）
rules:
  - name: dlp_block
    condition: "dlp-worker.score >= 80"
    action: quarantine

  - name: default
    condition: "true"
    action: deliver
    destination: "mail.example.com:10025"
```

---

## 注意事項

- ルールは **上から順に評価**し、最初にマッチしたルールで処理が終わる
- `condition: "true"` は必ずフォールバックとして最後に置く（ないとマッチしない場合にメールが消える）
- ワーカーが無効（`enabled: false`）の場合、そのワーカーのキーは facts に存在しない → 条件が `false` になる
- `destination` のホスト名は Docker ネットワーク内のサービス名を直接使用できる
