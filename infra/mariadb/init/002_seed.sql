-- MailShield OSS - 開発用シードデータ
-- ⚠️ このファイルは開発・評価用途専用です。本番環境では使用しないでください。

-- 開発用管理者ユーザー
-- パスワード: "password"（bcrypt ハッシュ。本番環境では必ず変更すること）
INSERT INTO users (id, email, display_name, password_hash, role, is_active)
VALUES
    ('00000000-0000-0000-0000-000000000002',
     'admin@internal.test',
     'Admin User',
     '$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi',
     'admin',
     1)
ON DUPLICATE KEY UPDATE display_name = VALUES(display_name), is_active = VALUES(is_active);
