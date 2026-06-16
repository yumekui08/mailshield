package domain

import "time"

// AuditLog は操作の監査記録を保持する。
type AuditLog struct {
	ID         string         `json:"id"`
	EventType  string         `json:"event_type"`
	ActorID    *string        `json:"actor_id"`
	ActorEmail *string        `json:"actor_email"`
	TargetType *string        `json:"target_type"`
	TargetID   *string        `json:"target_id"`
	Detail     map[string]any `json:"detail"`
	IPAddress  *string        `json:"ip_address"`
	CreatedAt  time.Time      `json:"created_at"`
}

// AuditLogQuery は監査ログ一覧の検索条件を保持する。
type AuditLogQuery struct {
	Page      int
	PerPage   int
	EventType string // 前方一致（例: "auth." → auth.* 全件）
	ActorID   string
	FromDate  string // YYYY-MM-DD
	ToDate    string // YYYY-MM-DD
}

// 監査イベント種別定数
const (
	EventAuthLoginSuccess         = "auth.login.success"
	EventAuthLoginFailure         = "auth.login.failure"
	EventAuthLogout               = "auth.logout"
	EventAuthPasswordResetRequest = "auth.password_reset.requested"
	EventAuthPasswordResetDone    = "auth.password_reset.completed"

	EventQuarantineReleased     = "quarantine.released"
	EventQuarantineDeleted      = "quarantine.deleted"
	EventQuarantineBulkReleased = "quarantine.bulk_released"
	EventQuarantineBulkDeleted  = "quarantine.bulk_deleted"

	EventUserCreated         = "user.created"
	EventUserDeleted         = "user.deleted"
	EventUserRoleChanged     = "user.role_changed"
	EventUserPasswordChanged = "user.password_changed"

	EventMailboxCreated            = "mailbox.created"
	EventMailboxUpdated            = "mailbox.updated"
	EventMailboxDeleted            = "mailbox.deleted"
	EventMailboxAssignmentAdded    = "mailbox.assignment_added"
	EventMailboxAssignmentRemoved  = "mailbox.assignment_removed"

	EventAPIKeyCreated = "apikey.created"
	EventAPIKeyRevoked = "apikey.revoked"
)
