package directory

import (
	"fmt"
	"regexp"

	"github.com/koizumib/mailshield/services/api-server/internal/domain"
)

// MailboxAssignmentTuple は「あるユーザーがどのメールボックスに、どの role で
// 所属するか」を表す。グループ名の明示マッピングで解決しても、命名規則の
// 正規表現で解決しても、最終的にはこの形に正規化する（パターン1・将来のパターン2
// いずれの発見方法でも同じ reconcile ロジックに渡せるようにするため）。
type MailboxAssignmentTuple struct {
	MailboxEmail string
	// MailboxDisplayName はメールボックスが存在せず新規作成される場合の表示名。
	// 空の場合は MailboxEmail をそのまま使う（Web UI の手動作成と同じ挙動）。
	MailboxDisplayName string
	Role               domain.AssignmentRole
}

// GroupMailboxMapping はグループ識別子(LDAP の CN・SCIM の displayName)から
// メールボックス + role への明示的な対応付け 1 件を表す。
type GroupMailboxMapping struct {
	Group              string
	MailboxEmail       string
	MailboxDisplayName string
	Role               domain.AssignmentRole
}

// GroupMailboxPattern はグループ名の命名規則からメールボックス + role を
// 機械的に抽出する正規表現ルール。個別のグループごとに設定を書かずに済むため、
// メールボックス数が多い組織向けの解決手段として使う。
//
// Regex には名前付きキャプチャグループ "mailbox" と "role" を含めること
// （例: `^mbx-(?P<mailbox>[\w.-]+)-(?P<role>member|owner|admin)$`）。
type GroupMailboxPattern struct {
	Regex *regexp.Regexp
	// MailboxDomain が空でない場合、"mailbox" キャプチャの値をローカルパートとみなし
	// "local@MailboxDomain" をメールボックスアドレスとする。空の場合は
	// "mailbox" キャプチャの値をメールアドレスとしてそのまま使う。
	MailboxDomain string
}

// NewGroupMailboxPattern は正規表現をコンパイルし、必須の名前付きキャプチャグループ
// ("mailbox" / "role") が含まれているか検証する。設定不正は起動時に検出する。
func NewGroupMailboxPattern(pattern, mailboxDomain string) (*GroupMailboxPattern, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("mailbox_mappings.pattern のコンパイル失敗: %w", err)
	}
	hasMailbox, hasRole := false, false
	for _, name := range re.SubexpNames() {
		switch name {
		case "mailbox":
			hasMailbox = true
		case "role":
			hasRole = true
		}
	}
	if !hasMailbox || !hasRole {
		return nil, fmt.Errorf(`mailbox_mappings.pattern には名前付きキャプチャグループ (?P<mailbox>...) と (?P<role>...) が必要です: %q`, pattern)
	}
	return &GroupMailboxPattern{Regex: re, MailboxDomain: mailboxDomain}, nil
}

// GroupMailboxMapper はグループ識別子の集合から []MailboxAssignmentTuple を解決する。
// 明示マッピング(Mappings)・命名規則(Pattern)の両方、または片方だけを設定できる。
// 両方一致した場合は両方のタプルが得られる（重複は呼び出し側の reconcile で吸収される）。
type GroupMailboxMapper struct {
	Mappings []GroupMailboxMapping
	Pattern  *GroupMailboxPattern
}

// Resolve はグループ識別子の集合からメールボックス割り当てタプルを解決する。
// 不正な role（member/owner/admin 以外）にマッチした場合、そのタプルは無視する。
func (m GroupMailboxMapper) Resolve(groupNames []string) []MailboxAssignmentTuple {
	var tuples []MailboxAssignmentTuple

	for _, g := range groupNames {
		for _, mapping := range m.Mappings {
			if mapping.Group == g {
				tuples = append(tuples, MailboxAssignmentTuple{
					MailboxEmail:       mapping.MailboxEmail,
					MailboxDisplayName: mapping.MailboxDisplayName,
					Role:               mapping.Role,
				})
			}
		}

		if m.Pattern != nil {
			if t, ok := m.Pattern.resolve(g); ok {
				tuples = append(tuples, t)
			}
		}
	}

	return tuples
}

func (p *GroupMailboxPattern) resolve(groupName string) (MailboxAssignmentTuple, bool) {
	match := p.Regex.FindStringSubmatch(groupName)
	if match == nil {
		return MailboxAssignmentTuple{}, false
	}

	var mailboxLocal, roleStr string
	for i, name := range p.Regex.SubexpNames() {
		switch name {
		case "mailbox":
			mailboxLocal = match[i]
		case "role":
			roleStr = match[i]
		}
	}
	if mailboxLocal == "" {
		return MailboxAssignmentTuple{}, false
	}

	role := domain.AssignmentRole(roleStr)
	switch role {
	case domain.AssignmentRoleMember, domain.AssignmentRoleOwner, domain.AssignmentRoleAdmin:
	default:
		return MailboxAssignmentTuple{}, false
	}

	email := mailboxLocal
	if p.MailboxDomain != "" {
		email = mailboxLocal + "@" + p.MailboxDomain
	}

	return MailboxAssignmentTuple{MailboxEmail: email, Role: role}, true
}
