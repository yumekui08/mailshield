package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/koizumib/mailshield/services/api-server/internal/domain"
)

// ADR 008 のインポート/エクスポート: k8s マニフェスト風バンドル。
// すべてのエンティティを {kind, name, spec} の 1 ドキュメントで表し、参照は名前で行う。
// バンドル = ドキュメントの配列。含めるドキュメントを選ぶだけで粒度は自由。

const (
	kindWorkerInstance = "WorkerInstance"
	kindConfigVariable = "ConfigVariable"
	kindRouting        = "Routing"
	bundleVersion      = "mailshield.config/v1"
)

type manifestDoc struct {
	Kind string          `json:"kind"`
	Name string          `json:"name"` // WorkerInstance=alias / ConfigVariable=key / Routing=name
	Spec json.RawMessage `json:"spec"`
}

type configBundle struct {
	Version string        `json:"version"`
	Docs    []manifestDoc `json:"docs"`
	// RequiresVariables はバンドル内から ${VAR} 参照されている変数名（インポート先で値を用意する目印）。
	RequiresVariables []string `json:"requires_variables,omitempty"`
}

var varRefPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// HandleExportBundle は GET /api/v1/config/export?kinds=... を処理する。
// kinds 未指定なら全種別。catch-all ルーティングは環境ごとに自動生成されるため含めない。
func (h *ConfigHandler) HandleExportBundle(w http.ResponseWriter, r *http.Request) {
	want := parseKinds(r.URL.Query().Get("kinds"))
	var docs []manifestDoc
	varRefs := map[string]bool{}

	if want[kindWorkerInstance] {
		insts, err := h.repo.ListWorkerInstances(r.Context())
		if err != nil {
			writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "取得に失敗しました")
			return
		}
		for _, in := range insts {
			spec, _ := json.Marshal(map[string]any{
				"display_name": in.DisplayName, "worker_type": in.WorkerType, "kind": in.Kind,
				"config": in.Config, "default_timeout_seconds": in.DefaultTimeoutSeconds,
				"is_enabled": in.IsEnabled,
			})
			collectVarRefs(spec, varRefs)
			docs = append(docs, manifestDoc{Kind: kindWorkerInstance, Name: in.Alias, Spec: spec})
		}
	}
	if want[kindConfigVariable] {
		vars, err := h.repo.ListConfigVariables(r.Context())
		if err != nil {
			writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "取得に失敗しました")
			return
		}
		for _, v := range vars {
			spec, _ := json.Marshal(map[string]any{"value": v.Value, "description": v.Description})
			docs = append(docs, manifestDoc{Kind: kindConfigVariable, Name: v.Key, Spec: spec})
		}
	}
	if want[kindRouting] {
		rts, err := h.repo.ListRoutings(r.Context())
		if err != nil {
			writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "取得に失敗しました")
			return
		}
		for _, rt := range rts {
			if rt.IsCatchAll {
				continue // catch-all は各環境で自動生成されるため出力しない
			}
			spec, _ := json.Marshal(map[string]any{
				"priority": rt.Priority, "match_expr": rt.MatchExpr, "is_enabled": rt.IsEnabled,
				"policy_ref": rt.PolicyRef, "inspect": rt.Inspect, "transform": rt.Transform,
			})
			collectVarRefs(spec, varRefs)
			docs = append(docs, manifestDoc{Kind: kindRouting, Name: rt.Name, Spec: spec})
		}
	}

	bundle := configBundle{Version: bundleVersion, Docs: docs, RequiresVariables: sortedKeys(varRefs)}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="mailshield-config.json"`)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(bundle)
}

// importResult はインポート結果（種別ごとの作成/更新件数とエラー）を表す。
type importResult struct {
	Created int      `json:"created"`
	Updated int      `json:"updated"`
	Errors  []string `json:"errors"`
}

// HandleImportBundle は POST /api/v1/config/import を処理する。
// (kind, name) で冪等 upsert する。
func (h *ConfigHandler) HandleImportBundle(w http.ResponseWriter, r *http.Request) {
	var bundle configBundle
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&bundle); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "バンドルの解析に失敗しました")
		return
	}

	res := importResult{Errors: []string{}}
	// 既存を自然キーで引けるように索引化する。
	instByAlias := map[string]domain.WorkerInstance{}
	if list, err := h.repo.ListWorkerInstances(r.Context()); err == nil {
		for _, in := range list {
			instByAlias[in.Alias] = in
		}
	}
	varByKey := map[string]domain.ConfigVariable{}
	if list, err := h.repo.ListConfigVariables(r.Context()); err == nil {
		for _, v := range list {
			varByKey[v.Key] = v
		}
	}
	rtByName := map[string]domain.Routing{}
	if list, err := h.repo.ListRoutings(r.Context()); err == nil {
		for _, rt := range list {
			if !rt.IsCatchAll {
				rtByName[rt.Name] = rt
			}
		}
	}

	for _, doc := range bundle.Docs {
		switch doc.Kind {
		case kindWorkerInstance:
			h.importWorkerInstance(r, doc, instByAlias, &res)
		case kindConfigVariable:
			h.importVariable(r, doc, varByKey, &res)
		case kindRouting:
			h.importRouting(r, doc, rtByName, &res)
		default:
			res.Errors = append(res.Errors, "未知の kind: "+doc.Kind)
		}
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *ConfigHandler) importWorkerInstance(r *http.Request, doc manifestDoc, existing map[string]domain.WorkerInstance, res *importResult) {
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
	inst := domain.WorkerInstance{
		Alias: doc.Name, DisplayName: s.DisplayName, WorkerType: s.WorkerType, Kind: s.Kind,
		Config: s.Config, DefaultTimeoutSeconds: s.DefaultTimeoutSeconds, IsEnabled: s.IsEnabled,
	}
	if inst.Config == nil {
		inst.Config = map[string]any{}
	}
	if cur, ok := existing[doc.Name]; ok {
		inst.ID = cur.ID
		if err := h.repo.UpdateWorkerInstance(r.Context(), &inst); err != nil {
			res.Errors = append(res.Errors, "WorkerInstance "+doc.Name+": "+err.Error())
			return
		}
		res.Updated++
	} else {
		if err := h.repo.CreateWorkerInstance(r.Context(), &inst); err != nil {
			res.Errors = append(res.Errors, "WorkerInstance "+doc.Name+": "+err.Error())
			return
		}
		res.Created++
	}
}

func (h *ConfigHandler) importVariable(r *http.Request, doc manifestDoc, existing map[string]domain.ConfigVariable, res *importResult) {
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
		if err := h.repo.UpdateConfigVariable(r.Context(), &v); err != nil {
			res.Errors = append(res.Errors, "ConfigVariable "+doc.Name+": "+err.Error())
			return
		}
		res.Updated++
	} else {
		if err := h.repo.CreateConfigVariable(r.Context(), &v); err != nil {
			res.Errors = append(res.Errors, "ConfigVariable "+doc.Name+": "+err.Error())
			return
		}
		res.Created++
	}
}

func (h *ConfigHandler) importRouting(r *http.Request, doc manifestDoc, existing map[string]domain.Routing, res *importResult) {
	var s struct {
		Priority  int                    `json:"priority"`
		MatchExpr string                 `json:"match_expr"`
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
	rt := domain.Routing{
		Name: doc.Name, Priority: s.Priority, MatchExpr: s.MatchExpr, IsEnabled: s.IsEnabled,
		PolicyRef: s.PolicyRef, Inspect: s.Inspect, Transform: s.Transform,
	}
	if cur, ok := existing[doc.Name]; ok {
		rt.ID = cur.ID
		if err := h.repo.UpdateRouting(r.Context(), &rt); err != nil {
			res.Errors = append(res.Errors, "Routing "+doc.Name+": "+err.Error())
			return
		}
		res.Updated++
	} else {
		if err := h.repo.CreateRouting(r.Context(), &rt); err != nil {
			res.Errors = append(res.Errors, "Routing "+doc.Name+": "+err.Error())
			return
		}
		res.Created++
	}
}

func parseKinds(raw string) map[string]bool {
	if strings.TrimSpace(raw) == "" {
		return map[string]bool{kindWorkerInstance: true, kindConfigVariable: true, kindRouting: true}
	}
	alias := map[string]string{
		"worker_instance": kindWorkerInstance, "workerinstance": kindWorkerInstance,
		"variable": kindConfigVariable, "configvariable": kindConfigVariable,
		"routing": kindRouting,
	}
	out := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		if k, ok := alias[strings.ToLower(strings.TrimSpace(part))]; ok {
			out[k] = true
		}
	}
	return out
}

func collectVarRefs(b []byte, into map[string]bool) {
	for _, m := range varRefPattern.FindAllSubmatch(b, -1) {
		into[string(m[1])] = true
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
