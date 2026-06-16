-- MailShield OSS - マイグレーション 004
-- processed_eml_path カラムを mail_messages テーブルに追加する
-- 新規セットアップ（001_schema.sql 適用済み）には不要だが、
-- 003 以前を適用済みの既存 DB に対してこのファイルを適用する。

ALTER TABLE mail_messages
    ADD COLUMN IF NOT EXISTS processed_eml_path VARCHAR(1024) NULL DEFAULT NULL
        AFTER status;
