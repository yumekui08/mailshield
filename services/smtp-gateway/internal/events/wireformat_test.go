package events

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

// TestMailEventWireFormat は mail.received イベントの JSON キーが
// docs/specs/events.md のワイヤーフォーマットと一致することを検証する。
func TestMailEventWireFormat(t *testing.T) {
	event := &domain.MailEvent{
		MessageID:   "id-1",
		EMLPath:     "raw/2026/07/03/id-1.eml",
		AuthResults: domain.DefaultAuthResults(),
	}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)

	for _, key := range []string{
		`"message_id"`, `"eml_path"`, `"received_at"`, `"from_address"`,
		`"to_addresses"`, `"subject"`, `"size_bytes"`, `"has_attachment"`,
		`"rspamd_score"`, `"auth_results"`, `"spf"`, `"dkim"`, `"dmarc"`,
	} {
		if !strings.Contains(got, key) {
			t.Errorf("JSON に %s キーがありません: %s", key, got)
		}
	}
}
