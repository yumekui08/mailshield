package dbconfig

import (
	"context"
	"fmt"
	"time"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/pipeline"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/policy"
)

// CompiledRoute は 1 ルーティングを実行可能な形にしたもの。
type CompiledRoute struct {
	Name      string
	Direction string
	MatchExpr string
	Inspect   *pipeline.InspectPipeline
	Transform *pipeline.TransformPipeline
	Policy    *policy.Engine
}

// Routes は priority 昇順に並んだコンパイル済みルート集合。first-match で解決する。
type Routes struct {
	routes []CompiledRoute
}

// Resolve は最初に match したルートを返す（1 メール = 1 ルーティング）。
func (r *Routes) Resolve(mail *domain.Mail) (*CompiledRoute, bool) {
	for i := range r.routes {
		ok, err := policy.EvalMatch(r.routes[i].MatchExpr, mail)
		if err != nil {
			// 壊れた式は skip（publish 時に検証済みのため通常起きない）。
			continue
		}
		if ok {
			return &r.routes[i], true
		}
	}
	return nil, false
}

// InspectFactory は worker_type と型固有設定からインスタンスを生成する。
type InspectFactory func(config map[string]any) (domain.InspectWorker, error)

// TransformFactory は worker_type と型固有設定からインスタンスを生成する。
type TransformFactory func(config map[string]any) (domain.TransformWorker, error)

// Registry は worker_type → ファクトリの対応表。
type Registry struct {
	Inspect   map[string]InspectFactory
	Transform map[string]TransformFactory
}

// PolicyForFunc は policy_ref からポリシーエンジンを返す（policy_ref が空なら既定＝全配送）。
type PolicyForFunc func(policyRef string) (*policy.Engine, error)

// Build はスナップショットから実行可能なルート集合を構築する。
// 変数展開は呼び出し前に snap.Expand() を済ませておくこと。
func Build(snap *Snapshot, reg Registry, policyFor PolicyForFunc) (*Routes, error) {
	instByAlias := snap.instByAlias()
	var compiled []CompiledRoute

	for _, rt := range snap.sortedRoutings() {
		// 検査束ね（並列）
		var inspectEntries []domain.InspectEntry
		for _, b := range rt.Inspect {
			if !b.Enabled {
				continue
			}
			inst, ok := instByAlias[b.Alias]
			if !ok || !inst.IsEnabled {
				continue
			}
			factory, ok := reg.Inspect[inst.WorkerType]
			if !ok {
				return nil, fmt.Errorf("ルート %q: 未知の検査ワーカー型 %q（alias=%s）", rt.Name, inst.WorkerType, b.Alias)
			}
			worker, err := factory(inst.Config)
			if err != nil {
				return nil, fmt.Errorf("ルート %q: 検査ワーカー %q の生成失敗: %w", rt.Name, b.Alias, err)
			}
			inspectEntries = append(inspectEntries, domain.InspectEntry{
				// 結果は alias でキーする（ポリシー条件が alias を参照するため）。
				Worker:  aliasInspect{inner: worker, alias: b.Alias},
				Timeout: bindingTimeout(b, inst),
			})
		}

		// 変換束ね（定義順に直列）
		var transformWorkers []domain.TransformWorker
		for _, b := range rt.Transform {
			if !b.Enabled {
				continue
			}
			inst, ok := instByAlias[b.Alias]
			if !ok || !inst.IsEnabled {
				continue
			}
			factory, ok := reg.Transform[inst.WorkerType]
			if !ok {
				return nil, fmt.Errorf("ルート %q: 未知の変換ワーカー型 %q（alias=%s）", rt.Name, inst.WorkerType, b.Alias)
			}
			worker, err := factory(inst.Config)
			if err != nil {
				return nil, fmt.Errorf("ルート %q: 変換ワーカー %q の生成失敗: %w", rt.Name, b.Alias, err)
			}
			transformWorkers = append(transformWorkers, worker)
		}

		pe, err := policyFor(rt.PolicyRef)
		if err != nil {
			return nil, fmt.Errorf("ルート %q: ポリシー %q の構築失敗: %w", rt.Name, rt.PolicyRef, err)
		}

		compiled = append(compiled, CompiledRoute{
			Name:      rt.Name,
			Direction: rt.Direction,
			MatchExpr: rt.MatchExpr,
			Inspect:   pipeline.NewInspectPipeline(inspectEntries),
			Transform: pipeline.NewTransformPipeline(transformWorkers),
			Policy:    pe,
		})
	}
	return &Routes{routes: compiled}, nil
}

func bindingTimeout(b Binding, inst WorkerInstance) time.Duration {
	sec := inst.DefaultTimeoutSeconds
	if b.TimeoutSeconds != nil {
		sec = *b.TimeoutSeconds
	}
	if sec <= 0 {
		return 0
	}
	return time.Duration(sec) * time.Second
}

// aliasInspect は検査ワーカーをラップし、結果の WorkerName を alias に付け替える。
// 同じワーカー型から複数インスタンスを束ねても、ポリシーが alias で結果を区別できる。
type aliasInspect struct {
	inner domain.InspectWorker
	alias string
}

func (a aliasInspect) Name() string { return a.alias }

func (a aliasInspect) Inspect(ctx context.Context, mail *domain.Mail) (*domain.InspectResult, error) {
	res, err := a.inner.Inspect(ctx, mail)
	if err != nil {
		return nil, err
	}
	if res != nil {
		res.WorkerName = a.alias
	}
	return res, nil
}
