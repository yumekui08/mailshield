package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/koizumib/mailshield/services/api-server/internal/config"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
)

const (
	sessionKeyPrefix = "session:"
	stateKeyPrefix   = "oidc_state:"
	stateTTL         = 10 * time.Minute
)

// stateValue はOIDC stateと対応するnonce・リダイレクト先を保持する。
type stateValue struct {
	Nonce      string `json:"nonce"`
	RedirectTo string `json:"redirect_to"`
}

// SessionStore はセッション・OIDC ステート管理の抽象インターフェースである。
// Redis 実装（RedisSessionStore）と MariaDB 実装（MariaDBSessionStore）が存在する。
type SessionStore interface {
	Create(ctx context.Context, session *domain.Session) (string, error)
	Get(ctx context.Context, sessionID string) (*domain.Session, error)
	Delete(ctx context.Context, sessionID string) error
	SaveState(ctx context.Context, state, nonce, redirectTo string) error
	ConsumeState(ctx context.Context, state string) (nonce, redirectTo string, err error)
}

// RedisSessionStore は Redis を使ったセッションストアである。
type RedisSessionStore struct {
	client     *redis.Client
	sessionCfg *config.SessionConfig
}

// NewSessionStore は RedisSessionStore を初期化して返す。
func NewSessionStore(client *redis.Client, cfg *config.SessionConfig) *RedisSessionStore {
	return &RedisSessionStore{
		client:     client,
		sessionCfg: cfg,
	}
}

// Create はセッションをRedisに保存してセッションIDを返す。
func (s *RedisSessionStore) Create(ctx context.Context, session *domain.Session) (string, error) {
	sessionID := uuid.New().String()
	session.ID = sessionID

	data, err := json.Marshal(session)
	if err != nil {
		return "", fmt.Errorf("セッションのJSONエンコード失敗: %w", err)
	}

	key := sessionKeyPrefix + sessionID
	ttl := time.Duration(s.sessionCfg.TTLMinutes) * time.Minute

	if err := s.client.Set(ctx, key, data, ttl).Err(); err != nil {
		return "", fmt.Errorf("セッション保存失敗 (session_id=%s): %w", sessionID, err)
	}

	return sessionID, nil
}

// Get はセッションIDに対応するセッションをRedisから取得する。
func (s *RedisSessionStore) Get(ctx context.Context, sessionID string) (*domain.Session, error) {
	key := sessionKeyPrefix + sessionID

	data, err := s.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("セッションが見つかりません (session_id=%s)", sessionID)
		}
		return nil, fmt.Errorf("セッション取得失敗 (session_id=%s): %w", sessionID, err)
	}

	var session domain.Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("セッションのJSONデコード失敗 (session_id=%s): %w", sessionID, err)
	}

	return &session, nil
}

// Delete はセッションをRedisから削除する。
func (s *RedisSessionStore) Delete(ctx context.Context, sessionID string) error {
	key := sessionKeyPrefix + sessionID

	if err := s.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("セッション削除失敗 (session_id=%s): %w", sessionID, err)
	}

	return nil
}

// SaveState はOIDC認証フローのstateをRedisに保存する（TTL: 10分）。
func (s *RedisSessionStore) SaveState(ctx context.Context, state, nonce, redirectTo string) error {
	sv := stateValue{
		Nonce:      nonce,
		RedirectTo: redirectTo,
	}

	data, err := json.Marshal(sv)
	if err != nil {
		return fmt.Errorf("stateのJSONエンコード失敗: %w", err)
	}

	key := stateKeyPrefix + state
	if err := s.client.Set(ctx, key, data, stateTTL).Err(); err != nil {
		return fmt.Errorf("state保存失敗 (state=%s): %w", state, err)
	}

	return nil
}

// ConsumeState はOIDC stateを取得して即座に削除する（一度だけ使用可能）。
// 対応するnonceとリダイレクト先を返す。
func (s *RedisSessionStore) ConsumeState(ctx context.Context, state string) (nonce, redirectTo string, err error) {
	key := stateKeyPrefix + state

	data, err := s.client.GetDel(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return "", "", fmt.Errorf("stateが見つかりません (state=%s)", state)
		}
		return "", "", fmt.Errorf("state取得失敗 (state=%s): %w", state, err)
	}

	var sv stateValue
	if err := json.Unmarshal(data, &sv); err != nil {
		return "", "", fmt.Errorf("stateのJSONデコード失敗 (state=%s): %w", state, err)
	}

	return sv.Nonce, sv.RedirectTo, nil
}
