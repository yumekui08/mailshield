package otp

import "errors"

var (
	ErrCodeNotFound    = errors.New("OTP コードが見つかりません（期限切れか未発行）")
	ErrInvalidCode     = errors.New("OTP コードが正しくありません")
	ErrTooManyAttempts = errors.New("試行回数の上限に達しました。新しいコードを発行してください")
	ErrSessionNotFound = errors.New("OTP セッションが見つかりません（期限切れか無効）")
)
