// Package configseed はマニフェスト・バンドル（{kind,name,spec} の配列）を設定エンティティへ
// 同期する共通ロジック（ADR 008）。WebUI のインポート（upsert のみ）と、file モードの
// 起動時 seed（upsert＋prune）の両方から使う。
package configseed

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/koizumib/mailshield/services/api-server/internal/configsnap"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

const (
	KindWorkerInstance = "WorkerInstance"
	KindConfigVariable = "ConfigVariable"
	KindRouting        = "Routing"
	BundleVersion      = "mailshield.config/v1"
)

// Doc はバンドルの 1 ドキュメント。Name は WorkerInstance=alias / ConfigVariable=key / Routing=name。
type Doc struct {
	Kind string          `json:"kind"`
	Name string          `json:"name"`
	Spec json.RawMessage `json:"spec"`
}

// Bundle はドキュメントの配列。
type Bundle struct {
	Version           string   `json:"version"`
	Docs              []Doc    `json:"docs"`
	RequiresVariables []string `json:"requires_variables,omitempty"`
}

// Result は同期結果。
type Result struct {
	Created int      `json:"created"`
	Updated int      `json:"updated"`
	Deleted int      `json:"deleted"`
	Errors  []string `json:"errors"`
}

var (
	aliasPattern  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	varKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// Sync は docs を設定エンティティへ upsert する。prune=true なら、docs に含まれない
// エンティティを削除する（file モードの「ファイルが真実」を実現。catch-all は保護）。
// (kind, name) を自然キーとして冪等。ID は既存を保持して参照を壊さない。
func Sync(ctx context.Context, repo repository.ConfigRepository, docs []Doc, prune bool) Result {
	res := Result{Errors: []string{}}

	instByAlias := map[string]domain.WorkerInstance{}
	if list, err := repo.ListWorkerInstances(ctx); err == nil {
		for _, in := range list {
			instByAlias[in.Alias] = in
		}
	}
	varByKey := map[string]domain.ConfigVariable{}
	if list, err := repo.ListConfigVariables(ctx); err == nil {
		for _, v := range list {
			varByKey[v.Key] = v
		}
	}
	rtByName := map[string]domain.Routing{}
	if list, err := repo.ListRoutings(ctx); err == nil {
		for _, rt := range list {
			if !rt.IsCatchAll {
				rtByName[rt.Name] = rt
			}
		}
	}

	// docs に含まれる自然キー（prune 判定用）
	seenInst, seenVar, seenRt := map[string]bool{}, map[string]bool{}, map[string]bool{}

	for _, doc := range docs {
		switch doc.Kind {
		case KindWorkerInstance:
			seenInst[doc.Name] = true
			upsertWorkerInstance(ctx, repo, doc, instByAlias, &res)
		case KindConfigVariable:
			seenVar[doc.Name] = true
			upsertVariable(ctx, repo, doc, varByKey, &res)
		case KindRouting:
			seenRt[doc.Name] = true
			upsertRouting(ctx, repo, doc, rtByName, &res)
		default:
			res.Errors = append(res.Errors, "未知の kind: "+doc.Kind)
		}
	}

	if prune {
		for alias, in := range instByAlias {
			if !seenInst[alias] {
				if err := repo.DeleteWorkerInstance(ctx, in.ID); err == nil {
					res.Deleted++
				}
			}
		}
		for key, v := range varByKey {
			if !seenVar[key] {
				if err := repo.DeleteConfigVariable(ctx, v.ID); err == nil {
					res.Deleted++
				}
			}
		}
		for name, rt := range rtByName {
			if !seenRt[name] {
				if err := repo.DeleteRouting(ctx, rt.ID); err == nil {
					res.Deleted++
				}
			}
		}
	}
	return res
}

func upsertWorkerInstance(ctx context.Context, repo repository.ConfigRepository, doc Doc, existing map[string]domain.WorkerInstance, res *Result) {
	var s struct {
		DisplayName           string            `json:"display_name"`
		WorkerType            string            `json:"worker_type"`
		Kind                  domain.WorkerKind `json:"kind"`
		Config                map[string]any    `json:"config"`
		DefaultTimeoutSeconds int               `json:"default_timeout_seconds"`
		IsEnabled             bool              `json:"is_enabled"`
	}
	if err := json.Unmarshal(doc.Spec, &s); err != nil {
		res.Errors = append(res.Errors, "WorkerInstance "+doc.Name+": spec 不正")
		return
	}
	if !aliasPattern.MatchString(doc.Name) {
		res.Errors = append(res.Errors, "WorkerInstance "+doc.Name+": alias 不正")
		return
	}
	in := domain.WorkerInstance{
		Alias: doc.Name, DisplayName: s.DisplayName, WorkerType: s.WorkerType, Kind: s.Kind,
		Config: s.Config, DefaultTimeoutSeconds: s.DefaultTimeoutSeconds, IsEnabled: s.IsEnabled,
	}
	if in.Config == nil {
		in.Config = map[string]any{}
	}
	if cur, ok := existing[doc.Name]; ok {
		in.ID = cur.ID
		if err := repo.UpdateWorkerInstance(ctx, &in); err != nil {
			res.Errors = append(res.Errors, "WorkerInstance "+doc.Name+": "+err.Error())
			return
		}
		res.Updated++
	} else {
		if err := repo.CreateWorkerInstance(ctx, &in); err != nil {
			res.Errors = append(res.Errors, "WorkerInstance "+doc.Name+": "+err.Error())
			return
		}
		res.Created++
	}
}

func upsertVariable(ctx context.Context, repo repository.ConfigRepository, doc Doc, existing map[string]domain.ConfigVariable, res *Result) {
	var s struct {
		Value       string `json:"value"`
		Description string `json:"description"`
	}
	_ = json.Unmarshal(doc.Spec, &s)
	if !varKeyPattern.MatchString(doc.Name) {
		res.Errors = append(res.Errors, "ConfigVariable "+doc.Name+": key 不正")
		return
	}
	v := domain.ConfigVariable{Key: doc.Name, Value: s.Value, Description: s.Description}
	if cur, ok := existing[doc.Name]; ok {
		v.ID = cur.ID
		if err := repo.UpdateConfigVariable(ctx, &v); err != nil {
			res.Errors = append(res.Errors, "ConfigVariable "+doc.Name+": "+err.Error())
			return
		}
		res.Updated++
	} else {
		if err := repo.CreateConfigVariable(ctx, &v); err != nil {
			res.Errors = append(res.Errors, "ConfigVariable "+doc.Name+": "+err.Error())
			return
		}
		res.Created++
	}
}

func upsertRouting(ctx context.Context, repo repository.ConfigRepository, doc Doc, existing map[string]domain.Routing, res *Result) {
	var s struct {
		Priority  int                    `json:"priority"`
		MatchExpr string                 `json:"match_expr"`
		Direction string                 `json:"direction"`
		IsEnabled bool                   `json:"is_enabled"`
		PolicyRef string                 `json:"policy_ref"`
		Inspect   []domain.WorkerBinding `json:"inspect"`
		Transform []domain.WorkerBinding `json:"transform"`
	}
	if err := json.Unmarshal(doc.Spec, &s); err != nil {
		res.Errors = append(res.Errors, "Routing "+doc.Name+": spec 不正")
		return
	}
	if strings.TrimSpace(s.MatchExpr) == "" {
		res.Errors = append(res.Errors, "Routing "+doc.Name+": match_expr 必須")
		return
	}
	if s.Direction == "" {
		s.Direction = "inbound"
	}
	rt := domain.Routing{
		Name: doc.Name, Priority: s.Priority, MatchExpr: s.MatchExpr, Direction: s.Direction, IsEnabled: s.IsEnabled,
		PolicyRef: s.PolicyRef, Inspect: s.Inspect, Transform: s.Transform,
	}
	if cur, ok := existing[doc.Name]; ok {
		rt.ID = cur.ID
		if err := repo.UpdateRouting(ctx, &rt); err != nil {
			res.Errors = append(res.Errors, "Routing "+doc.Name+": "+err.Error())
			return
		}
		res.Updated++
	} else {
		if err := repo.CreateRouting(ctx, &rt); err != nil {
			res.Errors = append(res.Errors, "Routing "+doc.Name+": "+err.Error())
			return
		}
		res.Created++
	}
}

// ParseDir は dir 直下の *.json バンドルファイルを読み、docs を結合して返す。
// ファイルが見つからない/dir が無い場合は空バンドルを返す（エラーにしない）。
func ParseDir(dir string) (Bundle, error) {
	merged := Bundle{Version: BundleVersion}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return merged, nil
		}
		return merged, fmt.Errorf("設定バンドルディレクトリ読み込み失敗 (%s): %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return merged, fmt.Errorf("バンドルファイル読み込み失敗 (%s): %w", name, err)
		}
		var b Bundle
		if err := json.Unmarshal(data, &b); err != nil {
			return merged, fmt.Errorf("バンドルファイル JSON パース失敗 (%s): %w", name, err)
		}
		merged.Docs = append(merged.Docs, b.Docs...)
	}
	return merged, nil
}

// SeedFromFiles は file モードの起動時 seed を単一実行者で行う（ADR 008 ③-2）。
// GET_LOCK で 1 レプリカだけが seed し、他は何もしない。ファイルが真実なので prune=true。
// seed 後に catch-all を保証し、検証済みスナップショットを publish（source="file"）する。
func SeedFromFiles(ctx context.Context, repo repository.ConfigRepository, dir string, pub *configsnap.Publisher) (Result, error) {
	const lockName = "mailshield-seed"
	got, err := repo.TryAcquireSeedLock(ctx, lockName, 30)
	if err != nil {
		return Result{}, err
	}
	if !got {
		// 別レプリカが seed 中。そちらが publish するので何もしない。
		return Result{}, nil
	}
	defer func() { _ = repo.ReleaseSeedLock(ctx, lockName) }()

	bundle, err := ParseDir(dir)
	if err != nil {
		return Result{}, err
	}
	res := Sync(ctx, repo, bundle.Docs, true)

	// catch-all を保証（ファイルには含めない運用のため、無ければ作る）。
	if n, err := repo.CountCatchAllRoutings(ctx); err == nil && n == 0 {
		_ = repo.CreateRouting(ctx, &domain.Routing{
			Name: "デフォルト（すべてに一致）", Priority: 1_000_000, MatchExpr: "true",
			IsCatchAll: true, IsEnabled: true,
			Inspect: []domain.WorkerBinding{}, Transform: []domain.WorkerBinding{},
		})
	}
	if _, err := pub.Publish(ctx, "file", "seed"); err != nil {
		return res, fmt.Errorf("seed 後の publish 失敗: %w", err)
	}
	return res, nil
}
