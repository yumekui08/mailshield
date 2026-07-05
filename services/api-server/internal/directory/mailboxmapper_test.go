package directory

import (
	"testing"

	"github.com/koizumib/mailshield/services/api-server/internal/domain"
)

func TestGroupMailboxMapper_ExplicitMapping(t *testing.T) {
	mapper := GroupMailboxMapper{
		Mappings: []GroupMailboxMapping{
			{Group: "Sales-Team", MailboxEmail: "sales@example.com", MailboxDisplayName: "Sales", Role: domain.AssignmentRoleMember},
			{Group: "Sales-Managers", MailboxEmail: "sales@example.com", Role: domain.AssignmentRoleOwner},
		},
	}

	tuples := mapper.Resolve([]string{"Sales-Team", "Unrelated-Group"})
	if len(tuples) != 1 {
		t.Fatalf("tuples = %d 件, want 1", len(tuples))
	}
	if tuples[0].MailboxEmail != "sales@example.com" || tuples[0].Role != domain.AssignmentRoleMember {
		t.Errorf("tuples[0] = %+v", tuples[0])
	}
}

func TestGroupMailboxMapper_ExplicitMapping_MultipleGroupsSameMailbox(t *testing.T) {
	mapper := GroupMailboxMapper{
		Mappings: []GroupMailboxMapping{
			{Group: "Sales-Team", MailboxEmail: "sales@example.com", Role: domain.AssignmentRoleMember},
			{Group: "Sales-Managers", MailboxEmail: "sales@example.com", Role: domain.AssignmentRoleOwner},
		},
	}

	tuples := mapper.Resolve([]string{"Sales-Team", "Sales-Managers"})
	if len(tuples) != 2 {
		t.Fatalf("tuples = %d 件, want 2", len(tuples))
	}
}

func TestNewGroupMailboxPattern_ValidatesNamedGroups(t *testing.T) {
	if _, err := NewGroupMailboxPattern(`^mbx-(?P<mailbox>[\w.-]+)-(?P<role>member|owner|admin)$`, "example.com"); err != nil {
		t.Fatalf("正しい正規表現でエラー: %v", err)
	}
	if _, err := NewGroupMailboxPattern(`^mbx-([\w.-]+)-(member|owner|admin)$`, "example.com"); err == nil {
		t.Fatal("名前付きキャプチャグループが無い場合はエラーになるべき")
	}
	if _, err := NewGroupMailboxPattern(`(unclosed`, "example.com"); err == nil {
		t.Fatal("不正な正規表現はコンパイルエラーになるべき")
	}
}

func TestGroupMailboxMapper_Pattern(t *testing.T) {
	pattern, err := NewGroupMailboxPattern(`^mbx-(?P<mailbox>[\w.-]+)-(?P<role>member|owner|admin)$`, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	mapper := GroupMailboxMapper{Pattern: pattern}

	tuples := mapper.Resolve([]string{"mbx-sales-member", "mbx-hr-owner", "unrelated-group"})
	if len(tuples) != 2 {
		t.Fatalf("tuples = %d 件, want 2: %+v", len(tuples), tuples)
	}
	want := map[string]domain.AssignmentRole{
		"sales@example.com": domain.AssignmentRoleMember,
		"hr@example.com":    domain.AssignmentRoleOwner,
	}
	for _, tp := range tuples {
		wantRole, ok := want[tp.MailboxEmail]
		if !ok {
			t.Errorf("予期しない mailbox: %s", tp.MailboxEmail)
			continue
		}
		if tp.Role != wantRole {
			t.Errorf("%s の role = %q, want %q", tp.MailboxEmail, tp.Role, wantRole)
		}
	}
}

func TestGroupMailboxMapper_Pattern_NoDomainUsesCaptureAsIs(t *testing.T) {
	pattern, err := NewGroupMailboxPattern(`^mbx-(?P<mailbox>[^-]+@[\w.-]+)-(?P<role>member|owner|admin)$`, "")
	if err != nil {
		t.Fatal(err)
	}
	mapper := GroupMailboxMapper{Pattern: pattern}

	tuples := mapper.Resolve([]string{"mbx-sales@example.com-member"})
	if len(tuples) != 1 || tuples[0].MailboxEmail != "sales@example.com" {
		t.Errorf("tuples = %+v", tuples)
	}
}

func TestGroupMailboxMapper_Pattern_InvalidRoleIgnored(t *testing.T) {
	pattern, err := NewGroupMailboxPattern(`^mbx-(?P<mailbox>[\w.-]+)-(?P<role>[a-z]+)$`, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	mapper := GroupMailboxMapper{Pattern: pattern}

	// role が member/owner/admin のいずれでもない場合は無視される
	tuples := mapper.Resolve([]string{"mbx-sales-superuser"})
	if len(tuples) != 0 {
		t.Errorf("不正な role はタプルに含まれないべき: %+v", tuples)
	}
}

func TestGroupMailboxMapper_CombinedMappingsAndPattern(t *testing.T) {
	pattern, err := NewGroupMailboxPattern(`^mbx-(?P<mailbox>[\w.-]+)-(?P<role>member|owner|admin)$`, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	mapper := GroupMailboxMapper{
		Mappings: []GroupMailboxMapping{
			{Group: "Legacy-Support-Group", MailboxEmail: "support@example.com", Role: domain.AssignmentRoleMember},
		},
		Pattern: pattern,
	}

	tuples := mapper.Resolve([]string{"Legacy-Support-Group", "mbx-sales-owner"})
	if len(tuples) != 2 {
		t.Fatalf("tuples = %d 件, want 2: %+v", len(tuples), tuples)
	}
}

func TestGroupMailboxMapper_NoMatch(t *testing.T) {
	mapper := GroupMailboxMapper{}
	tuples := mapper.Resolve([]string{"some-group"})
	if len(tuples) != 0 {
		t.Errorf("マッピングが無い場合は空であるべき: %+v", tuples)
	}
}
