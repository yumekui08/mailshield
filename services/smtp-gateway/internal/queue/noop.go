package queue

import (
	"context"
	"log/slog"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

type noopPublisher struct{}

// NewNoop は何もしない EventPublisher を返す。
// queue.backend = none のときに使用する。統合イベント通知が不要な単一ノード構成向け。
func NewNoop() domain.EventPublisher {
	return &noopPublisher{}
}

func (p *noopPublisher) PublishMailReceived(_ context.Context, event *domain.MailEvent) error {
	slog.Debug("queue noop: mail.received イベントをスキップ", "message_id", event.MessageID)
	return nil
}

func (p *noopPublisher) Close() error { return nil }
