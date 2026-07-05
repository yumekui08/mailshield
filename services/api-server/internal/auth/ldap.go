package auth

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	goldap "github.com/go-ldap/ldap/v3"

	"github.com/koizumib/mailshield/services/api-server/internal/config"
	"github.com/koizumib/mailshield/services/api-server/internal/directory"
	ldapsync "github.com/koizumib/mailshield/services/api-server/internal/directory/ldap"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

// dialer は LDAPBindProvider が必要とする接続確立操作。
// 実装は ldapsync.Dial（テストではフェイクに差し替える）。
type dialer func(cfg ldapsync.ConnConfig) (ldapsync.Searcher, error)

// LDAPBindProvider は LDAP bind によるメール + パスワード認証を提供する。
//
// ユーザー情報（role・display_name）の真実の源は directory.Provisioner が管理する
// users テーブルであり、本 Provider はログインのたびに以下の search+bind パターンで
// パスワードを検証したうえで、OIDC の JIT と同じ Provisioner.Provision を呼ぶ。
//
//  1. サービスアカウント（connCfg.BindDN/BindPassword）で bind し、
//     メールアドレスからユーザーの DN を検索する
//  2. 見つかった DN + ログインフォームで入力されたパスワードで再度 bind し、
//     成功すればパスワードが正しいと判断する（LDAP サーバー自身にパスワード検証を委ねる。
//     こちら側でパスワードやハッシュを保持しない）
//  3. GroupRoleMapper で role を解決し、Provisioner.Provision で JIT プロビジョニングする
//     （権威順位 manual > ldap/scim > oidc は Provisioner 側で解決される）
//  4. mailboxSyncer が設定されていれば、同じグループ集合から MailboxMapper で
//     メールボックス割り当ても JIT 反映する（ログイン直後に本人のメールボックスが見えるようにする）。
//     このユーザー1人分の行だけを対象にするため、他ユーザーへの影響はない。
type LDAPBindProvider struct {
	dial          dialer
	connCfg       ldapsync.ConnConfig
	syncCfg       ldapsync.SyncConfig
	provisioner   *directory.Provisioner
	mailboxSyncer ldapsync.MailboxAssignmentSyncer
	sessionCfg    *config.SessionConfig
}

// NewLDAPBindProvider は config.LDAPConfig から LDAPBindProvider を構築する。
// mailboxSyncer が nil の場合、メールボックス割り当ての JIT 反映は行わない。
func NewLDAPBindProvider(ldapCfg *config.LDAPConfig, provisioner *directory.Provisioner, mailboxSyncer ldapsync.MailboxAssignmentSyncer, sessionCfg *config.SessionConfig) (*LDAPBindProvider, error) {
	connCfg, syncCfg, err := BuildLDAPConnConfig(ldapCfg)
	if err != nil {
		return nil, err
	}
	return &LDAPBindProvider{
		dial:          ldapsync.Dial,
		connCfg:       connCfg,
		syncCfg:       syncCfg,
		provisioner:   provisioner,
		mailboxSyncer: mailboxSyncer,
		sessionCfg:    sessionCfg,
	}, nil
}

// errInvalidCredentials はメールアドレス・パスワードの組み合わせが正しくないことを表す
// 統一エラーメッセージ。検索結果 0 件・複数件・bind 失敗のいずれでも同じ文言にすることで、
// エラーメッセージからユーザーの存在有無が推測できないようにする（列挙攻撃対策）。
var errInvalidCredentials = fmt.Errorf("メールアドレスまたはパスワードが正しくありません")

// Login はメールアドレスとパスワードを LDAP bind で検証してSessionを返す。
func (p *LDAPBindProvider) Login(ctx context.Context, email, password string) (*domain.Session, error) {
	if email == "" || password == "" {
		return nil, errInvalidCredentials
	}

	entry, err := p.findUserEntry(email)
	if err != nil {
		return nil, err
	}

	if err := p.verifyPassword(entry.DN, password); err != nil {
		return nil, err
	}

	groups := entry.Attributes[p.syncCfg.GroupsAttr]
	role := p.syncCfg.RoleMapper.Resolve(groups)
	dbUser, err := p.provisioner.Provision(ctx, directory.ExternalIdentity{
		Email:       email,
		DisplayName: entry.FirstAttr(p.syncCfg.NameAttr),
		Role:        role,
		Source:      domain.ProvisionedByLDAP,
	})
	if err != nil {
		return nil, fmt.Errorf("プロビジョニング失敗: %w", err)
	}
	if !dbUser.IsActive {
		return nil, fmt.Errorf("アカウントが無効です")
	}

	if p.mailboxSyncer != nil {
		groupCNs := make([]string, len(groups))
		for i, dn := range groups {
			groupCNs[i] = ldapsync.ExtractCN(dn)
		}
		tuples := p.syncCfg.MailboxMapper.Resolve(groupCNs)
		desired := make([]repository.MailboxAssignmentRequest, len(tuples))
		for i, t := range tuples {
			desired[i] = repository.MailboxAssignmentRequest{
				MailboxEmail:       t.MailboxEmail,
				MailboxDisplayName: t.MailboxDisplayName,
				Role:               t.Role,
			}
		}
		if err := p.mailboxSyncer.SyncMailboxAssignmentsForUser(ctx, dbUser.ID, domain.ProvisionedByLDAP, desired); err != nil {
			// メールボックス割り当ての反映失敗はログイン自体を失敗させない
			// （role/認証は既に確定しているため。次回ログインまたは定期同期で再試行される）。
			slog.Warn("LDAP bind ログイン: メールボックス割り当て同期失敗", "email", email, "error", err)
		}
	}

	return &domain.Session{
		User: domain.UserClaims{
			Sub:   dbUser.ID,
			Email: dbUser.Email,
			Name:  dbUser.DisplayName,
		},
		Role:      dbUser.Role,
		ExpiresAt: time.Now().Add(time.Duration(p.sessionCfg.TTLMinutes) * time.Minute),
	}, nil
}

// findUserEntry はサービスアカウントで bind し、email に一致するエントリを検索する。
// 一致が 0 件・複数件のいずれでも errInvalidCredentials を返す。
func (p *LDAPBindProvider) findUserEntry(email string) (ldapsync.Entry, error) {
	searchConn, err := p.dial(p.connCfg)
	if err != nil {
		return ldapsync.Entry{}, fmt.Errorf("LDAP 接続失敗: %w", err)
	}
	defer searchConn.Close()

	filter := fmt.Sprintf("(&%s(%s=%s))", p.syncCfg.UserFilter, p.syncCfg.EmailAttr, goldap.EscapeFilter(email))
	attrs := []string{p.syncCfg.EmailAttr, p.syncCfg.NameAttr, p.syncCfg.GroupsAttr}
	entries, err := searchConn.SearchUsers(p.syncCfg.BaseDN, filter, attrs)
	if err != nil {
		return ldapsync.Entry{}, fmt.Errorf("LDAP 検索失敗: %w", err)
	}
	if len(entries) != 1 {
		return ldapsync.Entry{}, errInvalidCredentials
	}
	return entries[0], nil
}

// verifyPassword は見つかった DN + ユーザー入力のパスワードで bind し、パスワードを検証する。
func (p *LDAPBindProvider) verifyPassword(dn, password string) error {
	verifyCfg := p.connCfg
	verifyCfg.BindDN = dn
	verifyCfg.BindPassword = password

	verifyConn, err := p.dial(verifyCfg)
	if err != nil {
		return errInvalidCredentials
	}
	return verifyConn.Close()
}
