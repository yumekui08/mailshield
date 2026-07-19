package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

// TestHandleList_ParsesFilters は GET /mailboxes のクエリパラメータが
// MailboxSearchFilter に正しく変換され、meta にページング情報が載ることを確認する。
func TestHandleList_ParsesFilters(t *testing.T) {
	var got repository.MailboxSearchFilter
	mock := &mockRepository{
		searchMailboxesFunc: func(_ context.Context, f repository.MailboxSearchFilter) ([]repository.Mailbox, int, error) {
			got = f
			return []repository.Mailbox{{ID: "mb1", EmailAddress: "sales@x.dev", IsActive: true}}, 7, nil
		},
	}
	h := NewMailboxesHandler(mock, testAuditLogger)

	req := httptest.NewRequest("GET",
		"/api/v1/mailboxes?q=sales&provisioned_by=ldap&active=false&missing_role=approver&assigned_user_id=u1&limit=20&offset=40",
		nil)
	rec := httptest.NewRecorder()
	h.HandleList(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got.Query != "sales" {
		t.Errorf("Query = %q", got.Query)
	}
	if got.ProvisionedBy != domain.ProvisionedByLDAP {
		t.Errorf("ProvisionedBy = %q", got.ProvisionedBy)
	}
	if got.Active == nil || *got.Active {
		t.Errorf("Active = %v, want false", got.Active)
	}
	if got.MissingRole != domain.AssignmentRoleApprover {
		t.Errorf("MissingRole = %q", got.MissingRole)
	}
	if got.AssignedUserID != "u1" {
		t.Errorf("AssignedUserID = %q", got.AssignedUserID)
	}
	if got.Limit != 20 || got.Offset != 40 {
		t.Errorf("Limit/Offset = %d/%d", got.Limit, got.Offset)
	}

	var resp struct {
		Data []mailboxResponse `json:"data"`
		Meta struct {
			Total  int `json:"total"`
			Limit  int `json:"limit"`
			Offset int `json:"offset"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Meta.Total != 7 || resp.Meta.Limit != 20 || resp.Meta.Offset != 40 {
		t.Errorf("meta = %+v", resp.Meta)
	}
	if len(resp.Data) != 1 || resp.Data[0].EmailAddress != "sales@x.dev" {
		t.Errorf("data = %+v", resp.Data)
	}
}

// TestHandleList_IgnoresInvalidEnums は不正な列挙値・数値を無視して既定にフォールバックすることを確認する。
func TestHandleList_IgnoresInvalidEnums(t *testing.T) {
	var got repository.MailboxSearchFilter
	mock := &mockRepository{
		searchMailboxesFunc: func(_ context.Context, f repository.MailboxSearchFilter) ([]repository.Mailbox, int, error) {
			got = f
			return []repository.Mailbox{}, 0, nil
		},
	}
	h := NewMailboxesHandler(mock, testAuditLogger)

	req := httptest.NewRequest("GET",
		"/api/v1/mailboxes?provisioned_by=bogus&missing_role=superuser&active=maybe&limit=abc", nil)
	rec := httptest.NewRecorder()
	h.HandleList(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if got.ProvisionedBy != "" {
		t.Errorf("不正な provisioned_by が渡った: %q", got.ProvisionedBy)
	}
	if got.MissingRole != "" {
		t.Errorf("不正な missing_role が渡った: %q", got.MissingRole)
	}
	if got.Active != nil {
		t.Errorf("不正な active が渡った: %v", got.Active)
	}
	if got.Limit != 50 {
		t.Errorf("不正な limit のフォールバックが 50 でない: %d", got.Limit)
	}
}
