package directory

import "github.com/koizumib/mailshield/services/api-server/internal/domain"

// MailboxAssignmentTuple は「あるユーザーがどのメールボックスに、どの role で
// 所属するか」を表す。解決方式（user_attribute / group_search / fixed）によらず、
// 最終的にはこの形に正規化してから reconcile ロジック
// （repository.SyncMailboxAssignmentsForUser）に渡す。
type MailboxAssignmentTuple struct {
	MailboxEmail string
	// MailboxDisplayName はメールボックスが存在せず新規作成される場合の表示名。
	// 空の場合は MailboxEmail をそのまま使う（Web UI の手動作成と同じ挙動）。
	MailboxDisplayName string
	Role               domain.AssignmentRole
}
