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

// MailProcessedEvent はメール処理完了時（ポリシー評価後）に発行するイベント。
// 最終アクションと各検査ワーカーのスコアを含み、SIEM 側での相関分析に使う。
// EML 本文は含めない。
type MailProcessedEvent struct {
	MessageID     string         `json:"message_id"`
	Route         string         `json:"route"`
	Direction     string         `json:"direction"`
	Action        string         `json:"action"` // deliver / reject / quarantine / approval
	FromAddress   string         `json:"from_address"`
	ToAddresses   []string       `json:"to_addresses"`
	Subject       string         `json:"subject"`
	TotalScore    int            `json:"total_score"`
	InspectScores []InspectScore `json:"inspect_scores"`
	ProcessedAt   string         `json:"processed_at"` // ISO 8601
}

// InspectScore は 1 ワーカーの検査結果サマリ（MailProcessedEvent 用）。
type InspectScore struct {
	Worker   string `json:"worker"`
	Score    int    `json:"score"`
	Detected bool   `json:"detected"`
}

type EventPublisher interface {
	PublishMailReceived(ctx context.Context, event *MailEvent) error
	// PublishMailProcessed はメール処理完了イベントを発行する。
	PublishMailProcessed(ctx context.Context, event *MailProcessedEvent) error
	Close() error
}
