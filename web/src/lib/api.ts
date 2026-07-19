import type {
  User,
  UserRecord,
  MailboxRecord,
  AssignmentRecord,
  AssignmentRole,
  PagedResult,
  Message,
  MessageDetail,
  Stats,
  StatsTimeseriesPoint,
  AuditLog,
  AuditLogParams,
  APIKey,
  CreateAPIKeyRequest,
  CreateAPIKeyResponse,
  ApprovalListItem,
  ApprovalStatus,
  ApprovalRequestDetail,
  PageMeta,
  DelayedRelease,
  PolicyRoute,
  PolicyRoutesResponse,
  PolicyDocument,
  PolicyHits,
  WorkerInstance,
  ConfigVariable,
  Routing,
} from "../types";

const BASE = "/api/v1";

async function request<T>(
  path: string,
  options?: RequestInit
): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    ...options,
  });

  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText);
    throw new ApiError(res.status, text);
  }

  if (res.status === 204) {
    return undefined as T;
  }

  return res.json() as Promise<T>;
}

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    message: string
  ) {
    super(message);
    this.name = "ApiError";
  }
}

// ─── 認証 ──────────────────────────────────────────────────

export type Provider = { id: string; name: string };

export type ProvidersResponse = {
  providers: Provider[];
  setup_required: boolean;
};

export async function getProviders(): Promise<ProvidersResponse> {
  return request<ProvidersResponse>("/auth/providers");
}

export async function loginStandalone(
  email: string,
  password: string
): Promise<{ message: string }> {
  return request<{ message: string }>("/auth/login", {
    method: "POST",
    body: JSON.stringify({ email, password }),
  });
}

export function getOIDCLoginURL(redirectTo: string): string {
  return `${BASE}/auth/login/oidc?redirect_to=${encodeURIComponent(redirectTo)}`;
}

export async function getMe(): Promise<User> {
  return request<User>("/auth/me");
}

export async function logout(): Promise<{ message: string }> {
  return request<{ message: string }>("/auth/logout", { method: "POST" });
}

export async function setup(
  email: string,
  password: string,
  displayName?: string
): Promise<{ message: string; email: string }> {
  return request<{ message: string; email: string }>("/auth/setup", {
    method: "POST",
    body: JSON.stringify({ email, password, display_name: displayName ?? "" }),
  });
}

export async function forgotPassword(email: string): Promise<{ message: string }> {
  return request<{ message: string }>("/auth/forgot-password", {
    method: "POST",
    body: JSON.stringify({ email }),
  });
}

export async function resetPassword(
  token: string,
  password: string,
): Promise<{ message: string }> {
  return request<{ message: string }>("/auth/reset-password", {
    method: "POST",
    body: JSON.stringify({ token, password }),
  });
}

// ─── ユーザー管理（admin のみ） ────────────────────────────

export async function listUsers(): Promise<{ data: UserRecord[]; meta: { total: number } }> {
  return request("/users");
}

export async function createUser(params: {
  email: string;
  password: string;
  display_name?: string;
  role: string;
}): Promise<UserRecord> {
  return request("/users", {
    method: "POST",
    body: JSON.stringify(params),
  });
}

export async function updateUser(
  id: string,
  params: { role?: string; password?: string; display_name?: string }
): Promise<UserRecord> {
  return request(`/users/${id}`, {
    method: "PATCH",
    body: JSON.stringify(params),
  });
}

export async function deleteUser(id: string): Promise<void> {
  await request(`/users/${id}`, { method: "DELETE" });
}

// ─── メールボックス管理（operator/admin のみ） ─────────────

export interface MailboxFilter {
  q?: string;
  assigned_user_id?: string;
  provisioned_by?: "manual" | "ldap" | "scim";
  active?: boolean;
  missing_role?: AssignmentRole;
  limit?: number;
  offset?: number;
}

export async function listMailboxes(
  filter: MailboxFilter = {}
): Promise<{ data: MailboxRecord[]; meta: { total: number; limit: number; offset: number } }> {
  const params = new URLSearchParams();
  if (filter.q) params.set("q", filter.q);
  if (filter.assigned_user_id) params.set("assigned_user_id", filter.assigned_user_id);
  if (filter.provisioned_by) params.set("provisioned_by", filter.provisioned_by);
  if (filter.active !== undefined) params.set("active", String(filter.active));
  if (filter.missing_role) params.set("missing_role", filter.missing_role);
  if (filter.limit !== undefined) params.set("limit", String(filter.limit));
  if (filter.offset !== undefined) params.set("offset", String(filter.offset));
  const qs = params.toString();
  return request(`/mailboxes${qs ? `?${qs}` : ""}`);
}

export async function createMailbox(params: {
  email_address: string;
  display_name?: string;
}): Promise<MailboxRecord> {
  return request("/mailboxes", { method: "POST", body: JSON.stringify(params) });
}

export async function updateMailbox(
  id: string,
  params: { display_name?: string; is_active?: boolean }
): Promise<MailboxRecord> {
  return request(`/mailboxes/${id}`, { method: "PATCH", body: JSON.stringify(params) });
}

export async function deleteMailbox(id: string): Promise<void> {
  await request(`/mailboxes/${id}`, { method: "DELETE" });
}

export async function listAssignments(
  mailboxId: string
): Promise<{ data: AssignmentRecord[]; meta: { total: number } }> {
  return request(`/mailboxes/${mailboxId}/assignments`);
}

export async function addAssignment(
  mailboxId: string,
  params: { user_id: string; role: AssignmentRole }
): Promise<AssignmentRecord> {
  return request(`/mailboxes/${mailboxId}/assignments`, {
    method: "POST",
    body: JSON.stringify(params),
  });
}

export async function removeAssignment(
  mailboxId: string,
  params: { user_id: string; role: AssignmentRole }
): Promise<void> {
  await request(`/mailboxes/${mailboxId}/assignments`, {
    method: "DELETE",
    body: JSON.stringify(params),
  });
}

// ─── 統計 ──────────────────────────────────────────────────

export async function getStats(): Promise<Stats> {
  return request<Stats>("/stats");
}

export async function getStatsTimeseries(
  days: number
): Promise<{ data: StatsTimeseriesPoint[] }> {
  return request<{ data: StatsTimeseriesPoint[] }>(
    `/stats/timeseries?days=${days}`
  );
}

// ─── 隔離メール ────────────────────────────────────────────

export interface QuarantineListParams {
  page?: number;
  per_page?: number;
  from?: string;
  subject?: string;
  has_attachment?: boolean | "";
}

export async function getQuarantineList(
  params: QuarantineListParams
): Promise<PagedResult<Message>> {
  const qs = new URLSearchParams();
  if (params.page) qs.set("page", String(params.page));
  if (params.per_page) qs.set("per_page", String(params.per_page));
  if (params.from) qs.set("from", params.from);
  if (params.subject) qs.set("subject", params.subject);
  if (params.has_attachment !== "" && params.has_attachment !== undefined) {
    qs.set("has_attachment", String(params.has_attachment));
  }
  qs.set("sort", "received_at");
  qs.set("order", "desc");
  return request<PagedResult<Message>>(`/quarantine?${qs.toString()}`);
}

export async function getQuarantineDetail(id: string): Promise<MessageDetail> {
  return request<MessageDetail>(`/quarantine/${id}`);
}

export async function releaseQuarantine(
  id: string
): Promise<{ message: string; id: string; status: string }> {
  return request(`/quarantine/${id}/release`, { method: "POST" });
}

export async function deleteQuarantine(
  id: string
): Promise<{ message: string; id: string; status: string }> {
  return request(`/quarantine/${id}`, { method: "DELETE" });
}

export interface BulkResult {
  succeeded: string[];
  failed: { id: string; reason: string }[];
}

export async function bulkReleaseQuarantine(ids: string[]): Promise<BulkResult> {
  return request<BulkResult>("/quarantine/bulk-release", {
    method: "POST",
    body: JSON.stringify({ ids }),
  });
}

export async function bulkDeleteQuarantine(ids: string[]): Promise<BulkResult> {
  return request<BulkResult>("/quarantine/bulk", {
    method: "DELETE",
    body: JSON.stringify({ ids }),
  });
}

// ─── メール処理ログ ────────────────────────────────────────

export interface MessageListParams extends QuarantineListParams {
  status?: string;
}

export async function getMessageList(
  params: MessageListParams
): Promise<PagedResult<Message>> {
  const qs = new URLSearchParams();
  if (params.page) qs.set("page", String(params.page));
  if (params.per_page) qs.set("per_page", String(params.per_page));
  if (params.from) qs.set("from", params.from);
  if (params.subject) qs.set("subject", params.subject);
  if (params.status) qs.set("status", params.status);
  if (params.has_attachment !== "" && params.has_attachment !== undefined) {
    qs.set("has_attachment", String(params.has_attachment));
  }
  qs.set("sort", "received_at");
  qs.set("order", "desc");
  return request<PagedResult<Message>>(`/messages?${qs.toString()}`);
}

export async function getMessageDetail(id: string): Promise<MessageDetail> {
  return request<MessageDetail>(`/messages/${id}`);
}

export async function getMessageEMLURL(id: string): Promise<{ url: string; expires_in: number }> {
  return request<{ url: string; expires_in: number }>(`/messages/${id}/eml`);
}

// ─── 添付ファイル ──────────────────────────────────────────

export type DownloadMode = "simple" | "otp" | "auth";

export interface Attachment {
  id: string;
  message_id: string;
  download_token: string;
  filename: string;
  content_type: string;
  size_bytes: number;
  storage_backend: "s3" | "spo";
  is_disabled: boolean;
  download_mode: DownloadMode;
  created_at: string;
}

export interface PublicAttachmentsResponse {
  mode: DownloadMode;
  attachments: Attachment[] | null;
}

export async function getAttachmentsByMessage(messageId: string): Promise<Attachment[]> {
  return request<Attachment[]>(`/messages/${messageId}/attachments`);
}

export async function getAttachments(downloadToken: string): Promise<Attachment[]> {
  return request<Attachment[]>(`/attachments/${downloadToken}`);
}

export function getAttachmentDownloadURL(downloadToken: string, filename: string): string {
  return `${BASE}/attachments/${downloadToken}/${encodeURIComponent(filename)}`;
}

export async function getPublicAttachmentsInfo(downloadToken: string): Promise<PublicAttachmentsResponse> {
  return request<PublicAttachmentsResponse>(`/public/attachments/${downloadToken}`);
}

export async function getPublicAttachmentsInfoByOTP(
  downloadToken: string,
  sessionId: string,
): Promise<PublicAttachmentsResponse> {
  return request<PublicAttachmentsResponse>(
    `/public/attachments/${downloadToken}?otp_session=${encodeURIComponent(sessionId)}`,
  );
}

export function getPublicAttachmentDownloadURL(downloadToken: string, filename: string): string {
  return `${BASE}/public/attachments/${downloadToken}/${encodeURIComponent(filename)}`;
}

export function getOTPAttachmentDownloadURL(
  downloadToken: string,
  filename: string,
  sessionId: string,
): string {
  return `${BASE}/public/attachments/${downloadToken}/${encodeURIComponent(filename)}?otp_session=${encodeURIComponent(sessionId)}`;
}

export async function requestOTP(
  downloadToken: string,
  email: string,
): Promise<{ message: string }> {
  return request<{ message: string }>(`/public/attachments/${downloadToken}/otp/request`, {
    method: "POST",
    body: JSON.stringify({ email }),
  });
}

export async function verifyOTP(
  downloadToken: string,
  email: string,
  code: string,
): Promise<{ session_id: string }> {
  return request<{ session_id: string }>(`/public/attachments/${downloadToken}/otp/verify`, {
    method: "POST",
    body: JSON.stringify({ email, code }),
  });
}

export async function disableAttachment(id: string, disabled: boolean): Promise<void> {
  await request(`/attachments/${id}/disable`, {
    method: "PATCH",
    body: JSON.stringify({ disabled }),
  });
}

export async function deleteAttachment(id: string): Promise<void> {
  await request(`/attachments/${id}`, { method: "DELETE" });
}

// ─── 監査ログ ──────────────────────────────────────────────────

export async function getAuditLogs(
  params: AuditLogParams = {},
): Promise<PagedResult<AuditLog>> {
  const q = new URLSearchParams();
  if (params.page) q.set("page", String(params.page));
  if (params.per_page) q.set("per_page", String(params.per_page));
  if (params.event_type) q.set("event_type", params.event_type);
  if (params.actor_id) q.set("actor_id", params.actor_id);
  if (params.from_date) q.set("from_date", params.from_date);
  if (params.to_date) q.set("to_date", params.to_date);
  const qs = q.toString();
  return request<PagedResult<AuditLog>>(`/audit-logs${qs ? `?${qs}` : ""}`);
}


// ─── API キー ──────────────────────────────────────────────────

export async function listAPIKeys(): Promise<{ data: APIKey[]; meta: { total: number } }> {
  return request<{ data: APIKey[]; meta: { total: number } }>("/api-keys");
}

export async function createAPIKey(req: CreateAPIKeyRequest): Promise<CreateAPIKeyResponse> {
  return request<CreateAPIKeyResponse>("/api-keys", {
    method: "POST",
    body: JSON.stringify(req),
  });
}

export async function revokeAPIKey(id: string): Promise<void> {
  await request(`/api-keys/${id}`, { method: "DELETE" });
}


// ─── シミュレーション ──────────────────────────────────────────

export interface SimulateInspectResult {
  worker: string;
  detected: boolean;
  score: number;
  details: Record<string, unknown>;
}

export interface SimulateResult {
  route_name: string;
  direction: string;
  inspect_results: SimulateInspectResult[];
  original_subject: string;
  transformed_subject: string;
  subject_changed: boolean;
  action: string;
  matched_rule: string;
  processing_ms: number;
}

export async function simulatePolicy(eml: string): Promise<SimulateResult> {
  const res = await fetch(`${BASE}/simulate/`, {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "message/rfc822" },
    body: eml,
  });
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    throw new Error(text || `simulate failed: ${res.status}`);
  }
  return res.json();
}

// ─── 承認フロー ─────────────────────────────────────────────

export interface ApprovalFilter {
  q?: string;
  status?: ApprovalStatus[];
  page?: number;
  per_page?: number;
}

export async function listApprovals(
  filter: ApprovalFilter = {}
): Promise<{ items: ApprovalListItem[]; meta: PageMeta }> {
  const params = new URLSearchParams();
  if (filter.q) params.set("q", filter.q);
  if (filter.status && filter.status.length > 0) params.set("status", filter.status.join(","));
  if (filter.page !== undefined) params.set("page", String(filter.page));
  if (filter.per_page !== undefined) params.set("per_page", String(filter.per_page));
  const qs = params.toString();
  return request(`/approvals${qs ? `?${qs}` : ""}`);
}

export async function getApproval(id: string): Promise<ApprovalRequestDetail> {
  return request<ApprovalRequestDetail>(`/approvals/${id}`);
}

export async function bulkApprove(ids: string[], comment?: string): Promise<BulkResult> {
  return request<BulkResult>("/approvals/bulk-approve", {
    method: "POST",
    body: JSON.stringify({ ids, comment: comment ?? "" }),
  });
}

export async function bulkReject(ids: string[], comment?: string): Promise<BulkResult> {
  return request<BulkResult>("/approvals/bulk-reject", {
    method: "POST",
    body: JSON.stringify({ ids, comment: comment ?? "" }),
  });
}

export async function approveRequest(id: string, comment?: string): Promise<{ status: string }> {
  return request<{ status: string }>(`/approvals/${id}/approve`, {
    method: "POST",
    body: JSON.stringify({ comment: comment ?? "" }),
  });
}

export async function rejectRequest(id: string, comment?: string): Promise<{ status: string }> {
  return request<{ status: string }>(`/approvals/${id}/reject`, {
    method: "POST",
    body: JSON.stringify({ comment: comment ?? "" }),
  });
}

// ─── 送信ディレイ（送信待ち） ───────────────────────────────

export async function listDelayed(): Promise<{ items: DelayedRelease[] }> {
  return request<{ items: DelayedRelease[] }>("/delayed");
}

export async function cancelDelayed(id: string): Promise<{ status: string }> {
  return request<{ status: string }>(`/delayed/${id}/cancel`, { method: "POST" });
}

export async function sendDelayedNow(id: string): Promise<{ status: string }> {
  return request<{ status: string }>(`/delayed/${id}/send-now`, { method: "POST" });
}

// ─── ポリシー編集（P2） ──────────────────────────────────────

export async function getPolicyRoutes(): Promise<PolicyRoutesResponse> {
  return request<PolicyRoutesResponse>("/policy/routes");
}

export async function getPolicyRoute(dir: string): Promise<PolicyRoute> {
  return request<PolicyRoute>(`/policy/routes/${encodeURIComponent(dir)}`);
}

export async function updatePolicyRoute(
  dir: string,
  doc: PolicyDocument
): Promise<{ status: string; rules: number }> {
  return request<{ status: string; rules: number }>(
    `/policy/routes/${encodeURIComponent(dir)}`,
    { method: "PUT", body: JSON.stringify(doc) }
  );
}

export async function getPolicyStats(): Promise<{ hits: PolicyHits }> {
  return request<{ hits: PolicyHits }>("/policy/stats");
}

export async function getPolicyVersions(
  dir: string
): Promise<{ versions: import("../types").PolicyVersion[] }> {
  return request<{ versions: import("../types").PolicyVersion[] }>(
    `/policy/routes/${encodeURIComponent(dir)}/versions`
  );
}

export async function rollbackPolicy(
  dir: string,
  versionId: string
): Promise<{ status: string }> {
  return request<{ status: string }>(
    `/policy/routes/${encodeURIComponent(dir)}/rollback`,
    { method: "POST", body: JSON.stringify({ version_id: versionId }) }
  );
}

// UserPicker 用の軽量ユーザー検索（operator 以上）
export async function searchUsers(
  q: string,
  limit = 50
): Promise<{ data: UserRecord[]; meta: { total: number } }> {
  const params = new URLSearchParams();
  if (q) params.set("q", q);
  params.set("limit", String(limit));
  return request<{ data: UserRecord[]; meta: { total: number } }>(
    `/users/search?${params.toString()}`
  );
}

// ─── 設定エンティティ（ADR 008・operator/admin） ───────────────

export async function listWorkerInstances(): Promise<{ data: WorkerInstance[]; meta: { total: number } }> {
  return request("/config/worker-instances");
}

export async function createWorkerInstance(
  params: Omit<WorkerInstance, "id" | "created_at" | "updated_at">
): Promise<WorkerInstance> {
  return request("/config/worker-instances", { method: "POST", body: JSON.stringify(params) });
}

export async function updateWorkerInstance(
  id: string,
  params: Omit<WorkerInstance, "id" | "created_at" | "updated_at">
): Promise<WorkerInstance> {
  return request(`/config/worker-instances/${id}`, { method: "PUT", body: JSON.stringify(params) });
}

export async function deleteWorkerInstance(id: string): Promise<void> {
  await request(`/config/worker-instances/${id}`, { method: "DELETE" });
}

export async function listConfigVariables(): Promise<{ data: ConfigVariable[]; meta: { total: number } }> {
  return request("/config/variables");
}

export async function createConfigVariable(
  params: { key: string; value: string; description: string }
): Promise<ConfigVariable> {
  return request("/config/variables", { method: "POST", body: JSON.stringify(params) });
}

export async function updateConfigVariable(
  id: string,
  params: { key: string; value: string; description: string }
): Promise<ConfigVariable> {
  return request(`/config/variables/${id}`, { method: "PUT", body: JSON.stringify(params) });
}

export async function deleteConfigVariable(id: string): Promise<void> {
  await request(`/config/variables/${id}`, { method: "DELETE" });
}

// ─── ルーティング（ADR 008・operator/admin） ───────────────────

type RoutingInput = Omit<Routing, "id" | "is_catchall" | "created_at" | "updated_at">;

export async function listRoutings(): Promise<{ data: Routing[]; meta: { total: number } }> {
  return request("/config/routings");
}

export async function createRouting(params: RoutingInput): Promise<Routing> {
  return request("/config/routings", { method: "POST", body: JSON.stringify(params) });
}

export async function updateRouting(id: string, params: RoutingInput): Promise<Routing> {
  return request(`/config/routings/${id}`, { method: "PUT", body: JSON.stringify(params) });
}

export async function deleteRouting(id: string): Promise<void> {
  await request(`/config/routings/${id}`, { method: "DELETE" });
}
