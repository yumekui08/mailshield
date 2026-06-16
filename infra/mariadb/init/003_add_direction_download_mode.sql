-- Migration: direction / download_mode / attachment_otp_tokens
-- mail_messages に direction カラムを追加
ALTER TABLE mail_messages
    ADD COLUMN IF NOT EXISTS direction ENUM('inbound','outbound','internal') NOT NULL DEFAULT 'inbound'
    AFTER status;

-- mail_attachments に download_mode カラムを追加
ALTER TABLE mail_attachments
    ADD COLUMN IF NOT EXISTS download_mode ENUM('simple','otp','auth') NOT NULL DEFAULT 'simple'
    AFTER is_disabled;

-- 添付ファイル OTP トークンテーブル
-- Mode 2 (otp) のゲスト認証に使用する
CREATE TABLE IF NOT EXISTS attachment_otp_tokens (
    id            CHAR(36)     NOT NULL,
    download_token CHAR(36)   NOT NULL,  -- mail_attachments.download_token
    email         VARCHAR(512) NOT NULL,  -- OTP 受信メールアドレス（To/CC に含まれること確認済み）
    otp_hash      VARCHAR(256) NOT NULL,  -- bcrypt ハッシュ
    expires_at    DATETIME(6)  NOT NULL,
    used_at       DATETIME(6)  NULL DEFAULT NULL,
    created_at    DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_otp_tokens_download_token (download_token),
    KEY idx_otp_tokens_expires_at     (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
