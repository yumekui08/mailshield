package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/koizumib/mailshield/services/api-server/internal/config"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
)

// MariaDBSessionStore は MariaDB を使ったセッション・OIDC ステートストアである。
// Redis が不要な単一ノード構成向け。sessions / oidc_states テーブルを使用する。
type MariaDBSessionStore struct {
	db         *sql.DB
	sessionCfg *config.SessionConfig
}

// NewMariaDBSessionStore は MariaDB セッションストアを初期化する。
func NewMariaDBSessionStore(db *sql.DB, cfg *config.SessionConfig) *MariaDBSessionStore {
	return &MariaDBSessionStore{db: db, sessionCfg: cfg}
}

func (s *MariaDBSessionStore) Create(ctx context.Context, session *domain.Session) (string, error) {
	sessionID := uuid.New().String()
	session.ID = sessionID

	data, err := json.Marshal(session)
	if err != nil {
		return "", fmt.Errorf("セッションのJSONエンコード失敗: %w", err)
	}

	ttl := time.Duration(s.sessionCfg.TTLMinutes) * time.Minute
	expiresAt := time.Now().Add(ttl).UTC()

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, data, expires_at) VALUES (?, ?, ?)`,
		sessionID, data, expiresAt,
	)
	if err != nil {
		return "", fmt.Errorf("セッション保存失敗 (session_id=%s): %w", sessionID, err)
	}
	return sessionID, nil
}

func (s *MariaDBSessionStore) Get(ctx context.Context, sessionID string) (*domain.Session, error) {
	var data []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT data FROM sessions WHERE id = ? AND expires_at > NOW()`,
		sessionID,
	).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("セッションが見つかりません (session_id=%s)", sessionID)
	}
	if err != nil {
		return nil, fmt.Errorf("セッション取得失敗 (session_id=%s): %w", sessionID, err)
	}

	var session domain.Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("セッションのJSONデコード失敗 (session_id=%s): %w", sessionID, err)
	}
	return &session, nil
}

func (s *MariaDBSessionStore) Delete(ctx context.Context, sessionID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sessionID); err != nil {
		return fmt.Errorf("セッション削除失敗 (session_id=%s): %w", sessionID, err)
	}
	return nil
}

// SaveState は OIDC 認証フローの state を MariaDB に保存する（TTL: 10 分）。
func (s *MariaDBSessionStore) SaveState(ctx context.Context, state, nonce, redirectTo string) error {
	expiresAt := time.Now().Add(10 * time.Minute).UTC()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO oidc_states (state, nonce, redirect_to, expires_at) VALUES (?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE nonce=VALUES(nonce), redirect_to=VALUES(redirect_to), expires_at=VALUES(expires_at)`,
		state, nonce, redirectTo, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("state 保存失敗 (state=%s): %w", state, err)
	}
	return nil
}

// ConsumeState は OIDC state を取得して即座に削除する（ワンタイム）。
func (s *MariaDBSessionStore) ConsumeState(ctx context.Context, state string) (nonce, redirectTo string, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", fmt.Errorf("トランザクション開始失敗: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	var n, r string
	err = tx.QueryRowContext(ctx,
		`SELECT nonce, redirect_to FROM oidc_states WHERE state = ? AND expires_at > NOW()`,
		state,
	).Scan(&n, &r)
	if err == sql.ErrNoRows {
		return "", "", fmt.Errorf("state が見つかりません (state=%s)", state)
	}
	if err != nil {
		return "", "", fmt.Errorf("state 取得失敗 (state=%s): %w", state, err)
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM oidc_states WHERE state = ?`, state); err != nil {
		return "", "", fmt.Errorf("state 削除失敗 (state=%s): %w", state, err)
	}

	if err = tx.Commit(); err != nil {
		return "", "", fmt.Errorf("トランザクションコミット失敗: %w", err)
	}
	return n, r, nil
}
