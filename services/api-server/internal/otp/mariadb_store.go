package otp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// MariaDBStore は MariaDB を使った OTP ストアである。
// Redis が不要な単一ノード構成向け。otp_codes / otp_sessions テーブルを使用する。
type MariaDBStore struct {
	db *sql.DB
}

// NewMariaDBStore は MariaDB OTP ストアを初期化する。
func NewMariaDBStore(db *sql.DB) *MariaDBStore {
	return &MariaDBStore{db: db}
}

func (s *MariaDBStore) GenerateCode(ctx context.Context, token, email string) (string, error) {
	code, err := generateCode()
	if err != nil {
		return "", fmt.Errorf("OTP コード生成失敗: %w", err)
	}

	expiresAt := time.Now().Add(CodeTTL).UTC()
	id := uuid.New().String()

	// 再送信時は既存行を上書きする（token+email が UNIQUE KEY）
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO otp_codes (id, token, email, code, attempts, expires_at)
		 VALUES (?, ?, ?, ?, 0, ?)
		 ON DUPLICATE KEY UPDATE id=VALUES(id), code=VALUES(code), attempts=0, expires_at=VALUES(expires_at)`,
		id, token, email, code, expiresAt,
	)
	if err != nil {
		return "", fmt.Errorf("OTP コード保存失敗: %w", err)
	}
	return code, nil
}

func (s *MariaDBStore) Verify(ctx context.Context, token, email, code string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("トランザクション開始失敗: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// 試行回数をインクリメント
	res, err := tx.ExecContext(ctx,
		`UPDATE otp_codes SET attempts = attempts + 1 WHERE token = ? AND email = ? AND expires_at > NOW()`,
		token, email,
	)
	if err != nil {
		return "", fmt.Errorf("試行回数更新失敗: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return "", ErrCodeNotFound
	}

	var storedCode string
	var attempts int64
	err = tx.QueryRowContext(ctx,
		`SELECT code, attempts FROM otp_codes WHERE token = ? AND email = ? AND expires_at > NOW()`,
		token, email,
	).Scan(&storedCode, &attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrCodeNotFound
	}
	if err != nil {
		return "", fmt.Errorf("OTP コード取得失敗: %w", err)
	}

	if attempts > maxAttempts {
		if commitErr := tx.Commit(); commitErr != nil {
			return "", fmt.Errorf("コミット失敗: %w", commitErr)
		}
		err = nil
		return "", ErrTooManyAttempts
	}

	if storedCode != code {
		if commitErr := tx.Commit(); commitErr != nil {
			return "", fmt.Errorf("コミット失敗: %w", commitErr)
		}
		err = nil
		return "", ErrInvalidCode
	}

	// コードを削除（ワンタイム）
	if _, err = tx.ExecContext(ctx, `DELETE FROM otp_codes WHERE token = ? AND email = ?`, token, email); err != nil {
		return "", fmt.Errorf("OTP コード削除失敗: %w", err)
	}

	// OTP セッションを発行
	sessID := uuid.New().String()
	expiresAt := time.Now().Add(SessionTTL).UTC()
	if _, err = tx.ExecContext(ctx,
		`INSERT INTO otp_sessions (id, token, email, expires_at) VALUES (?, ?, ?, ?)`,
		sessID, token, email, expiresAt,
	); err != nil {
		return "", fmt.Errorf("OTP セッション作成失敗: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return "", fmt.Errorf("コミット失敗: %w", err)
	}
	return sessID, nil
}

func (s *MariaDBStore) ValidateSession(ctx context.Context, sessID, token string) (string, error) {
	var t, email string
	err := s.db.QueryRowContext(ctx,
		`SELECT token, email FROM otp_sessions WHERE id = ? AND expires_at > NOW()`,
		sessID,
	).Scan(&t, &email)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrSessionNotFound
	}
	if err != nil {
		return "", fmt.Errorf("OTP セッション取得失敗: %w", err)
	}
	if t != token {
		return "", ErrSessionNotFound
	}
	return email, nil
}
