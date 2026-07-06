package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/koizumib/mailshield/services/api-server/internal/config"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
)

func TestHandleTimeseriesDefaultDays(t *testing.T) {
	var gotDays int
	var gotFilter *domain.MailboxVisibilityFilter
	repo := &mockRepository{
		getStatsTimeseriesFunc: func(_ context.Context, days int, filter *domain.MailboxVisibilityFilter) ([]domain.StatsTimeseriesPoint, error) {
			gotDays = days
			gotFilter = filter
			return []domain.StatsTimeseriesPoint{
				{Date: "2026-07-06", Delivered: 5, Quarantined: 1, Total: 6},
			}, nil
		},
	}
	h := NewStatsHandler(repo, config.MailboxPolicyConfig{})

	req := requestWithSession(http.MethodGet, "/api/v1/stats/timeseries", adminSession())
	rec := httptest.NewRecorder()
	h.HandleTimeseries(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if gotDays != 14 {
		t.Errorf("days = %d, want デフォルト 14", gotDays)
	}
	if gotFilter != nil {
		t.Errorf("admin のフィルターは nil であるべき: %+v", gotFilter)
	}

	var body struct {
		Data []domain.StatsTimeseriesPoint `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("レスポンス JSON 解析失敗: %v", err)
	}
	if len(body.Data) != 1 || body.Data[0].Delivered != 5 {
		t.Errorf("レスポンスが期待値でない: %+v", body.Data)
	}
}

func TestHandleTimeseriesDaysParam(t *testing.T) {
	var gotDays int
	repo := &mockRepository{
		getStatsTimeseriesFunc: func(_ context.Context, days int, _ *domain.MailboxVisibilityFilter) ([]domain.StatsTimeseriesPoint, error) {
			gotDays = days
			return []domain.StatsTimeseriesPoint{}, nil
		},
	}
	h := NewStatsHandler(repo, config.MailboxPolicyConfig{})

	req := requestWithSession(http.MethodGet, "/api/v1/stats/timeseries?days=30", adminSession())
	rec := httptest.NewRecorder()
	h.HandleTimeseries(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if gotDays != 30 {
		t.Errorf("days = %d, want 30", gotDays)
	}
}

func TestHandleTimeseriesInvalidDays(t *testing.T) {
	h := NewStatsHandler(&mockRepository{}, config.MailboxPolicyConfig{})

	for _, raw := range []string{"0", "91", "abc", "-1"} {
		req := requestWithSession(http.MethodGet, "/api/v1/stats/timeseries?days="+raw, adminSession())
		rec := httptest.NewRecorder()
		h.HandleTimeseries(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("days=%s: status = %d, want 400", raw, rec.Code)
		}
	}
}

func TestHandleTimeseriesViewerFilter(t *testing.T) {
	var gotFilter *domain.MailboxVisibilityFilter
	repo := &mockRepository{
		getMailboxAddressesForUserFunc: func(_ context.Context, _ string, _ []domain.AssignmentRole) ([]string, error) {
			return []string{"box@example.com"}, nil
		},
		getStatsTimeseriesFunc: func(_ context.Context, _ int, filter *domain.MailboxVisibilityFilter) ([]domain.StatsTimeseriesPoint, error) {
			gotFilter = filter
			return []domain.StatsTimeseriesPoint{}, nil
		},
	}
	h := NewStatsHandler(repo, config.MailboxPolicyConfig{})

	req := requestWithSession(http.MethodGet, "/api/v1/stats/timeseries", viewerSession("user-viewer"))
	rec := httptest.NewRecorder()
	h.HandleTimeseries(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if gotFilter == nil {
		t.Fatal("viewer にはフィルターが渡されるべき")
	}
	if len(gotFilter.InboundMailboxes) != 1 || gotFilter.InboundMailboxes[0] != "box@example.com" {
		t.Errorf("フィルター内容が期待値でない: %+v", gotFilter)
	}
}
