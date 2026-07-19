-- 設定 WebUI 化（ADR 008）③-2b: ルーティングに direction を追加。
-- gateway が mail.Direction を決めるのに使う（inbound/outbound/internal）。
ALTER TABLE routings ADD COLUMN direction ENUM('inbound','outbound','internal')
    NOT NULL DEFAULT 'inbound' AFTER match_expr;
