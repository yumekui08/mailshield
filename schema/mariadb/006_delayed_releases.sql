-- 006: 送信ディレイ（遅延送信）
--
-- policy engine の action: delay に対応する。メールを一定時間保留し、
-- release_at を過ぎたら自動配送する。保留中は送信者が取消・即時送信できる。
--
-- 既存環境への適用: mysql -u root -p mailshield < 006_delayed_releases.sql

ALTER TABLE mail_messages
    MODIFY COLUMN status
        ENUM('received','processing','delivered','quarantined','rejected','approval_pending','delayed','expired')
        NOT NULL DEFAULT 'received';

CREATE TABLE IF NOT EXISTS delayed_releases (
    id           CHAR(36)                                      NOT NULL,
    message_id   CHAR(36)                                      NOT NULL,
    release_at   DATETIME(6)                                   NOT NULL,
    status       ENUM('pending','released','cancelled')        NOT NULL DEFAULT 'pending',
    decided_by   CHAR(36)                                      NULL DEFAULT NULL,
    decided_at   DATETIME(6)                                   NULL DEFAULT NULL,
    created_at   DATETIME(6)                                   NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at   DATETIME(6)                                   NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_delayed_releases_message   (message_id),
    KEY idx_delayed_releases_due       (status, release_at),
    CONSTRAINT fk_delayed_releases_message  FOREIGN KEY (message_id) REFERENCES mail_messages (id),
    CONSTRAINT fk_delayed_releases_user     FOREIGN KEY (decided_by) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
