-- 005_add_audit_logs.sql
-- 監査ログテーブルを追加する

CREATE TABLE IF NOT EXISTS audit_logs (
  id           VARCHAR(36)   NOT NULL,
  event_type   VARCHAR(64)   NOT NULL,
  actor_id     VARCHAR(36)   NULL,
  actor_email  VARCHAR(255)  NULL,
  target_type  VARCHAR(64)   NULL,
  target_id    VARCHAR(255)  NULL,
  detail       JSON          NULL,
  ip_address   VARCHAR(45)   NULL,
  created_at   DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  INDEX idx_audit_event_type (event_type),
  INDEX idx_audit_actor_id   (actor_id),
  INDEX idx_audit_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
