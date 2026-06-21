package pwreset

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// MariaDBStore は MariaDB を使ったパスワードリセットトークンストアである。
// Redis が不要な単一ノード構成向け。pwreset_tokens テーブルを使用する。
type MariaDBStore struct {
	db *sql.DB
}

// NewMariaDBStore は MariaDB パスワードリセットストアを初期化する。
func NewMariaDBStore(db *sql.DB) *MariaDBStore {
	return &MariaDBStore{db: db}
}

func (s *MariaDBStore) GenerateToken(ctx context.Context, userID string) (string, error) {
	token := uuid.New().String()
	expiresAt := time.Now().Add(TokenTTL).UTC()

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pwreset_tokens (token, user_id, expires_at) VALUES (?, ?, ?)`,
		token, userID, expiresAt,
	)
	if err != nil {
		return "", fmt.Errorf("リセットトークン保存失敗: %w", err)
	}
	return token, nil
}

func (s *MariaDBStore) ConsumeToken(ctx context.Context, token string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("トランザクション開始失敗: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	var userID string
	err = tx.QueryRowContext(ctx,
		`SELECT user_id FROM pwreset_tokens WHERE token = ? AND expires_at > NOW()`,
		token,
	).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrTokenNotFound
	}
	if err != nil {
		return "", fmt.Errorf("リセットトークン取得失敗: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM pwreset_tokens WHERE token = ?`, token); err != nil {
		return "", fmt.Errorf("リセットトークン削除失敗: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return "", fmt.Errorf("コミット失敗: %w", err)
	}
	return userID, nil
}
