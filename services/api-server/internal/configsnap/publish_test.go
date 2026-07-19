package configsnap

import (
	"strings"
	"testing"

	"github.com/koizumib/mailshield/services/api-server/internal/domain"
)

func snap(vars []domain.ConfigVariable, insts []domain.WorkerInstance, rts []domain.Routing) *domain.ConfigSnapshot {
	return &domain.ConfigSnapshot{Variables: vars, WorkerInstances: insts, Routings: rts}
}

func catchAll() domain.Routing {
	return domain.Routing{Name: "default", MatchExpr: "true", IsCatchAll: true}
}

func TestValidate_OK(t *testing.T) {
	s := snap(
		[]domain.ConfigVariable{{Key: "INTERNAL_DOMAIN", Value: "@x.com"}},
		[]domain.WorkerInstance{
			{Alias: "av_internal", Kind: domain.WorkerKindInspect},
			{Alias: "fs_internal", Kind: domain.WorkerKindTransform},
		},
		[]domain.Routing{
			{Name: "inbound", MatchExpr: "mail.to endswith ${INTERNAL_DOMAIN}",
				Inspect:   []domain.WorkerBinding{{Alias: "av_internal", Enabled: true}},
				Transform: []domain.WorkerBinding{{Alias: "fs_internal", Enabled: true}}},
			catchAll(),
		},
	)
	if err := Validate(s); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidate_MissingCatchAll(t *testing.T) {
	s := snap(nil, nil, []domain.Routing{{Name: "inbound", MatchExpr: "true"}})
	if err := Validate(s); err == nil || !strings.Contains(err.Error(), "catch-all") {
		t.Errorf("catch-all 欠如が検出されない: %v", err)
	}
}

func TestValidate_TwoCatchAll(t *testing.T) {
	s := snap(nil, nil, []domain.Routing{catchAll(), catchAll()})
	if err := Validate(s); err == nil || !strings.Contains(err.Error(), "catch-all") {
		t.Errorf("catch-all 重複が検出されない: %v", err)
	}
}

func TestValidate_DanglingAlias(t *testing.T) {
	s := snap(nil, nil, []domain.Routing{
		{Name: "inbound", MatchExpr: "true", Inspect: []domain.WorkerBinding{{Alias: "ghost"}}},
		catchAll(),
	})
	if err := Validate(s); err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Errorf("未定義 alias が検出されない: %v", err)
	}
}

func TestValidate_KindMismatch(t *testing.T) {
	s := snap(nil,
		[]domain.WorkerInstance{{Alias: "fs_internal", Kind: domain.WorkerKindTransform}},
		[]domain.Routing{
			{Name: "inbound", MatchExpr: "true", Inspect: []domain.WorkerBinding{{Alias: "fs_internal"}}},
			catchAll(),
		},
	)
	if err := Validate(s); err == nil || !strings.Contains(err.Error(), "種別違い") {
		t.Errorf("種別不一致が検出されない: %v", err)
	}
}

func TestValidate_UndefinedVar(t *testing.T) {
	s := snap(nil, nil, []domain.Routing{
		{Name: "inbound", MatchExpr: "mail.to endswith ${MISSING}"},
		catchAll(),
	})
	if err := Validate(s); err == nil || !strings.Contains(err.Error(), "MISSING") {
		t.Errorf("未定義変数が検出されない: %v", err)
	}
}
