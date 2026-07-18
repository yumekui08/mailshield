-- マイグレーション 007: ポリシー変更履歴テーブル（既存 DB 向け）
-- 001_schema.sql に同内容が含まれる。既存 DB にのみ手動適用する。

CREATE TABLE IF NOT EXISTS policy_versions (
    id           CHAR(36)      NOT NULL,
    route_dir    VARCHAR(255)  NOT NULL,
    content      MEDIUMTEXT    NOT NULL,
    actor_id     CHAR(36)      NULL,
    actor_email  VARCHAR(512)  NULL,
    created_at   DATETIME(6)   NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_policy_versions_route (route_dir, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
