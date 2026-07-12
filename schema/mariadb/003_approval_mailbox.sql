-- 003: 承認フローのメールボックス承認者対応
--
-- approval_requests をメールボックス単位の承認に対応させる。
--   approver_id   : ユーザー個人を承認者に指定（従来方式・users.approver_id 経由）
--   mailbox_email : メールボックスを指定。そのメールボックスに role=admin で
--                   割り当てられたユーザー全員が承認・却下できる（先に決定した人が有効）
-- 必ずどちらか一方が入る（CHECK 制約）。
--
-- 既存環境への適用: mysql -u root -p mailshield < 003_approval_mailbox.sql

ALTER TABLE approval_requests
    MODIFY COLUMN approver_id CHAR(36) NULL DEFAULT NULL,
    ADD COLUMN mailbox_email VARCHAR(320) NULL DEFAULT NULL AFTER approver_id,
    ADD KEY idx_approval_requests_mailbox_status (mailbox_email, status),
    ADD CONSTRAINT chk_approval_requests_target CHECK (approver_id IS NOT NULL OR mailbox_email IS NOT NULL);
