# 提案: シナリオ×アクションの柔軟化と管理性向上（ドラフト）

作成: 2026-07-18 / ステータス: 💬 検討中（未確定・実装前）

m-FILTER のように「様々なシナリオに様々なアクションを、後から追加していける」ポリシー基盤へ
拡張し、かつその管理（作成・編集・検証・履歴）をしやすくするための設計案。
現行実装（`internal/policy/condition.go`・`engine.go`・`config/routes.d/<route>/policy.yaml`）を
土台に、段階的に拡張することを前提とする。

---

## 0. 現状の整理（何ができて何が足りないか）

**できていること**
- ルートごとの `policy.yaml`（first-match-wins）+ 任意の `policy.lua`
- 条件式: `==` `!=` `>= > <= <` `contains` `in_list` `&&`、`true/false`
- fact: ワーカーの `{name}.detected/score/{detail}`・`total_score`・`mail.*`（from/to/subject/size/has_attachment/direction）
- 名前付きリスト（`lists:` + 外部ファイル）で allow/deny 系を表現
- アクション: `deliver` / `reject` / `quarantine` / `approval` / `delay`
- `/simulate`（ドライラン）と Web UI プロキシ

**足りないこと（今回の拡張対象）**
| 区分 | 現状の制約 |
|-----|-----------|
| 条件 | AND（`&&`）のみ。OR は別ルールに分割。括弧・グルーピング・NOT グループなし |
| fact の種類 | 時間帯/曜日・宛先件数・添付の種類/個数/サイズ・特定ヘッダーの有無・言語・送信頻度（velocity）などが無い |
| アクション | メタ操作（タグ付け・ヘッダー追加・件名プレフィックス・BCC/転送・宛先追加・条件付き無害化・log-only）が無い |
| ルール属性 | `enabled` / 説明 / 優先度番号 / 有効期間（スケジュール）/ 作成者などのメタ情報が無い |
| 管理 | policy.yaml を手編集。UI エディタ・変更履歴/ロールバック・変更監査・ヒット件数の可視化が無い |
| 再利用 | リスト以外に「条件セット」「シナリオテンプレート」の再利用単位が無い |

---

## 1. Layer 1 — 条件モデルの強化

`condition.go` は素直な再帰下降に置き換えられる規模。後方互換を保ちつつ拡張する。

### 1-1. 論理式の拡張
- `||`（OR）・`(...)`（グルーピング）・`not (...)` を追加
- 既存の `&&`・単項条件はそのまま動く（後方互換）
- 例: `header-inspector.detected == true && (mail.direction == inbound || attachment.count > 0)`

### 1-2. fact の追加（buildFacts の拡張）
外部依存を増やさず、受信時に既知の情報から導出できるものを優先する。

| 追加 fact | 型 | 用途例 |
|-----------|---|-------|
| `mail.recipient_count` | int | 大量宛先の外部送信を承認へ |
| `mail.hour` / `mail.weekday` | int | 業務時間外の送信をディレイ/承認 |
| `mail.has_header.<name>` | bool | `List-Unsubscribe` 等の有無 |
| `mail.header.<name>` | string | 任意ヘッダー値で `contains`/`in_list` |
| `attachment.count` | int | 添付ゼロ/多数の判定 |
| `attachment.total_bytes` | int | 大容量添付のルーティング |
| `attachment.has_type.<ext>` | bool | 特定拡張子の存在 |
| `mail.tls` | bool | 受信時 TLS 有無（Postfix ヘッダーから） |
| `mail.velocity_1h` | int | 同一送信者の直近1時間の通数（DB 集計・送信側の異常検知） |

> `velocity_*` のみ DB 参照が要るため、有効化時だけ集計する（デフォルト無効）。

### 1-3. 比較演算子の追加
- `matches`（正規表現）・`starts_with` / `ends_with`
- `in_cidr`（IP 系 fact 向け）
- 数値の `between a..b`

---

## 2. Layer 2 — アクションモデルの強化（合成可能に）

現状の「1 ルール = 1 アクション」から、**終端アクション**（配送/拒否/隔離/承認/ディレイ = ルール評価を止める）と
**非終端アクション**（タグ・ヘッダー・BCC など = 適用して次ルールへ続行）に分ける。

```yaml
rules:
  - name: tag_external
    condition: "mail.direction == inbound"
    actions:                      # 複数・非終端を許可
      - type: add_subject_prefix
        value: "[EXTERNAL] "
      - type: add_header
        name: X-MailShield-Origin
        value: external
    continue: true                # 次のルールも評価する（非終端）

  - name: dlp_high
    condition: "dlp-worker.score >= 80"
    action: quarantine            # 従来どおり単一アクション（終端）も可
```

### 追加アクション候補
| アクション | 種別 | 説明 |
|-----------|------|------|
| `add_header` / `remove_header` | 非終端 | 下流クライアント向けのタグ（`X-Spam-Flag` 等） |
| `add_subject_prefix` | 非終端 | `[EXTERNAL]` `[要注意]` 等（m-FILTER 的な可視タグ） |
| `bcc` / `add_recipient` | 非終端 | 監査用の控え送付（※エンベロープ操作は Postfix 委任が原則。ここでは「MailShield が別配送で控えを送る」実装に限定） |
| `redirect` | 終端 | 宛先を差し替えて配送（誤送信の隔離代替） |
| `strip_attachments` | 非終端 | 条件付きで添付除去（macro-strip の汎用版） |
| `notify` | 非終端 | 任意テンプレートで通知（管理者/上長） |
| `hold_for_review` | 終端 | approval の汎用版（承認者を条件で切替） |
| `log_only` | 非終端 | 監査ログのみ（ルールの試験導入・シャドー運用） |

> 既存の `EvaluateAndAct` は `ActionResult` を返す形に変更済み。ここを
> `[]ActionResult`（順次適用 + 終端で停止）へ発展させる。ワーカーの
> `TransformWorker` インターフェースは変更しない（アクションはポリシー層で完結）。

---

## 3. Layer 3 — ルール/ポリシーの管理属性

各ルールにメタ情報を付与し、「シナリオを増やしても管理できる」状態にする。

```yaml
rules:
  - name: after_hours_bulk_outbound
    description: "業務時間外の大量宛先の外部送信は上長承認へ"
    enabled: true
    priority: 100                 # 明示的な優先度（ファイル順依存を脱却）
    schedule:                     # 任意: 有効期間・時間帯
      active_hours: "18:00-08:00"
      weekdays: [sat, sun]
    condition: "mail.direction == outbound && mail.recipient_count >= 20"
    action: approval
    tags: [dlp, after-hours]      # 分類・フィルタ用
```

- `enabled` によりデプロイせず ON/OFF（シャドー運用と相性良い）
- `priority` 昇順で評価（同値はファイル/定義順）。ファイル分割の負担を軽減
- `tags` で UI 上のグルーピング・検索
- 監査のため各ルールに安定した `id`（YAML の `name` を SSOT にするか UUID 付与）

---

## 4. Layer 4 — 管理 UX（Web UI ポリシーエディタ）

「手編集 YAML」から「UI で編集・検証・履歴」へ。既存の `/simulate` と RBAC を活かす。

### 4-1. ポリシーエディタ画面
- ルート選択 → ルール一覧（`priority` 順・drag で並べ替え・`enabled` トグル）
- 条件ビルダー（fact ドロップダウン + 演算子 + 値。生の式編集にも切替可）
- アクション選択（終端/非終端・パラメータ入力）
- 右ペインで**その場シミュレート**（EML 貼り付け → どのルールが発火するか・理由表示）
- 既存メモリの UI 嗜好に合わせフラット・高密度・ページング完備

### 4-2. バリデーションと安全策
- 保存前にサーバ側でパース検証（未定義 fact・未定義リスト・到達不能ルールを警告）
- 「デフォルトルール（`condition: "true"`）が最後にあるか」を必須チェック（メール消失防止）
- 適用前後の diff プレビュー

### 4-3. バージョニング・監査・4-eyes
- ポリシー変更を `policy_versions` に保存（誰が・いつ・diff）→ ロールバック可能
- `audit_logs` に `policy.updated` を記録（既存監査基盤を利用）
- 任意で**ポリシー変更自体の承認フロー**（editor が提案 → admin が承認で反映）

### 4-4. ロール追加
- `policy-editor`（ポリシー閲覧・編集提案）と `admin`（承認・反映）を分離できるように

### 4-5. ルールのヒット可視化
- 各ルールの発火件数を `metrics`/DB に集計 → 一覧に「直近7日ヒット数」を表示
- 「1件も当たっていないルール」「全部を飲み込むルール」を可視化して棚卸し

---

## 5. Layer 5 — 再利用（テンプレート/シナリオ）

- **名前付き条件（マクロ）**: よく使う条件式に名前を付けて参照（`when: is_external_bulk`）
- **シナリオテンプレート**: 「なりすまし対策」「PPAP 対策」「誤送信対策」等をプリセットとして同梱し、
  UI から「追加 → 値だけ調整」で導入できるように（m-FILTER の運用感）
- リスト管理 UI（送信元/ドメイン/IP の allow/deny を画面で編集 → `in_list` が参照）

---

## 6. その他の追加機能案（ポリシー以外）

| 案 | 価値 | 規模 | 備考 |
|----|------|------|------|
| allow/deny リストの Web UI 管理 | 高 | 小 | `in_list` の集合を画面で編集。運用者が最も触る |
| `[EXTERNAL]` 件名タグ（Layer2 の add_subject_prefix） | 高 | 小 | 受信外部メールの可視化。導入効果が大きく低コスト |
| ルール別ヒット件数ダッシュボード | 高 | 中 | 棚卸し・チューニングの土台。metrics 拡張 |
| 隔離ダイジェストメール | 中 | 中 | 既存バックログ。日次で受信者へ隔離一覧を通知 |
| データ保持ポリシー + Legal Hold | 中 | 中 | 既存バックログ。ルート/ステータス別 TTL + 日次削除 |
| 全文検索（Elasticsearch/OpenSearch） | 中 | 大 | 既存バックログ。運用件数は MariaDB のまま |
| 送信レート/異常送信検知（velocity fact 連動） | 中 | 中 | 乗っ取り送信の早期検知。Layer1 の `velocity_*` と対 |
| 通知テンプレートの管理 UI | 中 | 中 | 隔離/承認/OTP 文面を画面で編集・多言語化 |
| Postfix 連携レシピの文書化 | 中 | 小 | 強制 BCC 等 MTA 側機能の構成例（既存バックログ） |
| ポリシー変更の 4-eyes 承認 | 中 | 中 | Layer4-3。統制が要る組織向け |
| SCIM / MS Graph 同期 | 低〜中 | 大 | 既存バックログ。LDAP 基盤の Provisioner 再利用 |
| 添付 SHA-256 の allow/deny リスト | 低 | 小 | サンドボックス見送りの軽量代替（既知ハッシュのブロック） |

---

## 7. 推奨フェーズ分け（案）

段階導入し、各段で後方互換を維持する。

1. **P1（土台・低リスク）**: 条件の `||`/`()`/`not`・ルール `enabled`/`description`/`priority`、
   非終端アクション `add_header`/`add_subject_prefix`/`log_only`。すべて `policy.yaml` 内で完結し
   UI 変更不要。まず `[EXTERNAL]` タグと allow/deny リスト UI で体験を作る。
2. **P2（管理性）**: Web UI ポリシーエディタ（一覧・条件ビルダー・その場シミュレート・保存時検証）+
   `policy.updated` 監査 + ルール別ヒット件数。
3. **P3（fact 拡張）**: 時間帯/宛先件数/添付属性/ヘッダー系 fact、`bcc`/`redirect`/`strip_attachments`/`notify` アクション。
4. **P4（統制・再利用）**: バージョニング/ロールバック、ポリシー変更の 4-eyes、シナリオテンプレート、
   `policy-editor` ロール、velocity/異常送信検知。

---

## 8. 設計上の留意点

- **後方互換**: 既存 `policy.yaml`（`action:` 単一・`&&` のみ）はそのまま動くこと。`actions:`（複数）は追加構文。
- **CLAUDE.md 準拠**: `TransformWorker`/`InspectWorker` インターフェースは変更しない。アクション合成はポリシー層で閉じる。
  interface はコンシューマー側で定義、`internal/domain/` に外部依存を持ち込まない方針を維持。
- **安全側デフォルト**: 「マッチするルールなし」は現状どおり 550（メール消失防止）を堅持。UI 保存時に
  デフォルトルールの存在を必須検証。
- **シミュレート同値性**: 非終端アクションを入れても `/simulate` が実配送と同じ順序・結果を返すこと。
