-- マイグレーション 009: メールボックス割り当てロールの再整理（既存 DB 向け）
-- per-mailbox の admin を廃止し、承認担当を独立ロール approver として新設する。
-- 既存の admin 割り当ては approver + owner の両方に展開する（能力を失わないため）。
--   member=受信担当 / owner=送信担当（送信隔離の解放を含む）/ approver=承認担当

-- 1. enum に approver を追加（admin は一時的に併存させる）
ALTER TABLE mailbox_assignments
  MODIFY role ENUM('member','owner','admin','approver') NOT NULL;

-- 2. 既存 admin 行を approver と owner に展開（UNIQUE (mailbox_id,user_id,role) で重複は無視）
INSERT IGNORE INTO mailbox_assignments (id, mailbox_id, user_id, role, provisioned_by, created_at)
  SELECT UUID(), mailbox_id, user_id, 'approver', provisioned_by, created_at
    FROM mailbox_assignments WHERE role = 'admin';
INSERT IGNORE INTO mailbox_assignments (id, mailbox_id, user_id, role, provisioned_by, created_at)
  SELECT UUID(), mailbox_id, user_id, 'owner', provisioned_by, created_at
    FROM mailbox_assignments WHERE role = 'admin';

-- 3. 旧 admin 行を削除
DELETE FROM mailbox_assignments WHERE role = 'admin';

-- 4. enum から admin を除去
ALTER TABLE mailbox_assignments
  MODIFY role ENUM('member','owner','approver') NOT NULL;
