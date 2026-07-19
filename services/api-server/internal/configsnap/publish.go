// Package configsnap は設定エンティティを canonical スナップショットに固め、検証し、
// バージョンとして publish する（ADR 008 ③-2）。api-server 側の「設定オーナー」責務。
package configsnap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"

	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

// Publisher は現在の設定エンティティからスナップショットを組み立て、検証・publish する。
type Publisher struct {
	repo repository.ConfigRepository
}

func NewPublisher(repo repository.ConfigRepository) *Publisher {
	return &Publisher{repo: repo}
}

// Assemble は現在のエンティティを canonical スナップショットに束ねる。
// 各リストは repo 側で安定順序（変数=key / インスタンス=kind,alias / ルーティング=priority）に
// 並んでいるため、JSON 化した結果は決定的でチェックサムが安定する。
func (p *Publisher) Assemble(ctx context.Context) (*domain.ConfigSnapshot, error) {
	vars, err := p.repo.ListConfigVariables(ctx)
	if err != nil {
		return nil, err
	}
	insts, err := p.repo.ListWorkerInstances(ctx)
	if err != nil {
		return nil, err
	}
	policies, err := p.repo.ListPolicyInstances(ctx)
	if err != nil {
		return nil, err
	}
	rts, err := p.repo.ListRoutings(ctx)
	if err != nil {
		return nil, err
	}
	return &domain.ConfigSnapshot{Variables: vars, WorkerInstances: insts, Policies: policies, Routings: rts}, nil
}

// Publish はスナップショットを組み立て・検証し、現アクティブ版と内容が異なる場合のみ
// 新しい版を保存してアクティブ版ポインタを切り替える。changed はポインタを更新したか。
// 内容が同一なら版を作らない（再起動ごとの版量産を防ぐ）。
func (p *Publisher) Publish(ctx context.Context, source, author string) (changed bool, err error) {
	snap, err := p.Assemble(ctx)
	if err != nil {
		return false, err
	}
	if err := Validate(snap); err != nil {
		return false, fmt.Errorf("設定検証失敗: %w", err)
	}
	content, err := json.Marshal(snap)
	if err != nil {
		return false, fmt.Errorf("スナップショット JSON 変換失敗: %w", err)
	}
	sum := sha256.Sum256(content)
	checksum := hex.EncodeToString(sum[:])

	active, err := p.repo.GetActiveConfigChecksum(ctx)
	if err != nil {
		return false, err
	}
	if active == checksum {
		return false, nil // 内容不変：新版を作らない
	}

	ver := &domain.ConfigVersion{Checksum: checksum, Source: source, Author: author, Content: string(content)}
	if err := p.repo.SaveConfigVersion(ctx, ver); err != nil {
		return false, err
	}
	if err := p.repo.SetActiveConfigVersion(ctx, ver.ID); err != nil {
		return false, err
	}
	return true, nil
}

var varRefPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Validate はスナップショット全体の整合性を検証する（publish 前に必ず通す）。
//   - catch-all ルーティングが 1 つだけ存在する
//   - ルーティングが束ねる alias が実在するワーカーインスタンスを指す
//   - match_expr / ワーカー設定から参照される ${VAR} がすべて定義済み変数である
func Validate(snap *domain.ConfigSnapshot) error {
	instByAlias := map[string]domain.WorkerInstance{}
	for _, in := range snap.WorkerInstances {
		instByAlias[in.Alias] = in
	}
	varSet := map[string]bool{}
	for _, v := range snap.Variables {
		varSet[v.Key] = true
	}
	policySet := map[string]bool{}
	for _, p := range snap.Policies {
		policySet[p.Alias] = true
	}

	// ルーティングはすべてデータ。空状態も正当（catch-all は必須ではない）。
	for _, rt := range snap.Routings {
		if err := validateBindings(rt, rt.Inspect, domain.WorkerKindInspect, instByAlias); err != nil {
			return err
		}
		if err := validateBindings(rt, rt.Transform, domain.WorkerKindTransform, instByAlias); err != nil {
			return err
		}
		if rt.PolicyRef != "" && !policySet[rt.PolicyRef] {
			return fmt.Errorf("ルーティング %q が未定義のポリシー %q を参照しています", routingLabel(rt), rt.PolicyRef)
		}
		for v := range collectVarRefs(rt.MatchExpr) {
			if !varSet[v] {
				return fmt.Errorf("ルーティング %q の match_expr が未定義の変数 ${%s} を参照しています", routingLabel(rt), v)
			}
		}
	}

	for _, in := range snap.WorkerInstances {
		cfgBytes, _ := json.Marshal(in.Config)
		for v := range collectVarRefs(string(cfgBytes)) {
			if !varSet[v] {
				return fmt.Errorf("ワーカーインスタンス %q の設定が未定義の変数 ${%s} を参照しています", in.Alias, v)
			}
		}
	}
	return nil
}

func routingLabel(rt domain.Routing) string {
	if rt.Name != "" {
		return rt.Name
	}
	return rt.ID
}

// validateBindings は束ねの alias が実在し、種別（inspect/transform）が一致することを検証する。
func validateBindings(rt domain.Routing, bindings []domain.WorkerBinding, want domain.WorkerKind, instByAlias map[string]domain.WorkerInstance) error {
	for _, b := range bindings {
		inst, ok := instByAlias[b.Alias]
		if !ok {
			return fmt.Errorf("ルーティング %q が未定義のワーカーインスタンス %q を参照しています", routingLabel(rt), b.Alias)
		}
		if inst.Kind != want {
			return fmt.Errorf("ルーティング %q の %s 束ねに種別違いのワーカー %q（%s）が指定されています",
				routingLabel(rt), want, b.Alias, inst.Kind)
		}
	}
	return nil
}

func collectVarRefs(s string) map[string]bool {
	out := map[string]bool{}
	for _, m := range varRefPattern.FindAllStringSubmatch(s, -1) {
		out[m[1]] = true
	}
	return out
}

// VarNames はスナップショットが参照する全変数名（ソート済み）を返す（デバッグ・表示用）。
func VarNames(snap *domain.ConfigSnapshot) []string {
	set := map[string]bool{}
	for _, rt := range snap.Routings {
		for v := range collectVarRefs(rt.MatchExpr) {
			set[v] = true
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
