-- 設定 WebUI 化（ADR 008）: ポリシーを再利用可能な名前付きインスタンスにする。
-- ワーカーインスタンスと同様、ルーティングから alias で参照される。
-- content は policy.yaml と同形のルール定義（YAML テキスト）。
CREATE TABLE IF NOT EXISTS policy_instances (
    id           CHAR(36)     NOT NULL,
    alias        VARCHAR(64)  NOT NULL,               -- ルーティングの policy_ref から参照
    display_name VARCHAR(256) NOT NULL DEFAULT '',
    content      MEDIUMTEXT   NOT NULL,               -- lists + rules（YAML）
    created_at   DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at   DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_policy_instances_alias (alias)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
