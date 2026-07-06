package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterAllow(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	now := base
	l := NewRateLimiter(3, time.Minute)
	l.now = func() time.Time { return now }

	for i := range 3 {
		if !l.Allow("1.2.3.4") {
			t.Fatalf("%d 回目で拒否された（上限 3）", i+1)
		}
	}
	if l.Allow("1.2.3.4") {
		t.Error("上限超過後に許可された")
	}

	// 別キーは独立してカウントされる
	if !l.Allow("5.6.7.8") {
		t.Error("別キーが拒否された")
	}

	// ウィンドウ経過後は再び許可される
	now = base.Add(61 * time.Second)
	if !l.Allow("1.2.3.4") {
		t.Error("ウィンドウ経過後に拒否された")
	}
}

func TestRateLimiterSweep(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	now := base
	l := NewRateLimiter(1, time.Minute)
	l.now = func() time.Time { return now }

	l.Allow("a")
	l.Allow("b")
	if len(l.hits) != 2 {
		t.Fatalf("キー数 = %d, want 2", len(l.hits))
	}

	// 2 ウィンドウ経過後のアクセスで古いキーが掃除される
	now = base.Add(3 * time.Minute)
	l.Allow("c")
	l.mu.Lock()
	n := len(l.hits)
	l.mu.Unlock()
	if n != 1 {
		t.Errorf("掃除後のキー数 = %d, want 1", n)
	}
}

func TestRateLimiterMiddleware(t *testing.T) {
	l := NewRateLimiter(2, time.Minute)
	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	do := func(remoteAddr string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		req.RemoteAddr = remoteAddr
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	if rec := do("10.0.0.1:1111"); rec.Code != http.StatusOK {
		t.Errorf("1回目 = %d, want 200", rec.Code)
	}
	if rec := do("10.0.0.1:2222"); rec.Code != http.StatusOK {
		t.Errorf("2回目 = %d, want 200（ポート違いは同一 IP 扱い）", rec.Code)
	}
	rec := do("10.0.0.1:3333")
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("3回目 = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") != "60" {
		t.Errorf("Retry-After = %q, want \"60\"", rec.Header().Get("Retry-After"))
	}

	// 別 IP は許可される
	if rec := do("10.0.0.2:1111"); rec.Code != http.StatusOK {
		t.Errorf("別 IP = %d, want 200", rec.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	wants := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
		"Cache-Control":          "no-store",
	}
	for k, v := range wants {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}
