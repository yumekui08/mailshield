-- 005: provisioned_by 列の補完（欠落マイグレーションの修復）
--
-- users / mailboxes / mailbox_assignments の provisioned_by 列は当初
-- 個別マイグレーションとして存在したが、001_schema.sql への統合時に
-- マイグレーションファイルが削除されたため、それ以前に作成された既存 DB には
-- 列が存在しない（api-server のユーザー検索が失敗しログインできなくなる）。
--
-- IF NOT EXISTS 付きのため、001 で作成済みの新しい環境に流しても no-op で安全。
--
-- 既存環境への適用: mysql -u root -p mailshield < 005_add_provisioned_by.sql

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS provisioned_by ENUM('manual','oidc','ldap','scim') NOT NULL DEFAULT 'manual' AFTER approver_id;

ALTER TABLE mailboxes
    ADD COLUMN IF NOT EXISTS provisioned_by ENUM('manual','ldap','scim') NOT NULL DEFAULT 'manual' AFTER is_active;

ALTER TABLE mailbox_assignments
    ADD COLUMN IF NOT EXISTS provisioned_by ENUM('manual','ldap','scim') NOT NULL DEFAULT 'manual' AFTER role;
