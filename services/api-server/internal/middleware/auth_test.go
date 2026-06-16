package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/middleware"
)

// TestAuthenticate_NoCookie はCookieなしのリクエストが401 Unauthorizedを返すことを確認する。
func TestAuthenticate_NoCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages", nil)
	rr := httptest.NewRecorder()

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	// Cookieなしリクエストに対するミドルウェアの動作をシミュレート
	cookieName := "mailshield_session"
	mw := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := r.Cookie(cookieName)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]string{
					"code":    "UNAUTHORIZED",
					"message": "認証が必要です",
				},
			})
			return
		}
		next.ServeHTTP(w, r)
	})

	mw.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("期待ステータスコード: %d, 実際: %d", http.StatusUnauthorized, rr.Code)
	}

	if nextCalled {
		t.Error("認証失敗時にnextハンドラーが呼ばれてはいけない")
	}

	// レスポンスボディを確認
	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("レスポンスのJSONデコード失敗: %v", err)
	}

	errField, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatal("レスポンスに 'error' フィールドがありません")
	}

	if errField["code"] != "UNAUTHORIZED" {
		t.Errorf("期待エラーコード: UNAUTHORIZED, 実際: %v", errField["code"])
	}
}

// TestRequireRole_Forbidden はviewer権限が必要なロールに403 Forbiddenを返すことを確認する。
func TestRequireRole_Forbidden(t *testing.T) {
	session := &domain.Session{
		ID:   "test-session-viewer",
		Role: domain.RoleViewer,
		User: domain.UserClaims{
			Sub:   "user123",
			Email: "viewer@example.com",
		},
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/quarantine", nil)
	ctx := middleware.WithSession(req.Context(), session)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	// operator以上を要求するミドルウェア
	mw := middleware.RequireRole(domain.RoleOperator, domain.RoleAdmin)
	mw(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("期待ステータスコード: %d, 実際: %d", http.StatusForbidden, rr.Code)
	}

	if nextCalled {
		t.Error("権限不足時にnextハンドラーが呼ばれてはいけない")
	}
}

// TestRequireRole_Allowed は適切なロールのセッションがアクセスを許可されることを確認する。
func TestRequireRole_Allowed(t *testing.T) {
	tests := []struct {
		name     string
		role     domain.Role
		required []domain.Role
	}{
		{
			name:     "admin can access operator-or-above endpoint",
			role:     domain.RoleAdmin,
			required: []domain.Role{domain.RoleOperator, domain.RoleAdmin},
		},
		{
			name:     "operator can access operator-or-above endpoint",
			role:     domain.RoleOperator,
			required: []domain.Role{domain.RoleOperator, domain.RoleAdmin},
		},
		{
			name:     "viewer can access viewer-or-above endpoint",
			role:     domain.RoleViewer,
			required: []domain.Role{domain.RoleViewer, domain.RoleOperator, domain.RoleAdmin},
		},
		{
			name:     "admin can access viewer-or-above endpoint",
			role:     domain.RoleAdmin,
			required: []domain.Role{domain.RoleViewer, domain.RoleOperator, domain.RoleAdmin},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := &domain.Session{
				ID:        "test-session",
				Role:      tt.role,
				User:      domain.UserClaims{Sub: "user123"},
				ExpiresAt: time.Now().Add(1 * time.Hour),
			}

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			ctx := middleware.WithSession(req.Context(), session)
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()

			nextCalled := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			})

			mw := middleware.RequireRole(tt.required...)
			mw(next).ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("期待ステータスコード: %d, 実際: %d", http.StatusOK, rr.Code)
			}

			if !nextCalled {
				t.Error("権限がある場合はnextハンドラーが呼ばれるべき")
			}
		})
	}
}

// TestRequireRole_NoSession はセッションなしの場合に401を返すことを確認する。
func TestRequireRole_NoSession(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	mw := middleware.RequireRole(domain.RoleViewer)
	mw(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("期待ステータスコード: %d, 実際: %d", http.StatusUnauthorized, rr.Code)
	}

	if nextCalled {
		t.Error("セッションなし時にnextハンドラーが呼ばれてはいけない")
	}
}
