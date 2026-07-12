-- 004: 承認対象の複数メールボックス化 + 通知の宛先ごと再送管理
--
-- 前提: 003_approval_mailbox.sql 適用済みであること（未適用の環境は 003 → 004 の順で流す）。
--
-- 変更内容:
--   1. approval_request_mailboxes を新設し、依頼 1 件が複数のメールボックスを
--      対象にできるようにする（受信メールの複数宛先対応）。いずれかのメールボックスの
--      admin が承認すれば配送される。既存の approval_requests.mailbox_email は移行して廃止。
--      将来の多段承認ワークフローはこのテーブルに stage/order 列を追加して拡張する想定。
--   2. approval_notifications を新設し、承認者への依頼通知メールの送信状態を
--      宛先ごとに管理する（一部失敗時は失敗した宛先のみ再送）。
--
-- 既存環境への適用: mysql -u root -p mailshield < 004_approval_targets_notifications.sql

CREATE TABLE IF NOT EXISTS approval_request_mailboxes (
    id                  CHAR(36)     NOT NULL,
    approval_request_id CHAR(36)     NOT NULL,
    mailbox_email       VARCHAR(320) NOT NULL,
    created_at          DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_approval_request_mailboxes (approval_request_id, mailbox_email),
    KEY idx_approval_request_mailboxes_mailbox (mailbox_email),
    CONSTRAINT fk_approval_request_mailboxes_request
        FOREIGN KEY (approval_request_id) REFERENCES approval_requests (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 既存の mailbox_email を子テーブルへ移行
INSERT INTO approval_request_mailboxes (id, approval_request_id, mailbox_email)
SELECT UUID(), id, mailbox_email
  FROM approval_requests
 WHERE mailbox_email IS NOT NULL;

ALTER TABLE approval_requests
    DROP CONSTRAINT chk_approval_requests_target,
    DROP KEY idx_approval_requests_mailbox_status,
    DROP COLUMN mailbox_email;

CREATE TABLE IF NOT EXISTS approval_notifications (
    id                  CHAR(36)     NOT NULL,
    approval_request_id CHAR(36)     NOT NULL,
    recipient_email     VARCHAR(320) NOT NULL,
    sent                TINYINT(1)   NOT NULL DEFAULT 0,
    attempts            INT          NOT NULL DEFAULT 0,
    last_error          TEXT         NULL DEFAULT NULL,
    created_at          DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at          DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_approval_notifications (approval_request_id, recipient_email),
    KEY idx_approval_notifications_unsent (sent, attempts),
    CONSTRAINT fk_approval_notifications_request
        FOREIGN KEY (approval_request_id) REFERENCES approval_requests (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
