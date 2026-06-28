package domain

import "context"

// EML 本文は含めない。後続サービスは EMLPath で MinIO から取得する。
type MailEvent struct {
	MessageID     string      `json:"message_id"`
	EMLPath       string      `json:"eml_path"`
	ReceivedAt    string      `json:"received_at"` // ISO 8601
	FromAddress   string      `json:"from_address"`
	ToAddresses   []string    `json:"to_addresses"`
	Subject       string      `json:"subject"`
	SizeBytes     int64       `json:"size_bytes"`
	HasAttachment bool        `json:"has_attachment"`
	RspamdScore   float64     `json:"rspamd_score"`
	AuthResults   AuthResults `json:"auth_results"`
}

type EventPublisher interface {
	PublishMailReceived(ctx context.Context, event *MailEvent) error
	Close() error
}
