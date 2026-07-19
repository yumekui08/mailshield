package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/middleware"
)

// mockConfigRepo は ConfigRepository だけを実装するテスト用モック。
type mockConfigRepo struct {
	created     *domain.WorkerInstance
	createdV    *domain.ConfigVariable
	getInst     *domain.WorkerInstance
	getVar      *domain.ConfigVariable
	instList    []domain.WorkerInstance
	varList     []domain.ConfigVariable
	createdRt   *domain.Routing
	getRt       *domain.Routing
	rtList      []domain.Routing
	catchAllCnt int
	createdRts  []domain.Routing
}

func (m *mockConfigRepo) ListWorkerInstances(context.Context) ([]domain.WorkerInstance, error) {
	return m.instList, nil
}
func (m *mockConfigRepo) GetWorkerInstance(_ context.Context, _ string) (*domain.WorkerInstance, error) {
	return m.getInst, nil
}
func (m *mockConfigRepo) CreateWorkerInstance(_ context.Context, w *domain.WorkerInstance) error {
	w.ID = "inst-1"
	m.created = w
	return nil
}
func (m *mockConfigRepo) UpdateWorkerInstance(_ context.Context, w *domain.WorkerInstance) error {
	m.created = w
	return nil
}
func (m *mockConfigRepo) DeleteWorkerInstance(context.Context, string) error { return nil }
func (m *mockConfigRepo) ListConfigVariables(context.Context) ([]domain.ConfigVariable, error) {
	return m.varList, nil
}
func (m *mockConfigRepo) GetConfigVariable(context.Context, string) (*domain.ConfigVariable, error) {
	return m.getVar, nil
}
func (m *mockConfigRepo) CreateConfigVariable(_ context.Context, v *domain.ConfigVariable) error {
	v.ID = "var-1"
	m.createdV = v
	return nil
}
func (m *mockConfigRepo) UpdateConfigVariable(_ context.Context, v *domain.ConfigVariable) error {
	m.createdV = v
	return nil
}
func (m *mockConfigRepo) DeleteConfigVariable(context.Context, string) error { return nil }
func (m *mockConfigRepo) ListRoutings(context.Context) ([]domain.Routing, error) {
	return m.rtList, nil
}
func (m *mockConfigRepo) GetRouting(context.Context, string) (*domain.Routing, error) {
	return m.getRt, nil
}
func (m *mockConfigRepo) CreateRouting(_ context.Context, rt *domain.Routing) error {
	rt.ID = "rt-1"
	m.createdRt = rt
	m.createdRts = append(m.createdRts, *rt)
	if rt.IsCatchAll {
		m.catchAllCnt++
	}
	return nil
}
func (m *mockConfigRepo) UpdateRouting(_ context.Context, rt *domain.Routing) error {
	m.createdRt = rt
	return nil
}
func (m *mockConfigRepo) DeleteRouting(context.Context, string) error { return nil }
func (m *mockConfigRepo) CountCatchAllRoutings(context.Context) (int, error) {
	return m.catchAllCnt, nil
}

func postJSON(t *testing.T, target string, body any) *http.Request {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, target, bytes.NewReader(b))
	return req.WithContext(middleware.WithSession(req.Context(), adminSession()))
}

func TestCreateWorkerInstance_Valid(t *testing.T) {
	repo := &mockConfigRepo{}
	h := NewConfigHandler(repo, testAuditLogger)
	req := postJSON(t, "/api/v1/config/worker-instances", map[string]any{
		"alias":        "av_internal",
		"display_name": "内部向けウイルス検査",
		"worker_type":  "av-worker",
		"kind":         "inspect",
		"config":       map[string]any{"threshold": 50},
	})
	rr := httptest.NewRecorder()
	h.HandleCreateWorkerInstance(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if repo.created == nil || repo.created.Alias != "av_internal" || repo.created.Kind != domain.WorkerKindInspect {
		t.Errorf("保存内容が不正: %+v", repo.created)
	}
}

func TestCreateWorkerInstance_InvalidAlias(t *testing.T) {
	h := NewConfigHandler(&mockConfigRepo{}, testAuditLogger)
	for _, bad := range []string{"AV-Internal", "1av", "内部", "av internal", ""} {
		req := postJSON(t, "/api/v1/config/worker-instances", map[string]any{
			"alias": bad, "worker_type": "av-worker", "kind": "inspect",
		})
		rr := httptest.NewRecorder()
		h.HandleCreateWorkerInstance(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("alias=%q は 400 になるべき、実際: %d", bad, rr.Code)
		}
	}
}

func TestCreateWorkerInstance_InvalidKind(t *testing.T) {
	h := NewConfigHandler(&mockConfigRepo{}, testAuditLogger)
	req := postJSON(t, "/api/v1/config/worker-instances", map[string]any{
		"alias": "x", "worker_type": "av-worker", "kind": "both",
	})
	rr := httptest.NewRecorder()
	h.HandleCreateWorkerInstance(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("不正な kind は 400、実際: %d", rr.Code)
	}
}

func TestCreateConfigVariable_Valid(t *testing.T) {
	repo := &mockConfigRepo{}
	h := NewConfigHandler(repo, testAuditLogger)
	req := postJSON(t, "/api/v1/config/variables", map[string]any{
		"key": "INTERNAL_DOMAIN", "value": "@example.com", "description": "受信/送信判定用ドメイン",
	})
	rr := httptest.NewRecorder()
	h.HandleCreateConfigVariable(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if repo.createdV == nil || repo.createdV.Key != "INTERNAL_DOMAIN" {
		t.Errorf("保存内容が不正: %+v", repo.createdV)
	}
}

func TestCreateConfigVariable_InvalidKey(t *testing.T) {
	h := NewConfigHandler(&mockConfigRepo{}, testAuditLogger)
	for _, bad := range []string{"9DOMAIN", "my-var", "ドメイン", ""} {
		req := postJSON(t, "/api/v1/config/variables", map[string]any{"key": bad})
		rr := httptest.NewRecorder()
		h.HandleCreateConfigVariable(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("key=%q は 400、実際: %d", bad, rr.Code)
		}
	}
}

func TestListRoutings_AutoCreatesCatchAll(t *testing.T) {
	repo := &mockConfigRepo{catchAllCnt: 0}
	h := NewConfigHandler(repo, testAuditLogger)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/routings", nil)
	req = req.WithContext(middleware.WithSession(req.Context(), adminSession()))
	rr := httptest.NewRecorder()
	h.HandleListRoutings(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if len(repo.createdRts) != 1 || !repo.createdRts[0].IsCatchAll || repo.createdRts[0].MatchExpr != "true" {
		t.Errorf("catch-all が自動作成されていない: %+v", repo.createdRts)
	}
}

func TestDeleteRouting_CatchAllProtected(t *testing.T) {
	repo := &mockConfigRepo{getRt: &domain.Routing{ID: "rt-c", IsCatchAll: true}}
	h := NewConfigHandler(repo, testAuditLogger)
	req := requestWithSessionAndURLParam(http.MethodDelete, "/api/v1/config/routings/rt-c", "id", "rt-c", adminSession())
	rr := httptest.NewRecorder()
	h.HandleDeleteRouting(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("catch-all 削除は 400、実際: %d", rr.Code)
	}
}

func TestCreateRouting_RequiresMatchExpr(t *testing.T) {
	h := NewConfigHandler(&mockConfigRepo{}, testAuditLogger)
	req := postJSON(t, "/api/v1/config/routings", map[string]any{"name": "x", "match_expr": ""})
	rr := httptest.NewRecorder()
	h.HandleCreateRouting(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("match_expr 空は 400、実際: %d", rr.Code)
	}
}

func TestCreateRouting_ValidatesBindingAlias(t *testing.T) {
	h := NewConfigHandler(&mockConfigRepo{}, testAuditLogger)
	req := postJSON(t, "/api/v1/config/routings", map[string]any{
		"name": "inbound", "match_expr": "true",
		"inspect": []map[string]any{{"alias": "BAD-Alias", "enabled": true}},
	})
	rr := httptest.NewRecorder()
	h.HandleCreateRouting(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("不正な binding alias は 400、実際: %d", rr.Code)
	}
}

func TestCreateRouting_Valid(t *testing.T) {
	repo := &mockConfigRepo{}
	h := NewConfigHandler(repo, testAuditLogger)
	req := postJSON(t, "/api/v1/config/routings", map[string]any{
		"name": "inbound", "priority": 20, "match_expr": "mail.to endswith ${INTERNAL_DOMAIN}",
		"policy_ref": "標準受信",
		"inspect":    []map[string]any{{"alias": "av_internal", "enabled": true}},
		"transform":  []map[string]any{{"alias": "fs_internal", "enabled": true}},
	})
	rr := httptest.NewRecorder()
	h.HandleCreateRouting(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if repo.createdRt == nil || repo.createdRt.IsCatchAll {
		t.Errorf("通常ルーティングとして作成されるべき: %+v", repo.createdRt)
	}
	if len(repo.createdRt.Inspect) != 1 || repo.createdRt.Inspect[0].Alias != "av_internal" {
		t.Errorf("inspect 束ねが不正: %+v", repo.createdRt.Inspect)
	}
}
