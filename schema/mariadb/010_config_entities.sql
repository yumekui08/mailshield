-- 設定 WebUI 化（ADR 008）ステップ①: ワーカーインスタンスと設定変数。
--
-- ワーカーインスタンス: ワーカー型（コード側の実装）＋型固有設定＋名前 を
-- 名前付き・再利用可能な部品として持つ。ルーティングから alias で参照される。
--   id           … 不変の内部参照（UUID）
--   alias        … 条件 DSL と検査結果のキーに使う短い安定ハンドル（rename-safe）
--   display_name … 画面表示用（日本語可・変更可）
CREATE TABLE IF NOT EXISTS worker_instances (
    id            CHAR(36)     NOT NULL,
    alias         VARCHAR(64)  NOT NULL,
    display_name  VARCHAR(256) NOT NULL DEFAULT '',
    worker_type   VARCHAR(64)  NOT NULL,               -- filesep-worker / av-worker / lua:<dir> 等
    kind          ENUM('inspect','transform') NOT NULL,
    config_json   JSON         NOT NULL,               -- 型固有設定（不透明・アプリ層で検証）
    default_timeout_seconds INT NOT NULL DEFAULT 30,   -- ルーティング側で上書き可
    is_enabled    TINYINT(1)   NOT NULL DEFAULT 1,
    created_at    DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at    DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_worker_instances_alias (alias),
    KEY idx_worker_instances_kind (kind)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 設定変数: ルーティング match・ポリシー条件・ワーカー設定・配送先などから ${VAR} で
-- 参照する共有値（非機密・環境依存）。展開は設定ロード時に一度だけ行う。
-- ※ シークレット（パスワード等）はここに入れず OS 環境変数のままにすること。
CREATE TABLE IF NOT EXISTS config_variables (
    id          CHAR(36)     NOT NULL,
    var_key     VARCHAR(128) NOT NULL,                 -- ${VAR} の VAR。英数・_ のみ想定
    value       TEXT         NOT NULL,
    description VARCHAR(512) NOT NULL DEFAULT '',
    created_at  DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at  DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_config_variables_key (var_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
