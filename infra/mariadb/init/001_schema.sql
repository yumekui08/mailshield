-- MailShield OSS - MariaDB スキーマ
-- MariaDB 11.x

SET NAMES utf8mb4;
SET time_zone = '+00:00';

-- メールメッセージテーブル
-- smtp-inbound が受信したメールのメタデータ
CREATE TABLE IF NOT EXISTS mail_messages (
    id              CHAR(36)       NOT NULL,
    eml_path        VARCHAR(1024)  NOT NULL,
    from_address    VARCHAR(512)   NOT NULL,
    to_addresses    JSON           NOT NULL,          -- string[]
    subject         VARCHAR(998)   NOT NULL DEFAULT '',
    size_bytes      BIGINT         NOT NULL DEFAULT 0,
    has_attachment  TINYINT(1)     NOT NULL DEFAULT 0,
    rspamd_score    DECIMAL(6, 2)  NOT NULL DEFAULT 0.00,
    spf_result      ENUM('pass','fail','none') NOT NULL DEFAULT 'none',
    dkim_result     ENUM('pass','fail','none') NOT NULL DEFAULT 'none',
    dmarc_result    ENUM('pass','fail','none') NOT NULL DEFAULT 'none',
    status          ENUM('received','processing','delivered','quarantined','rejected','approval_pending')
                                   NOT NULL DEFAULT 'received',
    direction            ENUM('inbound','outbound','internal') NOT NULL DEFAULT 'inbound',
    processed_eml_path   VARCHAR(1024)  NULL DEFAULT NULL,  -- 変換後 EML の MinIO パス（archive 完了後に記録）
    received_at          DATETIME(6)    NOT NULL,
    created_at      DATETIME(6)    NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at      DATETIME(6)    NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_mail_messages_status (status),
    KEY idx_mail_messages_received (received_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 検査結果テーブル
-- 各検査ワーカーの結果を記録する
CREATE TABLE IF NOT EXISTS inspect_results (
    id          CHAR(36)     NOT NULL,
    message_id  CHAR(36)     NOT NULL,
    worker_name VARCHAR(128) NOT NULL,
    score       SMALLINT     NOT NULL DEFAULT 0,   -- 0-100
    detected    TINYINT(1)   NOT NULL DEFAULT 0,
    details     JSON         NOT NULL,
    created_at  DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_inspect_results_message (message_id),
    CONSTRAINT fk_inspect_results_message FOREIGN KEY (message_id) REFERENCES mail_messages (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ユーザーテーブル（スタンドアロン認証用）
-- OIDC 認証のユーザーはこのテーブルを使わない
CREATE TABLE IF NOT EXISTS users (
    id            CHAR(36)     NOT NULL,
    email         VARCHAR(512) NOT NULL,
    display_name  VARCHAR(256) NOT NULL DEFAULT '',
    password_hash VARCHAR(256) NOT NULL DEFAULT '',
    role          ENUM('admin','operator','viewer') NOT NULL DEFAULT 'viewer',
    is_active     TINYINT(1)   NOT NULL DEFAULT 1,
    created_at    DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at    DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_users_email (email)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- メールボックステーブル
-- 内部メールアドレスを管理する
CREATE TABLE IF NOT EXISTS mailboxes (
    id            CHAR(36)     NOT NULL,
    email_address VARCHAR(512) NOT NULL,
    display_name  VARCHAR(256) NOT NULL DEFAULT '',
    is_active     TINYINT(1)   NOT NULL DEFAULT 1,
    created_at    DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at    DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_mailboxes_email (email_address)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- メールボックス割り当てテーブル
-- ユーザーとメールボックスの関係（member/owner/admin）を管理する
CREATE TABLE IF NOT EXISTS mailbox_assignments (
    id          CHAR(36)                          NOT NULL,
    mailbox_id  CHAR(36)                          NOT NULL,
    user_id     CHAR(36)                          NOT NULL,
    role        ENUM('member','owner','admin')    NOT NULL,
    created_at  DATETIME(6)                       NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_mailbox_assignments (mailbox_id, user_id, role),
    KEY idx_mailbox_assignments_mailbox (mailbox_id),
    KEY idx_mailbox_assignments_user (user_id),
    CONSTRAINT fk_mailbox_assignments_mailbox FOREIGN KEY (mailbox_id) REFERENCES mailboxes (id) ON DELETE CASCADE,
    CONSTRAINT fk_mailbox_assignments_user    FOREIGN KEY (user_id)    REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 分離添付ファイルテーブル
-- filesep ワーカーが分離した添付ファイルのメタデータと管理情報を記録する
-- download_token はメッセージ単位の UUID でメール本文内リンクに使用する
CREATE TABLE IF NOT EXISTS mail_attachments (
    id               CHAR(36)                   NOT NULL,
    message_id       CHAR(36)                   NOT NULL,
    download_token   CHAR(36)                   NOT NULL,          -- メール内リンク用 UUID（メッセージ単位）
    filename         VARCHAR(512)               NOT NULL,
    content_type     VARCHAR(255)               NOT NULL DEFAULT '',
    size_bytes       BIGINT                     NOT NULL DEFAULT 0,
    storage_backend  ENUM('s3','spo')           NOT NULL DEFAULT 's3',
    storage_path     VARCHAR(1024)              NOT NULL,          -- バケット内オブジェクトキー
    is_disabled      TINYINT(1)                 NOT NULL DEFAULT 0, -- ダウンロード禁止フラグ
    download_mode    ENUM('simple','otp','auth') NOT NULL DEFAULT 'simple',
    created_at       DATETIME(6)                NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at       DATETIME(6)                NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    deleted_at       DATETIME(6)                NULL DEFAULT NULL,  -- ソフトデリート
    PRIMARY KEY (id),
    KEY idx_mail_attachments_message    (message_id),
    KEY idx_mail_attachments_token      (download_token),
    KEY idx_mail_attachments_deleted_at (deleted_at),
    CONSTRAINT fk_mail_attachments_message FOREIGN KEY (message_id) REFERENCES mail_messages (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 添付ファイル OTP トークンテーブル
-- Mode 2 (otp) のゲスト認証に使用する
CREATE TABLE IF NOT EXISTS attachment_otp_tokens (
    id             CHAR(36)     NOT NULL,
    download_token CHAR(36)     NOT NULL,  -- mail_attachments.download_token
    email          VARCHAR(512) NOT NULL,  -- OTP 受信メールアドレス（To/CC に含まれること確認済み）
    otp_hash       VARCHAR(256) NOT NULL,  -- bcrypt ハッシュ
    expires_at     DATETIME(6)  NOT NULL,
    used_at        DATETIME(6)  NULL DEFAULT NULL,
    created_at     DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_otp_tokens_download_token (download_token),
    KEY idx_otp_tokens_expires_at     (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 監査ログテーブル
CREATE TABLE IF NOT EXISTS audit_logs (
    id           CHAR(36)      NOT NULL,
    event_type   VARCHAR(64)   NOT NULL,
    actor_id     CHAR(36)      NULL,
    actor_email  VARCHAR(512)  NULL,
    target_type  VARCHAR(64)   NULL,
    target_id    VARCHAR(255)  NULL,
    detail       JSON          NULL,
    ip_address   VARCHAR(45)   NULL,
    created_at   DATETIME(6)   NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_audit_logs_event_type  (event_type),
    KEY idx_audit_logs_actor_id    (actor_id),
    KEY idx_audit_logs_created_at  (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- API キーテーブル
-- 機械間認証用のキーを管理する（平文は保存せず SHA-256 ハッシュのみ）
CREATE TABLE IF NOT EXISTS api_keys (
    id           CHAR(36)      NOT NULL,
    name         VARCHAR(128)  NOT NULL,
    key_hash     CHAR(64)      NOT NULL,   -- SHA-256 ハッシュ（平文は保存しない）
    role         ENUM('admin','operator','viewer') NOT NULL DEFAULT 'viewer',
    created_by   CHAR(36)      NULL,
    last_used_at DATETIME(6)   NULL,
    expires_at   DATETIME(6)   NULL,
    revoked_at   DATETIME(6)   NULL,
    created_at   DATETIME(6)   NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_api_keys_hash      (key_hash),
    KEY idx_api_keys_created_by (created_by)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

