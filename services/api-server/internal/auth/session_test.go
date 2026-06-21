package auth

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/koizumib/mailshield/services/api-server/internal/config"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
)

func newTestStore(t *testing.T) (*RedisSessionStore, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis起動失敗: %v", err)
	}
	t.Cleanup(mr.Close)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	cfg := &config.SessionConfig{
		TTLMinutes:  60,
		CookieName:  "mailshield_session",
		CookieSecure: false,
	}
	return NewSessionStore(client, cfg), mr
}

func testSession() *domain.Session {
	return &domain.Session{
		User: domain.UserClaims{
			Sub:   "user-sub-123",
			Email: "test@example.com",
			Name:  "Test User",
		},
		Role:        domain.RoleViewer,
		AccessToken: "access-token-xyz",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}
}

func TestSessionStore_CreateAndGet(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	sess := testSession()

	sessionID, err := store.Create(ctx, sess)
	if err != nil {
		t.Fatalf("Create 失敗: %v", err)
	}
	if sessionID == "" {
		t.Fatal("Create は空でないセッションIDを返すべき")
	}

	got, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("Get 失敗: %v", err)
	}
	if got.User.Sub != sess.User.Sub {
		t.Errorf("User.Sub 期待: %s, 実際: %s", sess.User.Sub, got.User.Sub)
	}
	if got.User.Email != sess.User.Email {
		t.Errorf("User.Email 期待: %s, 実際: %s", sess.User.Email, got.User.Email)
	}
	if got.Role != sess.Role {
		t.Errorf("Role 期待: %s, 実際: %s", sess.Role, got.Role)
	}
	if got.AccessToken != sess.AccessToken {
		t.Errorf("AccessToken 期待: %s, 実際: %s", sess.AccessToken, got.AccessToken)
	}
}

func TestSessionStore_GetAfterDelete(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	sessionID, err := store.Create(ctx, testSession())
	if err != nil {
		t.Fatalf("Create 失敗: %v", err)
	}

	if err := store.Delete(ctx, sessionID); err != nil {
		t.Fatalf("Delete 失敗: %v", err)
	}

	_, err = store.Get(ctx, sessionID)
	if err == nil {
		t.Error("Delete 後の Get はエラーを返すべき")
	}
}

func TestSessionStore_GetAfterTTLExpiry(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis起動失敗: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	cfg := &config.SessionConfig{TTLMinutes: 1}
	store := NewSessionStore(client, cfg)
	ctx := context.Background()

	sessionID, err := store.Create(ctx, testSession())
	if err != nil {
		t.Fatalf("Create 失敗: %v", err)
	}

	mr.FastForward(2 * time.Minute)

	_, err = store.Get(ctx, sessionID)
	if err == nil {
		t.Error("TTL切れ後の Get はエラーを返すべき")
	}
}

func TestSessionStore_SaveAndConsumeState(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	state := "test-state-abc"
	nonce := "test-nonce-xyz"
	redirectTo := "/dashboard"

	if err := store.SaveState(ctx, state, nonce, redirectTo); err != nil {
		t.Fatalf("SaveState 失敗: %v", err)
	}

	gotNonce, gotRedirectTo, err := store.ConsumeState(ctx, state)
	if err != nil {
		t.Fatalf("ConsumeState 失敗: %v", err)
	}
	if gotNonce != nonce {
		t.Errorf("nonce 期待: %s, 実際: %s", nonce, gotNonce)
	}
	if gotRedirectTo != redirectTo {
		t.Errorf("redirectTo 期待: %s, 実際: %s", redirectTo, gotRedirectTo)
	}
}

func TestSessionStore_ConsumeStateIsOneTimeUse(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	state := "one-time-state"

	if err := store.SaveState(ctx, state, "nonce", "/"); err != nil {
		t.Fatalf("SaveState 失敗: %v", err)
	}

	_, _, err := store.ConsumeState(ctx, state)
	if err != nil {
		t.Fatalf("初回 ConsumeState 失敗: %v", err)
	}

	_, _, err = store.ConsumeState(ctx, state)
	if err == nil {
		t.Error("2回目の ConsumeState はエラーを返すべき（一度限りの使用）")
	}
}

func TestSessionStore_ConsumeStateNotFound(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	_, _, err := store.ConsumeState(ctx, "nonexistent-state")
	if err == nil {
		t.Error("存在しない state の ConsumeState はエラーを返すべき")
	}
}

func TestSessionStore_GetNotFound(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	_, err := store.Get(ctx, "nonexistent-session-id")
	if err == nil {
		t.Error("存在しない sessionID の Get はエラーを返すべき")
	}
}
