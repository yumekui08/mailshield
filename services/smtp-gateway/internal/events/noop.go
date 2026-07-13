// Package events は mail.received 統合イベントの発行を担う。
// バックエンドは webhook（HTTP POST）または none（発行なし）。
// イベント発行はメールフローに影響しない（失敗してもログのみで処理続行）。
package events

import (
	"context"
	"log/slog"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

type noopPublisher struct{}

// NewNoop は何もしない EventPublisher を返す。
// events.backend = none のときに使用する。統合イベント通知が不要な構成向け。
func NewNoop() domain.EventPublisher {
	return &noopPublisher{}
}

func (p *noopPublisher) PublishMailReceived(_ context.Context, event *domain.MailEvent) error {
	slog.Debug("events noop: mail.received イベントをスキップ", "message_id", event.MessageID)
	return nil
}

func (p *noopPublisher) Close() error { return nil }
