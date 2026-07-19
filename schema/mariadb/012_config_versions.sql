-- 設定 WebUI 化（ADR 008）③-2: 設定スナップショットのバージョニングとアクティブ版ポインタ。
--
-- config_versions: エンティティ（ワーカーインスタンス・変数・ルーティング）を 1 つの
-- canonical スナップショット（JSON）に固めたもの。検証済みのみを積む。gateway はこの
-- content を読んでインメモリにパイプラインを構築する（メール経路では DB を引かない）。
--   checksum : content の SHA-256（同一内容の版を重複生成しないため／ポーリング差分検知）
--   source   : 生成元（"ui" = WebUI 編集 / "file" = ファイル seed）
CREATE TABLE IF NOT EXISTS config_versions (
    id         CHAR(36)    NOT NULL,
    checksum   CHAR(64)    NOT NULL,          -- SHA-256 hex
    source     VARCHAR(16) NOT NULL DEFAULT 'ui',
    author     VARCHAR(256) NOT NULL DEFAULT '',
    content    MEDIUMTEXT  NOT NULL,          -- canonical スナップショット JSON
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_config_versions_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- config_active: アクティブ版ポインタ（単一行・id は常に 1）。この 1 行の更新が
-- 「原子的な切替（consensus point）」になる。ロールバックはポインタを戻すだけ。
CREATE TABLE IF NOT EXISTS config_active (
    id         TINYINT     NOT NULL DEFAULT 1,
    version_id CHAR(36)    NULL DEFAULT NULL,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    CONSTRAINT chk_config_active_singleton CHECK (id = 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
