package handler

import (
	"net/http/httptest"
	"testing"
	"time"
)

// TestParseListQuery_Defaults はデフォルト値が正しく設定されることを確認する。
func TestParseListQuery_Defaults(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/messages", nil)

	q, err := parseListQuery(req)
	if err != nil {
		t.Fatalf("デフォルトパラメーターのパース失敗: %v", err)
	}

	if q.Page != 1 {
		t.Errorf("デフォルトpage期待: 1, 実際: %d", q.Page)
	}
	if q.PerPage != 20 {
		t.Errorf("デフォルトper_page期待: 20, 実際: %d", q.PerPage)
	}
	if q.Sort != "received_at" {
		t.Errorf("デフォルトsort期待: received_at, 実際: %s", q.Sort)
	}
	if q.Order != "desc" {
		t.Errorf("デフォルトorder期待: desc, 実際: %s", q.Order)
	}
}

// TestParseListQuery_ValidParams は有効なパラメーターが正しく解析されることを確認する。
func TestParseListQuery_ValidParams(t *testing.T) {
	req := httptest.NewRequest("GET",
		"/api/v1/messages?page=3&per_page=50&status=delivered&from=test@&sort=subject&order=asc&has_attachment=true&since=2024-01-01T00:00:00Z&until=2024-12-31T23:59:59Z",
		nil,
	)

	q, err := parseListQuery(req)
	if err != nil {
		t.Fatalf("有効パラメーターのパース失敗: %v", err)
	}

	if q.Page != 3 {
		t.Errorf("page期待: 3, 実際: %d", q.Page)
	}
	if q.PerPage != 50 {
		t.Errorf("per_page期待: 50, 実際: %d", q.PerPage)
	}
	if q.Status != "delivered" {
		t.Errorf("status期待: delivered, 実際: %s", q.Status)
	}
	if q.From != "test@" {
		t.Errorf("from期待: test@, 実際: %s", q.From)
	}
	if q.Sort != "subject" {
		t.Errorf("sort期待: subject, 実際: %s", q.Sort)
	}
	if q.Order != "asc" {
		t.Errorf("order期待: asc, 実際: %s", q.Order)
	}
	if q.HasAttachment == nil || !*q.HasAttachment {
		t.Error("has_attachment期待: true")
	}
	if q.Since == nil {
		t.Fatal("since期待: 非nil")
	}
	expectedSince, _ := time.Parse(time.RFC3339, "2024-01-01T00:00:00Z")
	if !q.Since.Equal(expectedSince) {
		t.Errorf("since期待: %v, 実際: %v", expectedSince, *q.Since)
	}
}

// TestParseListQuery_InvalidPage は無効なpageパラメーターでエラーが返ることを確認する。
func TestParseListQuery_InvalidPage(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{name: "非数値", query: "/messages?page=abc"},
		{name: "0以下", query: "/messages?page=0"},
		{name: "負の数", query: "/messages?page=-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.query, nil)
			_, err := parseListQuery(req)
			if err == nil {
				t.Errorf("無効なpage=%sでエラーが期待されるが、エラーがなかった", tt.query)
			}
		})
	}
}

// TestParseListQuery_InvalidPerPage は無効なper_pageパラメーターでエラーが返ることを確認する。
func TestParseListQuery_InvalidPerPage(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{name: "非数値", query: "/messages?per_page=abc"},
		{name: "0以下", query: "/messages?per_page=0"},
		{name: "100超", query: "/messages?per_page=101"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.query, nil)
			_, err := parseListQuery(req)
			if err == nil {
				t.Errorf("無効なper_page=%sでエラーが期待されるが、エラーがなかった", tt.query)
			}
		})
	}
}

// TestParseListQuery_InvalidSort は無効なsortパラメーターでエラーが返ることを確認する。
func TestParseListQuery_InvalidSort(t *testing.T) {
	req := httptest.NewRequest("GET", "/messages?sort=invalid_column", nil)
	_, err := parseListQuery(req)
	if err == nil {
		t.Error("無効なsortでエラーが期待されるが、エラーがなかった")
	}
}

// TestParseListQuery_InvalidOrder は無効なorderパラメーターでエラーが返ることを確認する。
func TestParseListQuery_InvalidOrder(t *testing.T) {
	req := httptest.NewRequest("GET", "/messages?order=invalid", nil)
	_, err := parseListQuery(req)
	if err == nil {
		t.Error("無効なorderでエラーが期待されるが、エラーがなかった")
	}
}

// TestParseListQuery_InvalidSince は無効なsinceパラメーターでエラーが返ることを確認する。
func TestParseListQuery_InvalidSince(t *testing.T) {
	req := httptest.NewRequest("GET", "/messages?since=not-a-date", nil)
	_, err := parseListQuery(req)
	if err == nil {
		t.Error("無効なsinceでエラーが期待されるが、エラーがなかった")
	}
}

// TestParseListQuery_MaxPerPage は最大per_page=100が正常に処理されることを確認する。
func TestParseListQuery_MaxPerPage(t *testing.T) {
	req := httptest.NewRequest("GET", "/messages?per_page=100", nil)
	q, err := parseListQuery(req)
	if err != nil {
		t.Fatalf("per_page=100のパース失敗: %v", err)
	}
	if q.PerPage != 100 {
		t.Errorf("per_page期待: 100, 実際: %d", q.PerPage)
	}
}

// TestCalcTotalPages はページ数計算が正しいことを確認する。
func TestCalcTotalPages(t *testing.T) {
	tests := []struct {
		name     string
		total    int
		perPage  int
		expected int
	}{
		{name: "割り切れる場合", total: 100, perPage: 20, expected: 5},
		{name: "割り切れない場合", total: 101, perPage: 20, expected: 6},
		{name: "total=0", total: 0, perPage: 20, expected: 0},
		{name: "total=1", total: 1, perPage: 20, expected: 1},
		{name: "total=perPage", total: 20, perPage: 20, expected: 1},
		{name: "perPage=0（ゼロ除算回避）", total: 100, perPage: 0, expected: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calcTotalPages(tt.total, tt.perPage)
			if result != tt.expected {
				t.Errorf("期待: %d, 実際: %d", tt.expected, result)
			}
		})
	}
}
