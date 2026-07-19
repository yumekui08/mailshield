package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestImportBundle_UpsertsAll(t *testing.T) {
	repo := &mockConfigRepo{}
	h := NewConfigHandler(repo, testAuditLogger)
	bundle := map[string]any{
		"version": "mailshield.config/v1",
		"docs": []map[string]any{
			{"kind": "WorkerInstance", "name": "av_internal", "spec": map[string]any{
				"worker_type": "av-worker", "kind": "inspect", "config": map[string]any{"threshold": 50}, "is_enabled": true}},
			{"kind": "ConfigVariable", "name": "INTERNAL_DOMAIN", "spec": map[string]any{"value": "@x.com"}},
			{"kind": "Routing", "name": "inbound", "spec": map[string]any{
				"priority": 20, "match_expr": "true", "policy_ref": "std"}},
		},
	}
	req := postJSON(t, "/api/v1/config/import", bundle)
	rr := httptest.NewRecorder()
	h.HandleImportBundle(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var res struct {
		Created int      `json:"created"`
		Errors  []string `json:"errors"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&res)
	if res.Created != 3 || len(res.Errors) != 0 {
		t.Errorf("created=%d errors=%v, want created=3 errors=[]", res.Created, res.Errors)
	}
}

func TestImportBundle_RejectsBadAlias(t *testing.T) {
	h := NewConfigHandler(&mockConfigRepo{}, testAuditLogger)
	bundle := map[string]any{"docs": []map[string]any{
		{"kind": "WorkerInstance", "name": "BAD-Alias", "spec": map[string]any{"worker_type": "av-worker", "kind": "inspect"}},
	}}
	req := postJSON(t, "/api/v1/config/import", bundle)
	rr := httptest.NewRecorder()
	h.HandleImportBundle(rr, req)
	var res struct {
		Created int      `json:"created"`
		Errors  []string `json:"errors"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&res)
	if res.Created != 0 || len(res.Errors) == 0 {
		t.Errorf("不正 alias は作成されずエラーになるべき: created=%d errors=%v", res.Created, res.Errors)
	}
}
