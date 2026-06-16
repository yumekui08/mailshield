package domain

import "time"

// APIKey は発行済み API キーのメタ情報を保持する。平文キーは DB に保存しない。
type APIKey struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Role       Role       `json:"role"`
	CreatedBy  *string    `json:"created_by"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// IsActive はキーが現在使用可能かどうかを返す。
func (k *APIKey) IsActive() bool {
	if k.RevokedAt != nil {
		return false
	}
	if k.ExpiresAt != nil && time.Now().After(*k.ExpiresAt) {
		return false
	}
	return true
}
