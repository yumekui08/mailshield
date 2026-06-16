CREATE TABLE IF NOT EXISTS api_keys (
  id           CHAR(36)     NOT NULL,
  name         VARCHAR(128) NOT NULL,
  key_hash     CHAR(64)     NOT NULL,
  role         ENUM('admin','operator','viewer') NOT NULL DEFAULT 'viewer',
  created_by   CHAR(36)     NULL,
  last_used_at DATETIME(3)  NULL,
  expires_at   DATETIME(3)  NULL,
  revoked_at   DATETIME(3)  NULL,
  created_at   DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_api_key_hash (key_hash),
  INDEX idx_api_key_created_by (created_by)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
