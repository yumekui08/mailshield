-- MailShield OSS - users.provisioned_by 追加
-- OIDC ログイン時の JIT プロビジョニングで users 行を作成・更新するようになったため、
-- role・display_name の同期主体（manual/oidc/ldap/scim）を記録する列を追加する。
-- 既存行はすべて手動作成扱い（manual）として扱う。
--
-- 新規インストールでは 001_schema.sql に含まれるため本ファイルは不要（適用しても無害）。
-- 既存環境へのアップグレードでは docs/setup/upgrade.md の手順に従い本ファイルを手動適用すること。

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS provisioned_by ENUM('manual','oidc','ldap','scim') NOT NULL DEFAULT 'manual'
        AFTER approver_id;
