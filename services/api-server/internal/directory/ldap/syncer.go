package ldap

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	goldap "github.com/go-ldap/ldap/v3"

	"github.com/koizumib/mailshield/services/api-server/internal/directory"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

// UserDeactivator は Syncer が必要とする repository.Repository のサブセット。
// コンシューマー側（本パッケージ）で狭く定義する。
type UserDeactivator interface {
	// DeactivateMissingLDAPUsers は provisioned_by=ldap のユーザーのうち、
	// presentEmails に含まれないものを is_active=0 にし、無効化した件数を返す。
	DeactivateMissingLDAPUsers(ctx context.Context, presentEmails []string) (int, error)
}

// MailboxAssignmentSyncer は Syncer が必要とする repository.Repository のサブセット。
type MailboxAssignmentSyncer interface {
	// SyncMailboxAssignmentsForUser は 1 ユーザー分のメールボックス割り当てを
	// desired に一致させる（詳細は repository.Repository の同名メソッドを参照）。
	SyncMailboxAssignmentsForUser(ctx context.Context, userID string, source domain.ProvisionedBy, desired []repository.MailboxAssignmentRequest) error
	// ListMailboxes は全メールボックスを返す。fixed 方式のロール
	// （この同期ソースが管理する全メールボックスへの一括付与）の対象決定に使う。
	ListMailboxes(ctx context.Context) ([]repository.Mailbox, error)
}

// SyncConfig は 1 回の同期処理に必要なディレクトリ側の設定。
type SyncConfig struct {
	BaseDN     string
	UserFilter string
	EmailAttr  string
	NameAttr   string
	GroupsAttr string
	RoleMapper directory.GroupRoleMapper
	// MailboxResolution はメールボックス割り当ての解決設定（ロールごとに
	// user_attribute / group_search / fixed を選択）。nil なら自動反映しない。
	MailboxResolution *MailboxResolution
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

// Syncer は LDAP ディレクトリと users / mailbox_assignments テーブルを同期する。
type Syncer struct {
	provisioner   *directory.Provisioner
	deactivator   UserDeactivator
	mailboxSyncer MailboxAssignmentSyncer
	cfg           SyncConfig
}

// NewSyncer は Syncer を返す。mailboxSyncer が nil の場合、メールボックス割り当ての
// 自動同期は行わない（role/display_name の同期のみ）。
func NewSyncer(provisioner *directory.Provisioner, deactivator UserDeactivator, mailboxSyncer MailboxAssignmentSyncer, cfg SyncConfig) *Syncer {
	return &Syncer{provisioner: provisioner, deactivator: deactivator, mailboxSyncer: mailboxSyncer, cfg: cfg}
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
//
// メールボックス割り当て（MailboxResolution 設定時）は 2 パスで反映する:
//   - 第1パス（ユーザーループ内）: user_attribute + group_search で解決した割り当てを
//     ユーザーごとに reconcile する。fixed 対象ユーザーはこの時点では確定できない
//     （「全メールボックス」の集合が第1パスで作成されるメールボックスに依存する）ため保留する
//   - 第2パス（ループ後）: DB のメールボックス一覧（第1パスの作成分を含む）を取得し、
//     fixed 対象ユーザーへ「provisioned_by=ldap の全メールボックス × fixed ロール」を付与する
func (s *Syncer) Sync(ctx context.Context, searcher Searcher) (Result, error) {
	attrs := []string{s.cfg.EmailAttr, s.cfg.NameAttr, s.cfg.GroupsAttr}
	entries, err := searcher.SearchUsers(s.cfg.BaseDN, s.cfg.UserFilter, attrs)
	if err != nil {
		return Result{}, fmt.Errorf("LDAP ユーザー検索失敗: %w", err)
	}

	var result Result
	presentEmails := make([]string, 0, len(entries))

	mailboxEnabled := s.mailboxSyncer != nil && !s.cfg.MailboxResolution.Empty()
	var groupTuples map[string][]directory.MailboxAssignmentTuple
	cache := NewDerefCache()
	if mailboxEnabled {
		groupTuples, err = s.cfg.MailboxResolution.ResolveGroupSearchAll(searcher)
		if err != nil {
			// group_search の失敗はメールボックス反映のみ諦め、ユーザー同期は続行する。
			// 中途半端な groupTuples で reconcile すると正当な割り当てを誤削除しうるため、
			// このサイクルの割り当て反映はまるごとスキップする。
			slog.Error("LDAP同期: group_search 失敗（このサイクルのメールボックス反映をスキップ）", "error", err)
			result.Errors = append(result.Errors, err)
			mailboxEnabled = false
		}
	}

	// fixed 方式の対象ユーザーは第2パスで処理する（第1パスの解決結果を持ち越す）
	type pendingFixed struct {
		userID string
		email  string
		tuples []directory.MailboxAssignmentTuple
	}
	var pending []pendingFixed

	for _, e := range entries {
		email := e.FirstAttr(s.cfg.EmailAttr)
		if email == "" {
			slog.Warn("LDAP同期: email 属性が空のためスキップ", "dn", e.DN, "email_attr", s.cfg.EmailAttr)
			result.Skipped++
			continue
		}

		displayName := e.FirstAttr(s.cfg.NameAttr)
		groupDNs := e.Attributes[s.cfg.GroupsAttr]
		role := s.cfg.RoleMapper.Resolve(groupDNs)

		dbUser, err := s.provisioner.Provision(ctx, directory.ExternalIdentity{
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

		if !mailboxEnabled {
			continue
		}

		tuples := s.cfg.MailboxResolution.ResolveUserAttribute(searcher, e, cache)
		tuples = append(tuples, groupTuples[NormalizeDN(e.DN)]...)

		if len(s.cfg.MailboxResolution.FixedRolesForEmail(email)) > 0 {
			pending = append(pending, pendingFixed{userID: dbUser.ID, email: email, tuples: tuples})
			continue
		}

		// desired が空でも呼ぶ。グループから正当に離脱したユーザーの
		// メールボックス割り当てを剥奪するのに必要なため（0件ガードは不要。
		// LDAP 検索全体が0件の場合はこのループ自体が回らないため既に保護されている）。
		if err := s.mailboxSyncer.SyncMailboxAssignmentsForUser(ctx, dbUser.ID, domain.ProvisionedByLDAP, toRequests(tuples)); err != nil {
			slog.Error("LDAP同期: メールボックス割り当て同期失敗", "email", email, "error", err)
			result.Errors = append(result.Errors, err)
		}
	}

	// 第2パス: fixed 対象ユーザー。第1パスで作成されたメールボックスを含む
	// DB の一覧（provisioned_by=ldap のみ）に対して fixed ロールを付与する。
	if mailboxEnabled && len(pending) > 0 {
		ldapMailboxes, err := s.listLDAPMailboxEmails(ctx)
		if err != nil {
			slog.Error("LDAP同期: fixed 用メールボックス一覧取得失敗", "error", err)
			result.Errors = append(result.Errors, err)
		} else {
			for _, p := range pending {
				tuples := p.tuples
				for _, fixedRole := range s.cfg.MailboxResolution.FixedRolesForEmail(p.email) {
					for _, mb := range ldapMailboxes {
						tuples = append(tuples, directory.MailboxAssignmentTuple{MailboxEmail: mb, Role: fixedRole})
					}
				}
				if err := s.mailboxSyncer.SyncMailboxAssignmentsForUser(ctx, p.userID, domain.ProvisionedByLDAP, toRequests(tuples)); err != nil {
					slog.Error("LDAP同期: fixed メールボックス割り当て同期失敗", "email", p.email, "error", err)
					result.Errors = append(result.Errors, err)
				}
			}
		}
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

// toRequests は解決済みタプルを repository の入力形式に変換する。
func toRequests(tuples []directory.MailboxAssignmentTuple) []repository.MailboxAssignmentRequest {
	desired := make([]repository.MailboxAssignmentRequest, len(tuples))
	for i, t := range tuples {
		desired[i] = repository.MailboxAssignmentRequest{
			MailboxEmail:       t.MailboxEmail,
			MailboxDisplayName: t.MailboxDisplayName,
			Role:               t.Role,
		}
	}
	return desired
}

// listLDAPMailboxEmails は provisioned_by=ldap のメールボックスのアドレス一覧を返す。
// fixed 方式の付与対象は「この同期ソースが管理する世界」に限定する
// （manual のメールボックスは Web UI での手動割り当てに委ねる）。
func (s *Syncer) listLDAPMailboxEmails(ctx context.Context) ([]string, error) {
	mailboxes, err := s.mailboxSyncer.ListMailboxes(ctx)
	if err != nil {
		return nil, fmt.Errorf("メールボックス一覧取得失敗: %w", err)
	}
	var emails []string
	for _, mb := range mailboxes {
		if mb.ProvisionedBy == domain.ProvisionedByLDAP && mb.IsActive {
			emails = append(emails, mb.EmailAddress)
		}
	}
	return emails, nil
}

// ExtractCN は LDAP DN から先頭 RDN（通常 CN）の値を抽出する。
// メールボックスのグループ→role マッピングは CN（表示名相当）に対して照合するため、
// memberOf 属性の値（DN 形式）から比較用の識別子を取り出すのに使う。
// パース失敗時は DN をそのまま返す（呼び出し側でどのマッピングにも一致しないだけで、
// 処理全体を止める必要はないため）。
func ExtractCN(dn string) string {
	parsed, err := goldap.ParseDN(dn)
	if err != nil || len(parsed.RDNs) == 0 || len(parsed.RDNs[0].Attributes) == 0 {
		return dn
	}
	return parsed.RDNs[0].Attributes[0].Value
}
