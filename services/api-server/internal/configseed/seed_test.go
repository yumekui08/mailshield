package configseed

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

// fakeRepo は ConfigRepository を埋め込み（未実装メソッドは呼ばれない前提）、
// Sync が使うエンティティ操作だけをインメモリで実装する。
type fakeRepo struct {
	repository.ConfigRepository
	insts map[string]*domain.WorkerInstance
	vars  map[string]*domain.ConfigVariable
	rts   map[string]*domain.Routing
	seq   int
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		insts: map[string]*domain.WorkerInstance{},
		vars:  map[string]*domain.ConfigVariable{},
		rts:   map[string]*domain.Routing{},
	}
}

func (f *fakeRepo) id() string { f.seq++; return "id-" + string(rune('a'+f.seq)) }

func (f *fakeRepo) ListWorkerInstances(context.Context) ([]domain.WorkerInstance, error) {
	out := []domain.WorkerInstance{}
	for _, v := range f.insts {
		out = append(out, *v)
	}
	return out, nil
}
func (f *fakeRepo) CreateWorkerInstance(_ context.Context, w *domain.WorkerInstance) error {
	w.ID = f.id()
	f.insts[w.ID] = w
	return nil
}
func (f *fakeRepo) UpdateWorkerInstance(_ context.Context, w *domain.WorkerInstance) error {
	f.insts[w.ID] = w
	return nil
}
func (f *fakeRepo) DeleteWorkerInstance(_ context.Context, id string) error {
	delete(f.insts, id)
	return nil
}
func (f *fakeRepo) ListConfigVariables(context.Context) ([]domain.ConfigVariable, error) {
	out := []domain.ConfigVariable{}
	for _, v := range f.vars {
		out = append(out, *v)
	}
	return out, nil
}
func (f *fakeRepo) CreateConfigVariable(_ context.Context, v *domain.ConfigVariable) error {
	v.ID = f.id()
	f.vars[v.ID] = v
	return nil
}
func (f *fakeRepo) UpdateConfigVariable(_ context.Context, v *domain.ConfigVariable) error {
	f.vars[v.ID] = v
	return nil
}
func (f *fakeRepo) DeleteConfigVariable(_ context.Context, id string) error {
	delete(f.vars, id)
	return nil
}
func (f *fakeRepo) ListRoutings(context.Context) ([]domain.Routing, error) {
	out := []domain.Routing{}
	for _, v := range f.rts {
		out = append(out, *v)
	}
	return out, nil
}
func (f *fakeRepo) CreateRouting(_ context.Context, rt *domain.Routing) error {
	rt.ID = f.id()
	f.rts[rt.ID] = rt
	return nil
}
func (f *fakeRepo) UpdateRouting(_ context.Context, rt *domain.Routing) error {
	f.rts[rt.ID] = rt
	return nil
}
func (f *fakeRepo) DeleteRouting(_ context.Context, id string) error {
	delete(f.rts, id)
	return nil
}

func doc(kind, name string, spec map[string]any) Doc {
	b, _ := json.Marshal(spec)
	return Doc{Kind: kind, Name: name, Spec: b}
}

func TestSync_UpsertThenPrune(t *testing.T) {
	f := newFakeRepo()
	ctx := context.Background()

	// 初回: 2 インスタンス + 1 変数を作成
	docs := []Doc{
		doc(KindWorkerInstance, "av_internal", map[string]any{"worker_type": "av-worker", "kind": "inspect"}),
		doc(KindWorkerInstance, "fs_internal", map[string]any{"worker_type": "filesep-worker", "kind": "transform"}),
		doc(KindConfigVariable, "INTERNAL_DOMAIN", map[string]any{"value": "@x.com"}),
	}
	res := Sync(ctx, f, docs, true)
	if res.Created != 3 || len(res.Errors) != 0 {
		t.Fatalf("初回 created=%d errors=%v", res.Created, res.Errors)
	}

	// 2 回目: av_internal だけ残し fs_internal と変数を prune、av_internal は更新
	docs2 := []Doc{
		doc(KindWorkerInstance, "av_internal", map[string]any{"worker_type": "av-worker", "kind": "inspect", "display_name": "更新"}),
	}
	res2 := Sync(ctx, f, docs2, true)
	if res2.Updated != 1 {
		t.Errorf("av_internal は更新されるべき: %+v", res2)
	}
	if res2.Deleted != 2 {
		t.Errorf("fs_internal と変数が prune されるべき: deleted=%d", res2.Deleted)
	}
	if len(f.insts) != 1 || len(f.vars) != 0 {
		t.Errorf("prune 後の状態が不正: insts=%d vars=%d", len(f.insts), len(f.vars))
	}
}

func TestSync_NoPruneKeepsExisting(t *testing.T) {
	f := newFakeRepo()
	ctx := context.Background()
	Sync(ctx, f, []Doc{doc(KindConfigVariable, "A", map[string]any{"value": "1"})}, false)
	// prune=false なら別 doc を入れても既存は消えない
	Sync(ctx, f, []Doc{doc(KindConfigVariable, "B", map[string]any{"value": "2"})}, false)
	if len(f.vars) != 2 {
		t.Errorf("prune=false なら両方残るべき: %d", len(f.vars))
	}
}
