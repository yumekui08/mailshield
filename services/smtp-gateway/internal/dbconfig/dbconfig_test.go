package dbconfig

import (
	"context"
	"testing"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/policy"
)

type fakeInspect struct{ name string }

func (f fakeInspect) Name() string { return f.name }
func (f fakeInspect) Inspect(_ context.Context, _ *domain.Mail) (*domain.InspectResult, error) {
	return &domain.InspectResult{WorkerName: f.name, Detected: true, Score: 10}, nil
}

type fakeTransform struct{ name string }

func (f fakeTransform) Name() string { return f.name }
func (f fakeTransform) Transform(_ context.Context, m *domain.Mail) (*domain.Mail, error) {
	return m, nil
}

func testRegistry() Registry {
	return Registry{
		Inspect: map[string]InspectFactory{
			"av-worker": func(map[string]any) (domain.InspectWorker, error) { return fakeInspect{"av-worker"}, nil },
		},
		Transform: map[string]TransformFactory{
			"filesep-worker": func(map[string]any) (domain.TransformWorker, error) { return fakeTransform{"filesep-worker"}, nil },
		},
	}
}

func testPolicyFor(_ string) (*policy.Engine, error) { return policy.New("", nil) }

const sampleJSON = `{
  "variables": [{"key":"INTERNAL_DOMAIN","value":"@internal.test"}],
  "worker_instances": [
    {"alias":"av_internal","worker_type":"av-worker","kind":"inspect","config":{"threshold":50},"default_timeout_seconds":30,"is_enabled":true},
    {"alias":"fs_internal","worker_type":"filesep-worker","kind":"transform","is_enabled":true}
  ],
  "routings": [
    {"name":"inbound","priority":20,"match_expr":"mail.to ends_with ${INTERNAL_DOMAIN}","direction":"inbound","is_enabled":true,
     "inspect":[{"alias":"av_internal","enabled":true}],"transform":[{"alias":"fs_internal","enabled":true}]},
    {"name":"default","priority":1000000,"match_expr":"true","direction":"inbound","is_catchall":true,"is_enabled":true}
  ]
}`

func TestParseExpandBuildResolve(t *testing.T) {
	snap, err := Parse([]byte(sampleJSON))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	snap.Expand()
	// 変数展開: match_expr に値が入る
	if snap.Routings[0].MatchExpr != "mail.to ends_with @internal.test" {
		t.Errorf("変数展開されていない: %q", snap.Routings[0].MatchExpr)
	}

	routes, err := Build(snap, testRegistry(), testPolicyFor)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// 内部宛メールは inbound にマッチ
	inMail := &domain.Mail{ToAddresses: []string{"user@internal.test"}}
	rt, ok := routes.Resolve(inMail)
	if !ok || rt.Name != "inbound" {
		t.Fatalf("内部宛が inbound に解決されない: ok=%v rt=%v", ok, rt)
	}

	// 外部宛は catch-all（default）にフォールバック
	extMail := &domain.Mail{ToAddresses: []string{"user@example.com"}}
	rt2, ok := routes.Resolve(extMail)
	if !ok || rt2.Name != "default" {
		t.Fatalf("外部宛が catch-all に解決されない: ok=%v rt=%v", ok, rt2)
	}
}

func TestBuild_AliasKeyedResults(t *testing.T) {
	snap, _ := Parse([]byte(sampleJSON))
	snap.Expand()
	routes, err := Build(snap, testRegistry(), testPolicyFor)
	if err != nil {
		t.Fatal(err)
	}
	rt, _ := routes.Resolve(&domain.Mail{ToAddresses: []string{"u@internal.test"}})
	results, err := rt.Inspect.Run(context.Background(), &domain.Mail{ToAddresses: []string{"u@internal.test"}})
	if err != nil {
		t.Fatal(err)
	}
	// 検査結果は worker_type ではなく alias でキーされる
	if len(results) != 1 || results[0].WorkerName != "av_internal" {
		t.Errorf("結果が alias でキーされていない: %+v", results)
	}
}

func TestBuild_UnknownWorkerType(t *testing.T) {
	snap, _ := Parse([]byte(sampleJSON))
	snap.Expand()
	// 検査レジストリを空にすると未知型でエラー
	reg := Registry{Inspect: map[string]InspectFactory{}, Transform: testRegistry().Transform}
	if _, err := Build(snap, reg, testPolicyFor); err == nil {
		t.Error("未知のワーカー型でエラーになるべき")
	}
}
