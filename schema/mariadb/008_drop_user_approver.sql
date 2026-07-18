-- マイグレーション 008: 個人承認者（users.approver_id）の廃止（既存 DB 向け）
-- 承認者はメールボックスの role=admin 割り当てに一本化する。
-- 承認のシステム全体フォールバックは approval.global_approver_email（approval_requests.approver_id）で維持する。

ALTER TABLE users DROP FOREIGN KEY fk_users_approver;
ALTER TABLE users DROP INDEX idx_users_approver;
ALTER TABLE users DROP COLUMN approver_id;
