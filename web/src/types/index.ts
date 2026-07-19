export type MessageStatus =
  | "received"
  | "processing"
  | "delivered"
  | "quarantined"
  | "rejected"
  | "approval_pending"
  | "delayed"
  | "expired";

export type DelayedReleaseStatus = "pending" | "released" | "cancelled";

export interface DelayedRelease {
  id: string;
  message_id: string;
  release_at: string;
  status: DelayedReleaseStatus;
  decided_by: string | null;
  decided_at: string | null;
  created_at: string;
  from_address: string;
  to_addresses: string[];
  subject: string;
  size_bytes: number;
  has_attachment: boolean;
}

export type ApprovalStatus = "pending" | "approved" | "rejected" | "expired";

export interface ApprovalRequest {
  id: string;
  message_id: string;
  /** グローバルフォールバック承認者のユーザー ID（メールボックス承認では null） */
  approver_id: string | null;
  /** メールボックス承認の対象アドレス（1..n）。いずれかの admin 割り当てユーザーが承認可 */
  mailbox_emails: string[] | null;
  status: ApprovalStatus;
  comment: string | null;
  notification_sent: boolean;
  result_notified: boolean;
  decided_at: string | null;
  expires_at: string;
  created_at: string;
  updated_at: string;
}

/** 承認一覧の 1 行（依頼 + メール件名・送信元） */
export interface ApprovalListItem extends ApprovalRequest {
  subject: string;
  from_address: string;
}

/** 承認詳細に添付する分離済み添付ファイル（download_token で DL） */
export interface ApprovalAttachment {
  id: string;
  message_id: string;
  download_token: string;
  filename: string;
  content_type: string;
  size_bytes: number;
  is_disabled: boolean;
  created_at: string;
}

export interface ApprovalRequestDetail extends ApprovalRequest {
  /** メール本体のメタデータ（件名・送信元・宛先・サイズ等） */
  message: Message;
  /** EML から抽出したテキスト本文 */
  text_body: string;
  /** EML から抽出した HTML 本文（サンドボックス iframe で描画すること） */
  html_body: string;
  /** 分離済み添付ファイル一覧 */
  attachments: ApprovalAttachment[];
}

export type Role = "admin" | "operator" | "viewer";

// ─── 設定エンティティ（ADR 008） ───────────────────────────────
export type WorkerKind = "inspect" | "transform";

export interface WorkerInstance {
  id: string;
  alias: string;
  display_name: string;
  worker_type: string;
  kind: WorkerKind;
  config: Record<string, unknown>;
  default_timeout_seconds: number;
  is_enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface ConfigVariable {
  id: string;
  key: string;
  value: string;
  description: string;
  created_at: string;
  updated_at: string;
}

export interface WorkerBinding {
  alias: string;
  enabled: boolean;
  timeout_seconds?: number | null;
}

export type RoutingDirection = "inbound" | "outbound" | "internal";

export interface Routing {
  id: string;
  name: string;
  priority: number;
  match_expr: string;
  direction: RoutingDirection;
  is_catchall: boolean;
  is_enabled: boolean;
  policy_ref: string;
  inspect: WorkerBinding[];
  transform: WorkerBinding[];
  created_at: string;
  updated_at: string;
}

export interface Message {
  id: string;
  eml_path: string;
  from_address: string;
  to_addresses: string[];
  subject: string;
  size_bytes: number;
  has_attachment: boolean;
  rspamd_score: number;
  spf_result: string;
  dkim_result: string;
  dmarc_result: string;
  status: MessageStatus;
  received_at: string;
  updated_at: string;
}

export interface InspectResult {
  id: string;
  worker_name: string;
  score: number;
  detected: boolean;
  details: Record<string, unknown>;
  created_at: string;
}

export interface MessageDetail extends Message {
  inspect_results: InspectResult[];
}

export interface PageMeta {
  total: number;
  page: number;
  per_page: number;
  total_pages: number;
}

export interface PagedResult<T> {
  data: T[];
  meta: PageMeta;
}

export interface User {
  sub: string;
  email: string;
  name: string;
  role: Role;
}

// /api/v1/users のレスポンス型（管理用ユーザー情報）
export interface UserRecord {
  id: string;
  email: string;
  display_name: string;
  role: Role;
  is_active: boolean;
}

export type AssignmentRole = "member" | "owner" | "approver";

export interface AssignmentRoleSummary {
  role: AssignmentRole;
  count: number;
  sample: { email: string; display_name: string }[];
}

export interface MailboxRecord {
  id: string;
  email_address: string;
  display_name: string;
  is_active: boolean;
  assignment_summary: AssignmentRoleSummary[] | null;
}

export interface StatsPeriod {
  delivered: number;
  quarantined: number;
  rejected: number;
  total: number;
}

export interface Stats {
  today: StatsPeriod;
  week: StatsPeriod;
}

export interface StatsTimeseriesPoint {
  date: string;
  delivered: number;
  quarantined: number;
  rejected: number;
  total: number;
}

export interface AssignmentRecord {
  id: string;
  mailbox_id: string;
  user_id: string;
  role: AssignmentRole;
  user_email: string;
  user_display_name: string;
}

export interface AuditLog {
  id: string;
  event_type: string;
  actor_id: string | null;
  actor_email: string | null;
  target_type: string | null;
  target_id: string | null;
  detail: Record<string, unknown> | null;
  ip_address: string | null;
  created_at: string;
}

export interface AuditLogParams {
  page?: number;
  per_page?: number;
  event_type?: string;
  actor_id?: string;
  from_date?: string;
  to_date?: string;
}

export interface APIKey {
  id: string;
  name: string;
  role: Role;
  created_by: string | null;
  last_used_at: string | null;
  expires_at: string | null;
  revoked_at: string | null;
  created_at: string;
}

export interface CreateAPIKeyRequest {
  name: string;
  role: Role;
  expires_at?: string;
}

export interface CreateAPIKeyResponse extends APIKey {
  key: string;
}


// ─── ポリシー編集（P2） ──────────────────────────────────────

export interface PolicyActionSpec {
  type: string;
  destination?: string;
  delay_minutes?: number;
  name?: string;
  value?: string;
}

export interface PolicyRule {
  name: string;
  description?: string;
  enabled?: boolean;
  priority?: number;
  tags?: string[];
  condition: string;
  action?: string;
  destination?: string;
  delay_minutes?: number;
  actions?: PolicyActionSpec[];
}

export interface PolicyDocument {
  lists?: Record<string, { values?: string[]; file?: string }>;
  rules: PolicyRule[];
}

export interface PolicyRoute {
  dir: string;
  name: string;
  direction: string;
  policy: PolicyDocument;
}

export type PolicyHits = Record<string, Record<string, number>>;

export interface PolicyRoutesResponse {
  routes: PolicyRoute[];
  hits: PolicyHits;
}

export interface PolicyVersion {
  id: string;
  route_dir: string;
  actor_email?: string | null;
  created_at: string;
}
