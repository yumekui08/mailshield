package metrics

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRenderCounters(t *testing.T) {
	m := New("test-1.0")
	m.IncReceived("10-inbound")
	m.IncReceived("10-inbound")
	m.IncReceived("20-outbound")
	m.IncUnrouted()
	m.IncAction("10-inbound", "deliver")
	m.IncAction("10-inbound", "quarantine")
	m.IncError("storage_save")
	m.IncDetected("10-inbound", "av-worker")

	out := m.Render()

	wants := []string{
		`mailshield_build_info{version="test-1.0"} 1`,
		`mailshield_mail_received_total{route="10-inbound"} 2`,
		`mailshield_mail_received_total{route="20-outbound"} 1`,
		`mailshield_mail_unrouted_total 1`,
		`mailshield_mail_action_total{route="10-inbound",action="deliver"} 1`,
		`mailshield_mail_action_total{route="10-inbound",action="quarantine"} 1`,
		`mailshield_mail_errors_total{stage="storage_save"} 1`,
		`mailshield_inspect_detected_total{route="10-inbound",worker="av-worker"} 1`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("出力に %q が含まれない:\n%s", w, out)
		}
	}
}

func TestRenderHistogram(t *testing.T) {
	m := New("test")
	m.ObserveProcessing(0.05) // le=0.1 以上すべてに入る
	m.ObserveProcessing(3.0)  // le=5 以上に入る
	m.ObserveProcessing(120)  // +Inf のみ

	out := m.Render()

	wants := []string{
		`mailshield_mail_processing_seconds_bucket{le="0.1"} 1`,
		`mailshield_mail_processing_seconds_bucket{le="2.5"} 1`,
		`mailshield_mail_processing_seconds_bucket{le="5"} 2`,
		`mailshield_mail_processing_seconds_bucket{le="60"} 2`,
		`mailshield_mail_processing_seconds_bucket{le="+Inf"} 3`,
		`mailshield_mail_processing_seconds_count 3`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("出力に %q が含まれない:\n%s", w, out)
		}
	}
	if !strings.Contains(out, "mailshield_mail_processing_seconds_sum 123.05") {
		t.Errorf("sum が期待値でない:\n%s", out)
	}
}

func TestHandler(t *testing.T) {
	m := New("test")
	m.IncReceived("r1")

	srv := httptest.NewServer(m.Handler())
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatalf("GET 失敗: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type が text/plain でない: %s", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `mailshield_mail_received_total{route="r1"} 1`) {
		t.Errorf("レスポンスにカウンターが含まれない:\n%s", body)
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New("test")
	done := make(chan struct{})
	for range 10 {
		go func() {
			for range 100 {
				m.IncReceived("r")
				m.IncAction("r", "deliver")
				m.ObserveProcessing(0.2)
			}
			done <- struct{}{}
		}()
	}
	for range 10 {
		<-done
	}
	out := m.Render()
	if !strings.Contains(out, `mailshield_mail_received_total{route="r"} 1000`) {
		t.Errorf("並行カウントが一致しない:\n%s", out)
	}
}
