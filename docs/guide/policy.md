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

### Web UI での編集

Web UI の「ポリシー」画面（`admin` は編集、`operator` は閲覧）からルールを追加・編集・
並べ替え・有効/無効の切り替えができる。保存すると `policy.yaml` が書き換えられ、
smtp-gateway に即座に反映される（`POST /reload`）。反映に失敗した場合は変更を巻き戻し、
エラー内容を表示する。各ルールの発火（ヒット）件数も一覧に表示され、ルールの棚卸しに使える。

> [!NOTE]
> UI で保存すると `policy.yaml` は再生成され、**手書きのコメントは失われる**（UI が SSOT になる）。
> コメントを残したい場合はファイルを直接編集し、UI からの保存を避けること。

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

### ルールのメタ属性

各ルールには以下の任意属性を付けられる。

| 属性 | 説明 |
|------|------|
| `description` | ルールの説明（管理用）。動作には影響しない |
| `enabled` | `false` にすると評価対象から除外する（省略時は有効）。デプロイせず ON/OFF できる |
| `priority` | 評価順（**昇順・小さいほど先**）。同じ値はファイル定義順を保持する。省略時は 0。既存ファイル（priority なし）はファイル順のまま |
| `tags` | 分類・検索用のラベル（文字列リスト）。動作には影響しない |

```yaml
rules:
  - name: attachment_review
    description: "添付付きの外部送信は上長承認へ"
    enabled: true
    priority: 100
    tags: [dlp]
    condition: "mail.direction == outbound && mail.has_attachment == true"
    action: approval
```

---

## アクション

アクションは **終端アクション**（適用するとルール評価を止める）と **非終端アクション**
（メールを加工して次のルール評価へ続行する）の 2 種類がある。

### 終端アクション

| アクション | 説明 |
|-----------|------|
| `deliver` | `destination` で指定した MTA へ SMTP 送信する |
| `quarantine` | メールを隔離する。受信者に即時通知メールを送信（設定による） |
| `reject` | 送信者にバウンスを返す |
| `approval` | 承認キューに保留する。承認者はメールボックスの admin 割り当て（優先）→ ユーザー個人の `approver_id` → `approval.global_approver_email` の順で解決される（詳細: [設定リファレンスの approval](../specs/configuration.md#approval)） |
| `delay` | 送信を一定時間保留する（送信ディレイ）。`delay_minutes` で保留時間を指定（省略時 5 分）。保留中は送信者が Web UI から取消・即時送信でき、時間が来ると自動送信される |
| `redirect` | 宛先を `value`（差し替え先アドレス・カンマ区切りで複数可）に差し替えて配送する。誤送信の受け皿や監査用アドレスへの付け替えに使う。`destination` で配送先 MTA も指定できる |

### 非終端アクション

| アクション | パラメータ | 説明 |
|-----------|-----------|------|
| `add_subject_prefix` | `value` | 件名の先頭に文字列を付加する（例: `[EXTERNAL] `）。受信外部メールの可視化に使う |
| `add_header` | `name` / `value` | ヘッダーを 1 行追加する（例: `X-MailShield-Origin: external`）。下流メールクライアントの振り分けに使う |
| `remove_header` | `name` | 指定名のヘッダー（折り畳み継続行を含む）をすべて削除する |
| `strip_attachments` | `value`（任意） | 添付ファイルを除去する。`value` に拡張子をカンマ区切りで指定するとその拡張子のみ、空なら全添付を除去する |
| `log_only` | — | メールを変更せず監査ログのみ残す（ルールのシャドー運用・試験導入に使う） |

非終端アクションを持つルールは、アクション適用後も**次のルールの評価を続行する**。
これにより「まずタグを付け、後続ルールでそのタグを条件に判定する」といった多段処理ができる。

### 複数アクション（`actions:`）

1 ルールに複数のアクションを順に適用するには `actions:` リストを使う。上から順に適用され、
最初の終端アクションで評価が止まる（それ以降の要素は無視される）。

```yaml
rules:
  # 受信外部メールに [EXTERNAL] タグとヘッダーを付与（非終端のみ → 次のルールへ続行）
  - name: tag_external
    condition: "mail.direction == inbound"
    actions:
      - type: add_subject_prefix
        value: "[EXTERNAL] "
      - type: add_header
        name: X-MailShield-Origin
        value: external

  - name: default_deliver
    condition: "true"
    action: deliver          # 単一 action: も従来どおり使える（後方互換）
```

### `delay` の `delay_minutes`

送信ディレイは主に送信メール（outbound ルート）の誤送信対策に使う。保留時間を過ぎると api-server のバックグラウンドワーカーが自動的に配送する。

```yaml
rules:
  # 全送信メールを 3 分保留（送信取消の猶予）
  - name: outbound_delay
    condition: "mail.direction == outbound"
    action: delay
    delay_minutes: 3
```

保留中のメールは Web UI の「送信待ち」画面に表示され、送信者（送信元メールボックスの owner）が「今すぐ送信」または「取消」できる。取消したメールは配送されない（status=rejected）。

### `deliver` の `destination`

`destination` には **deliverer 名**（`mailshield.yaml` の `deliverers` で定義）または **host:port** を指定できる。名前が優先して解決される。

```yaml
action: deliver
destination: "sendgrid"          # deliverer 名（deliverers.sendgrid を使用。STARTTLS + AUTH 可）
destination: "postfix:10025"     # ホスト:ポート（平文 SMTP）
destination: "mailpit:1025"      # 開発環境（Mailpit）
destination: "10.0.0.1:25"       # IP 指定
destination: "postfix"           # ポート省略時は :25（同名の deliverer があればそちらが優先）
```

`destination` を省略した場合は `deliverers.default` が使われる。`deliverers.default` が未定義なら `reinject.host:port` にフォールバックする。

ルールごとに deliverer を分けられるため、「outbound ルートの通常メールは SendGrid、社内向けは Postfix」のような振り分けができる:

```yaml
rules:
  - name: internal_relay
    condition: "header-worker.internal == true"
    action: deliver
    destination: "postfix"        # deliverers.postfix
  - name: default_send
    condition: "true"
    action: deliver
    destination: "sendgrid"       # deliverers.sendgrid（STARTTLS + SMTP AUTH）
```

SendGrid / Amazon SES 等の外部 SMTP エンドポイントの定義方法は [設定リファレンスの deliverers](../specs/configuration.md#deliverers) を参照。

---

## 条件式（condition）

条件は 1 行で書きます。`&&`（論理積）・`||`（論理和）・`not`（否定）・`( )`（グルーピング）で
複数の比較を組み合わせられます。

### 演算子

| 演算子 | 例 | 説明 |
|--------|-----|------|
| `true` / `false` | `condition: "true"` | 定数（デフォルトルールに使う） |
| `==` / `!=` | `av-worker.detected == true` | 等値・不等（ブールは大文字小文字を無視） |
| `>=` `>` `<=` `<` | `dlp-worker.score >= 80` | 数値比較 |
| `contains` | `mail.subject contains 請求書` | 部分文字列（大文字小文字を無視） |
| `starts_with` / `ends_with` | `mail.from ends_with @example.com` | 前方一致・後方一致（大文字小文字を無視） |
| `matches` | `mail.subject matches ^Invoice #\d+$` | 正規表現マッチ（Go RE2 構文） |
| `in_list` | `mail.from_domain in_list freemail` | 名前付きリストに含まれるか（下記） |
| `&&` | `A == 1 && B >= 50` | 論理積（AND） |
| `\|\|` | `A == 1 \|\| B == 1` | 論理和（OR） |
| `not` | `not (A == 1 && B == 1)` | 否定 |
| `( )` | `(A \|\| B) && C` | グルーピング |

**優先順位（高い順）:** `not` > `&&` > `||`。括弧で上書きできます。

```yaml
# 例: 添付があり、かつ (ヘッダー検知 または URL 検知) のとき隔離
- name: attach_and_suspicious
  condition: "mail.has_attachment == true && (header-inspector.detected == true || url-worker.detected == true)"
  action: quarantine
```

> [!NOTE]
> 構造トークン（`&&` `||` `(` `)`）は値の中に含めないでください（例: 件名に `(` を含む照合は不可）。
> 文字列先頭の `not ` は否定演算子として解釈されます（値の途中の "not" は影響しません）。

### 名前付きリスト（lists）と `in_list`

`in_list` で参照する集合を policy.yaml のトップレベル `lists` に定義します。インライン（`values`）と外部ファイル（`file`・policy.yaml からの相対パス・1 行 1 要素・`#` はコメント）を併用でき、和集合になります。

```yaml
lists:
  freemail:
    file: ../../lists/freemail-domains.txt   # 同梱のフリーメールドメイン一覧
    values: [example-free.test]              # 追加のインライン要素
  deny_domains:
    values: [evil.example, phishing.test]

rules:
  # 個人ドメイン（フリーメール）宛の送信は上長承認へ
  - name: freemail_to_approval
    condition: "mail.direction == outbound && mail.to_domains in_list freemail"
    action: approval

  # 拒否ドメイン宛はブロック
  - name: deny_domain_block
    condition: "mail.to_domains in_list deny_domains"
    action: reject
```

`in_list` はメールアドレス（`@` を含む値）の場合、そのドメイン部でも照合します。

### 合算スコア（total_score）

全検査ワーカーの `score` の合計が `total_score` として使えます。個々のワーカーでは閾値に届かない複合的な兆候をまとめて判定する（Mail Detox 的な運用）のに使います。

```yaml
- name: suspicious_total
  condition: "total_score >= 100"
  action: quarantine
```

---

## 条件式のキー

### ワーカー由来（`{ワーカー名}.{フィールド}`）

| キー | 型 | 説明 |
|-----|---|------|
| `{worker}.detected` | bool | 検知フラグ |
| `{worker}.score` | int (0–100) | スコア |
| `total_score` | int | 全ワーカーの score 合計 |

ワーカー別の `details` キーの例:

| キー | ワーカー | 説明 |
|-----|---------|------|
| `av-worker.virus_name` | av-worker | 検知ウイルス名 |
| `header-inspector.reason` | header-inspector | 検知理由 |
| `url-worker.matched_url` | url-worker | マッチした URL |
| `attachment-inspector.reasons` | attachment-inspector | 検知理由の一覧 |

### メール属性（`mail.*`）

| キー | 型 | 説明 |
|-----|---|------|
| `mail.from` | string | エンベロープ送信者アドレス（小文字） |
| `mail.from_domain` | string | 送信者のドメイン部（小文字） |
| `mail.to` | string | 全宛先をカンマ連結（小文字） |
| `mail.to_domains` | string | 全宛先のドメインをカンマ連結（小文字） |
| `mail.subject` | string | 件名 |
| `mail.size_bytes` | int | メールサイズ（バイト） |
| `mail.has_attachment` | bool | 添付の有無 |
| `mail.direction` | string | `inbound` / `outbound` |
| `mail.recipient_count` | int | 宛先（RCPT TO）の件数 |
| `mail.hour` | int | 受信時刻の時（0–23・**UTC**） |
| `mail.weekday` | string | 受信曜日（`sun` `mon` … `sat`・**UTC**） |
| `mail.has_header.<名前>` | bool | 指定ヘッダー（小文字）が存在するか。例: `mail.has_header.list-unsubscribe == true` |
| `mail.header.<名前>` | string | 指定ヘッダーの値（小文字名で参照）。例: `mail.header.x-priority == 1` |

> [!NOTE]
> `mail.to` / `mail.to_domains` は宛先が複数の場合カンマ連結された1つの文字列です。
> 「宛先のいずれかがフリーメール」を判定するには `in_list`（アドレス/ドメイン単位で照合）を使ってください。

> [!NOTE]
> `mail.hour` / `mail.weekday` は **UTC** 基準です。業務時間帯で判定する場合はタイムゾーン差を考慮してください。

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
