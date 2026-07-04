package ldap

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/koizumib/mailshield/services/api-server/internal/directory"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
)

// UserDeactivator は Syncer が必要とする repository.Repository のサブセット。
// コンシューマー側（本パッケージ）で狭く定義する。
type UserDeactivator interface {
	// DeactivateMissingLDAPUsers は provisioned_by=ldap のユーザーのうち、
	// presentEmails に含まれないものを is_active=0 にし、無効化した件数を返す。
	DeactivateMissingLDAPUsers(ctx context.Context, presentEmails []string) (int, error)
}

// SyncConfig は 1 回の同期処理に必要なディレクトリ側の設定。
type SyncConfig struct {
	BaseDN     string
	UserFilter string
	EmailAttr  string
	NameAttr   string
	GroupsAttr string
	RoleMapper directory.GroupRoleMapper
	// DeactivateMissing を true にすると、同期結果に含まれなくなった
	// provisioned_by=ldap のユーザーを無効化する。
	DeactivateMissing bool
}

// Result は 1 回の同期の結果を表す。
type Result struct {
	Synced      int
	Skipped     int
	Deactivated int
	Errors      []error
}

// Syncer は LDAP ディレクトリと users テーブルを同期する。
type Syncer struct {
	provisioner *directory.Provisioner
	deactivator UserDeactivator
	cfg         SyncConfig
}

// NewSyncer は Syncer を返す。
func NewSyncer(provisioner *directory.Provisioner, deactivator UserDeactivator, cfg SyncConfig) *Syncer {
	return &Syncer{provisioner: provisioner, deactivator: deactivator, cfg: cfg}
}

// RunPeriodic は起動直後に1回同期し、以後 interval 間隔で同期を繰り返すループを起動する。
// ctx がキャンセルされるまでブロックする。
// 承認フローの定期ジョブ（RunExpiryWorker 等）と異なり起動直後にも同期するのは、
// 新規インストール後に sync_interval_minutes（デフォルト60分）分もユーザーが
// 同期されないまま待たされる状況を避けるため。
// LDAP 接続は同期のたびに新規に張り直す（低頻度・長時間稼働の定期ジョブでは、
// 接続を張りっぱなしにするよりサーバー再起動等に対して堅牢なため）。
func (s *Syncer) RunPeriodic(ctx context.Context, connCfg ConnConfig, interval time.Duration) {
	s.syncOnce(ctx, connCfg)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.syncOnce(ctx, connCfg)
		}
	}
}

func (s *Syncer) syncOnce(ctx context.Context, connCfg ConnConfig) {
	searcher, err := Dial(connCfg)
	if err != nil {
		slog.Error("LDAP同期: 接続失敗", "error", err)
		return
	}
	defer searcher.Close()

	if _, err := s.Sync(ctx, searcher); err != nil {
		slog.Error("LDAP同期失敗", "error", err)
	}
}

// Sync は searcher から取得したエントリを1件ずつ Provisioner に渡し、
// 設定に応じて同期結果に含まれなくなったユーザーを無効化する。
// searcher は呼び出し側が Dial して渡し、本メソッドは Close しない
// （1 プロセス内で複数回の Sync に同じ接続を使い回すかどうかは呼び出し側の裁量とする）。
func (s *Syncer) Sync(ctx context.Context, searcher Searcher) (Result, error) {
	attrs := []string{s.cfg.EmailAttr, s.cfg.NameAttr, s.cfg.GroupsAttr}
	entries, err := searcher.SearchUsers(s.cfg.BaseDN, s.cfg.UserFilter, attrs)
	if err != nil {
		return Result{}, fmt.Errorf("LDAP ユーザー検索失敗: %w", err)
	}

	var result Result
	presentEmails := make([]string, 0, len(entries))

	for _, e := range entries {
		email := e.FirstAttr(s.cfg.EmailAttr)
		if email == "" {
			slog.Warn("LDAP同期: email 属性が空のためスキップ", "dn", e.DN, "email_attr", s.cfg.EmailAttr)
			result.Skipped++
			continue
		}

		displayName := e.FirstAttr(s.cfg.NameAttr)
		groups := e.Attributes[s.cfg.GroupsAttr]
		role := s.cfg.RoleMapper.Resolve(groups)

		_, err := s.provisioner.Provision(ctx, directory.ExternalIdentity{
			Email:       email,
			DisplayName: displayName,
			Role:        role,
			Source:      domain.ProvisionedByLDAP,
		})
		if err != nil {
			slog.Error("LDAP同期: ユーザーのプロビジョニング失敗", "email", email, "error", err)
			result.Errors = append(result.Errors, err)
			continue
		}
		result.Synced++
		presentEmails = append(presentEmails, email)
	}

	if s.cfg.DeactivateMissing {
		if len(presentEmails) == 0 {
			// LDAP 検索が 0 件（誤検知の可能性が高い）の場合に全 ldap ユーザーを
			// 無効化してしまわないよう、deactivator を呼ばない。
			slog.Warn("LDAP同期: 検索結果が0件のため無効化処理をスキップします（誤設定の可能性）")
		} else {
			n, err := s.deactivator.DeactivateMissingLDAPUsers(ctx, presentEmails)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("無効化処理失敗: %w", err))
			} else {
				result.Deactivated = n
			}
		}
	}

	slog.Info("LDAP同期完了",
		"synced", result.Synced, "skipped", result.Skipped,
		"deactivated", result.Deactivated, "errors", len(result.Errors))

	return result, nil
}
