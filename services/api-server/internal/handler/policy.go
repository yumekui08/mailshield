package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/koizumib/mailshield/services/api-server/internal/audit"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/middleware"
	"github.com/koizumib/mailshield/services/api-server/internal/policyfile"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

// PolicyHandler はポリシー（routes.d/<route>/policy.yaml）の閲覧・編集 API を提供する。
type PolicyHandler struct {
	routesDir   string
	gatewayURL  string
	client      *http.Client
	repo        repository.Repository
	auditLogger *audit.Logger
}

// NewPolicyHandler は PolicyHandler を返す。
// routesDir は routes.d の絶対パス、gatewayURL は smtp-gateway のヘルスポート URL。
func NewPolicyHandler(routesDir, gatewayURL string, repo repository.Repository, auditLogger *audit.Logger) *PolicyHandler {
	return &PolicyHandler{
		routesDir:   routesDir,
		gatewayURL:  gatewayURL,
		client:      &http.Client{Timeout: 15 * time.Second},
		repo:        repo,
		auditLogger: auditLogger,
	}
}

// HandleListRoutes は GET /api/v1/policy/routes を処理する。
// 全ルートの route.yaml + policy.yaml と、ルール別ヒット件数を返す。
func (h *PolicyHandler) HandleListRoutes(w http.ResponseWriter, r *http.Request) {
	routes, err := policyfile.ListRoutes(h.routesDir)
	if err != nil {
		slog.Error("ポリシールート一覧取得失敗", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "ポリシーの読み込みに失敗しました")
		return
	}
	hits := h.fetchHits(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"routes": routes, "hits": hits})
}

// HandleGetRoute は GET /api/v1/policy/routes/{route} を処理する。
func (h *PolicyHandler) HandleGetRoute(w http.ResponseWriter, r *http.Request) {
	dir := chi.URLParam(r, "route")
	route, err := policyfile.FindRoute(h.routesDir, dir)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	if route == nil {
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "ルートが見つかりません")
		return
	}
	writeJSON(w, http.StatusOK, route)
}

// HandleUpdateRoute は PUT /api/v1/policy/routes/{route} を処理する。
// ルールを検証 → policy.yaml を書き込み → smtp-gateway に /reload を要求し、
// リロードに失敗した場合は元のファイルへ書き戻す（稼働中ポリシーを壊さない）。
func (h *PolicyHandler) HandleUpdateRoute(w http.ResponseWriter, r *http.Request) {
	dir := chi.URLParam(r, "route")
	session := middleware.GetSession(r.Context())

	route, err := policyfile.FindRoute(h.routesDir, dir)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	if route == nil {
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "ルートが見つかりません")
		return
	}

	var doc policyfile.Document
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024*1024)).Decode(&doc); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "リクエストの形式が不正です")
		return
	}
	if err := policyfile.ValidateDocument(&doc); err != nil {
		writeErrorResponse(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}

	data, err := policyfile.Marshal(&doc)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "シリアライズに失敗しました")
		return
	}

	// 元の内容を退避 → 履歴保存 → 書き込み → gateway リロード。失敗時は書き戻す。
	backup, _ := os.ReadFile(route.PolicyPath)
	h.saveVersion(r.Context(), dir, backup, session)
	if err := policyfile.WriteAtomic(route.PolicyPath, data); err != nil {
		slog.Error("policy.yaml 書き込み失敗", "path", route.PolicyPath, "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "書き込みに失敗しました")
		return
	}

	if err := h.reloadGateway(r.Context()); err != nil {
		// リロード失敗 → 書き戻して稼働中ポリシーを維持
		if backup != nil {
			_ = policyfile.WriteAtomic(route.PolicyPath, backup)
		}
		slog.Warn("ポリシーリロード失敗（変更を巻き戻しました）", "route", dir, "error", err)
		writeErrorResponse(w, http.StatusUnprocessableEntity, "RELOAD_FAILED",
			"smtp-gateway が新しいポリシーを拒否しました: "+err.Error())
		return
	}

	h.audit(session, "policy.updated", dir)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "rules": len(doc.Rules)})
}

// HandleListVersions は GET /api/v1/policy/routes/{route}/versions を処理する。
func (h *PolicyHandler) HandleListVersions(w http.ResponseWriter, r *http.Request) {
	dir := chi.URLParam(r, "route")
	versions, err := h.repo.ListPolicyVersions(r.Context(), dir, 50)
	if err != nil {
		slog.Error("ポリシー履歴取得失敗", "route", dir, "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "履歴の取得に失敗しました")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
}

// HandleRollback は POST /api/v1/policy/routes/{route}/rollback を処理する。
// ボディ: {"version_id": "..."}。指定バージョンの内容を書き戻して gateway に反映する。
func (h *PolicyHandler) HandleRollback(w http.ResponseWriter, r *http.Request) {
	dir := chi.URLParam(r, "route")
	session := middleware.GetSession(r.Context())

	route, err := policyfile.FindRoute(h.routesDir, dir)
	if err != nil || route == nil {
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "ルートが見つかりません")
		return
	}

	var body struct {
		VersionID string `json:"version_id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&body); err != nil || body.VersionID == "" {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "version_id が必要です")
		return
	}
	ver, err := h.repo.GetPolicyVersion(r.Context(), body.VersionID)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "履歴の取得に失敗しました")
		return
	}
	if ver == nil || ver.RouteDir != dir {
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "指定のバージョンが見つかりません")
		return
	}

	// 現在の内容を履歴に残してから書き戻す
	backup, _ := os.ReadFile(route.PolicyPath)
	h.saveVersion(r.Context(), dir, backup, session)
	if err := policyfile.WriteAtomic(route.PolicyPath, []byte(ver.Content)); err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "書き込みに失敗しました")
		return
	}
	if err := h.reloadGateway(r.Context()); err != nil {
		if backup != nil {
			_ = policyfile.WriteAtomic(route.PolicyPath, backup)
		}
		writeErrorResponse(w, http.StatusUnprocessableEntity, "RELOAD_FAILED",
			"smtp-gateway が復元後のポリシーを拒否しました: "+err.Error())
		return
	}
	h.audit(session, "policy.rolled_back", dir)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// saveVersion は content（変更前の policy.yaml）を履歴に保存する（best-effort）。
func (h *PolicyHandler) saveVersion(ctx context.Context, dir string, content []byte, session *domain.Session) {
	if content == nil {
		return // 既存ファイルなし（初回作成）は履歴なし
	}
	v := &domain.PolicyVersion{
		ID:       uuid.NewString(),
		RouteDir: dir,
		Content:  string(content),
	}
	if session != nil {
		v.ActorID = audit.StrPtr(session.User.Sub)
		v.ActorEmail = audit.StrPtr(session.User.Email)
	}
	if err := h.repo.SavePolicyVersion(ctx, v); err != nil {
		slog.Warn("ポリシー履歴の保存失敗（続行）", "route", dir, "error", err)
	}
}

func (h *PolicyHandler) audit(session *domain.Session, event, dir string) {
	if h.auditLogger == nil || session == nil {
		return
	}
	h.auditLogger.Log(domain.AuditLog{
		EventType:  event,
		ActorID:    audit.StrPtr(session.User.Sub),
		ActorEmail: audit.StrPtr(session.User.Email),
		TargetType: audit.StrPtr("policy_route"),
		TargetID:   audit.StrPtr(dir),
	})
}

// HandleStats は GET /api/v1/policy/stats を処理する（ルール別ヒット件数のプロキシ）。
func (h *PolicyHandler) HandleStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"hits": h.fetchHits(r.Context())})
}

// reloadGateway は smtp-gateway の POST /reload を呼ぶ。非 200 はエラー本文を含めて返す。
func (h *PolicyHandler) reloadGateway(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.gatewayURL+"/reload", nil)
	if err != nil {
		return err
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("smtp-gateway に接続できません: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &e)
		if e.Error != "" {
			return fmt.Errorf("%s", e.Error)
		}
		return fmt.Errorf("smtp-gateway が status %d を返しました", resp.StatusCode)
	}
	return nil
}

// fetchHits は smtp-gateway の GET /policy/stats を取得する（失敗時は空マップ）。
func (h *PolicyHandler) fetchHits(ctx context.Context) map[string]map[string]int64 {
	empty := map[string]map[string]int64{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.gatewayURL+"/policy/stats", nil)
	if err != nil {
		return empty
	}
	resp, err := h.client.Do(req)
	if err != nil {
		slog.Warn("ポリシーヒット件数の取得失敗", "error", err)
		return empty
	}
	defer resp.Body.Close()
	var out struct {
		Hits map[string]map[string]int64 `json:"hits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.Hits == nil {
		return empty
	}
	return out.Hits
}
