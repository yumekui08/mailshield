package domain

import "time"

// PolicyVersion はポリシー（policy.yaml）変更前のスナップショット。
// UI からの更新の直前内容を保存し、ロールバックに使う。
type PolicyVersion struct {
	ID         string    `json:"id"`
	RouteDir   string    `json:"route_dir"`
	Content    string    `json:"-"` // policy.yaml 全文（一覧では返さない）
	ActorID    *string   `json:"actor_id,omitempty"`
	ActorEmail *string   `json:"actor_email,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}
