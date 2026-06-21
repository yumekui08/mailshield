package audit_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/koizumib/mailshield/services/api-server/internal/audit"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
)

// ─── Log nil ガード ─────────────────────────────────────────────

func TestLog_NilLogger(t *testing.T) {
	var l *audit.Logger
	l.Log(domain.AuditLog{EventType: "test"}) // panic しないこと
}

func TestLog_NilWriter(t *testing.T) {
	l := audit.New(nil)
	l.Log(domain.AuditLog{EventType: "test"}) // panic しないこと
}

// ─── StrPtr ─────────────────────────────────────────────────────

func TestStrPtr(t *testing.T) {
	s := "hello"
	p := audit.StrPtr(s)
	if p == nil {
		t.Fatal("StrPtr() returned nil")
	}
	if *p != s {
		t.Errorf("*StrPtr(%q) = %q, want %q", s, *p, s)
	}
}

func TestStrPtr_Empty(t *testing.T) {
	p := audit.StrPtr("")
	if p == nil || *p != "" {
		t.Error("空文字列のポインタが正しく返されるべき")
	}
}

// ─── ClientIP ────────────────────────────────────────────────────

func TestClientIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "192.0.2.10, 10.0.0.1")
	got := audit.ClientIP(req)
	if got != "192.0.2.10" {
		t.Errorf("ClientIP() = %q, want 192.0.2.10", got)
	}
}

func TestClientIP_SingleXForwardedFor(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.5")
	got := audit.ClientIP(req)
	if got != "203.0.113.5" {
		t.Errorf("ClientIP() = %q, want 203.0.113.5", got)
	}
}

func TestClientIP_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.1:12345"
	got := audit.ClientIP(req)
	if got != "192.0.2.1" {
		t.Errorf("ClientIP() = %q, want 192.0.2.1", got)
	}
}

func TestClientIP_XForwardedForTakesPriority(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.2:9999"
	req.Header.Set("X-Forwarded-For", "203.0.113.99")
	got := audit.ClientIP(req)
	if got != "203.0.113.99" {
		t.Errorf("X-Forwarded-For を優先するべき: got %q", got)
	}
}

// ─── Log with writer ─────────────────────────────────────────────

type callbackWriter struct {
	fn func()
}

func (w *callbackWriter) CreateAuditLog(_ context.Context, _ *domain.AuditLog) error {
	w.fn()
	return nil
}

func TestLog_CallsWriter(t *testing.T) {
	done := make(chan struct{}, 1)
	w := &callbackWriter{fn: func() { done <- struct{}{} }}
	l := audit.New(w)
	actorID := "user-1"
	l.Log(domain.AuditLog{EventType: "login", ActorID: &actorID})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("writer が時間内に呼ばれなかった")
	}
}
