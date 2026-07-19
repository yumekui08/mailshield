-- 設定 WebUI 化（ADR 008）ステップ②: ルーティング。
--
-- メールがどの検査・変換・ポリシーを通るかを決める合成単位。
-- priority 昇順で first-match（最初に match した 1 つだけを通す）。
-- inspect_json / transform_json はワーカーインスタンスの束ね（alias 参照）を
-- 可変リストとして JSON で持つ（ADR 008: 正規化列でなくドキュメント）。
--   [{ "alias": "av_internal", "enabled": true, "timeout_seconds": 30 }, ...]
CREATE TABLE IF NOT EXISTS routings (
    id             CHAR(36)     NOT NULL,
    name           VARCHAR(256) NOT NULL DEFAULT '',
    priority       INT          NOT NULL,               -- 昇順評価
    match_expr     TEXT         NOT NULL,               -- catch-all は "true"
    is_catchall    TINYINT(1)   NOT NULL DEFAULT 0,     -- 最終フォールバック（システム保証）
    is_enabled     TINYINT(1)   NOT NULL DEFAULT 1,
    policy_ref     VARCHAR(128) NOT NULL DEFAULT '',    -- 適用するポリシー名
    inspect_json   JSON         NOT NULL,               -- 検査インスタンスの束ね（並列）
    transform_json JSON         NOT NULL,               -- 変換インスタンスの束ね（直列・定義順）
    created_at     DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at     DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_routings_priority (priority)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
