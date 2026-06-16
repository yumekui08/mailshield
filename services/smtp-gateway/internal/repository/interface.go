// Package repository は MailRepository インターフェースを再エクスポートする。
// コンシューマーはこのパッケージを import してインターフェースを参照する。
package repository

import "github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"

// MailRepository は domain.MailRepository の再エクスポート。
type MailRepository = domain.MailRepository
