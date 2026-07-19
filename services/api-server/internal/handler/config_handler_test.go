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
	created         *domain.WorkerInstance
	createdV        *domain.ConfigVariable
	getInst         *domain.WorkerInstance
	getVar          *domain.ConfigVariable
	instList        []domain.WorkerInstance
	varList         []domain.ConfigVariable
	createdRt       *domain.Routing
	getRt           *domain.Routing
	rtList          []domain.Routing
	catchAllCnt     int
	createdRts      []domain.Routing
	savedVersion    *domain.ConfigVersion
	activeVersion   *domain.ConfigVersion
	activeVersionID string
	createdPol      *domain.PolicyInstance
	getPol          *domain.PolicyInstance
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
func (m *mockConfigRepo) ListPolicyInstances(context.Context) ([]domain.PolicyInstance, error) {
	return nil, nil
}
func (m *mockConfigRepo) GetPolicyInstance(context.Context, string) (*domain.PolicyInstance, error) {
	return m.getPol, nil
}
func (m *mockConfigRepo) CreatePolicyInstance(_ context.Context, p *domain.PolicyInstance) error {
	p.ID = "pol-1"
	m.createdPol = p
	return nil
}
func (m *mockConfigRepo) UpdatePolicyInstance(_ context.Context, p *domain.PolicyInstance) error {
	m.createdPol = p
	return nil
}
func (m *mockConfigRepo) DeletePolicyInstance(context.Context, string) error { return nil }
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
func (m *mockConfigRepo) SaveConfigVersion(_ context.Context, v *domain.ConfigVersion) error {
	v.ID = "ver-1"
	m.savedVersion = v
	return nil
}
func (m *mockConfigRepo) GetActiveConfigVersion(context.Context) (*domain.ConfigVersion, error) {
	return m.activeVersion, nil
}
func (m *mockConfigRepo) GetActiveConfigChecksum(context.Context) (string, error) {
	if m.activeVersion != nil {
		return m.activeVersion.Checksum, nil
	}
	return "", nil
}
func (m *mockConfigRepo) SetActiveConfigVersion(_ context.Context, versionID string) error {
	m.activeVersionID = versionID
	return nil
}
func (m *mockConfigRepo) TryAcquireSeedLock(context.Context, string, int) (bool, error) {
	return true, nil
}
func (m *mockConfigRepo) ReleaseSeedLock(context.Context, string) error { return nil }

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

// 空状態を許容する（catch-all を自動生成しない）。
func TestListRoutings_EmptyNoAutoCreate(t *testing.T) {
	repo := &mockConfigRepo{}
	h := NewConfigHandler(repo, testAuditLogger)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/routings", nil)
	req = req.WithContext(middleware.WithSession(req.Context(), adminSession()))
	rr := httptest.NewRecorder()
	h.HandleListRoutings(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if len(repo.createdRts) != 0 {
		t.Errorf("ルーティングを自動作成すべきでない: %+v", repo.createdRts)
	}
}

// どのルーティングも削除できる（catch-all の特別扱いは廃止）。
func TestDeleteRouting_Works(t *testing.T) {
	repo := &mockConfigRepo{getRt: &domain.Routing{ID: "rt-1"}}
	h := NewConfigHandler(repo, testAuditLogger)
	req := requestWithSessionAndURLParam(http.MethodDelete, "/api/v1/config/routings/rt-1", "id", "rt-1", adminSession())
	rr := httptest.NewRecorder()
	h.HandleDeleteRouting(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Errorf("削除は 204、実際: %d", rr.Code)
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
