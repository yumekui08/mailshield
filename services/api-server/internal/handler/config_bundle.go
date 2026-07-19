package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/koizumib/mailshield/services/api-server/internal/configseed"
)

// ADR 008 のインポート/エクスポート: k8s マニフェスト風バンドル。
// すべてのエンティティを {kind, name, spec} の 1 ドキュメントで表し、参照は名前で行う。
// バンドル = ドキュメントの配列。含めるドキュメントを選ぶだけで粒度は自由。

const (
	kindWorkerInstance = "WorkerInstance"
	kindConfigVariable = "ConfigVariable"
	kindPolicyInstance = "PolicyInstance"
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
	if want[kindPolicyInstance] {
		pols, err := h.repo.ListPolicyInstances(r.Context())
		if err != nil {
			writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "取得に失敗しました")
			return
		}
		for _, p := range pols {
			spec, _ := json.Marshal(map[string]any{"display_name": p.DisplayName, "content": p.Content})
			docs = append(docs, manifestDoc{Kind: kindPolicyInstance, Name: p.Alias, Spec: spec})
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
				"priority": rt.Priority, "match_expr": rt.MatchExpr, "direction": rt.Direction,
				"is_enabled": rt.IsEnabled, "policy_ref": rt.PolicyRef,
				"inspect": rt.Inspect, "transform": rt.Transform,
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

// HandleImportBundle は POST /api/v1/config/import を処理する。
// (kind, name) で冪等 upsert する（削除はしない）。共通ロジックは configseed に委譲。
func (h *ConfigHandler) HandleImportBundle(w http.ResponseWriter, r *http.Request) {
	var bundle configseed.Bundle
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&bundle); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "バンドルの解析に失敗しました")
		return
	}
	res := configseed.Sync(r.Context(), h.repo, bundle.Docs, false)
	// インポート後にスナップショットを再 publish（アクティブ版を更新）。
	h.publish(r)
	writeJSON(w, http.StatusOK, res)
}

func parseKinds(raw string) map[string]bool {
	if strings.TrimSpace(raw) == "" {
		return map[string]bool{kindWorkerInstance: true, kindConfigVariable: true, kindPolicyInstance: true, kindRouting: true}
	}
	alias := map[string]string{
		"worker_instance": kindWorkerInstance, "workerinstance": kindWorkerInstance,
		"variable": kindConfigVariable, "configvariable": kindConfigVariable,
		"policy": kindPolicyInstance, "policyinstance": kindPolicyInstance,
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
