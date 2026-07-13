package events_test

import (
	"context"
	"testing"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/events"
)

func TestNoopPublisher_PublishMailReceived(t *testing.T) {
	p := events.NewNoop()
	event := &domain.MailEvent{
		MessageID:   "test-msg-001",
		FromAddress: "sender@example.com",
	}
	if err := p.PublishMailReceived(context.Background(), event); err != nil {
		t.Errorf("PublishMailReceived() error = %v, want nil", err)
	}
}

func TestNoopPublisher_Close(t *testing.T) {
	p := events.NewNoop()
	if err := p.Close(); err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
}
