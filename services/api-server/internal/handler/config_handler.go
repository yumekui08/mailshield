package handler

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/koizumib/mailshield/services/api-server/internal/audit"
	"github.com/koizumib/mailshield/services/api-server/internal/configsnap"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/middleware"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

// ConfigHandler は設定エンティティ（ワーカーインスタンス・設定変数・ルーティング）の管理 API（ADR 008）。
type ConfigHandler struct {
	repo        repository.ConfigRepository
	auditLogger *audit.Logger
	publisher   *configsnap.Publisher
}

func NewConfigHandler(repo repository.ConfigRepository, auditLogger *audit.Logger) *ConfigHandler {
	return &ConfigHandler{repo: repo, auditLogger: auditLogger, publisher: configsnap.NewPublisher(repo)}
}

// publish は設定変更後にスナップショットを検証・publish し、アクティブ版を切り替える。
// 検証に失敗しても書き込み自体は成功済みなので、ここではログのみ（次回の publish で回復する）。
// gateway はアクティブ版のみを読むため、検証に通らない中間状態は配布されない。
func (h *ConfigHandler) publish(r *http.Request) {
	if h.publisher == nil {
		return
	}
	author := ""
	if s := middleware.GetSession(r.Context()); s != nil {
		author = s.User.Email
	}
	if _, err := h.publisher.Publish(r.Context(), "ui", author); err != nil {
		slog.Warn("設定スナップショットの publish に失敗（アクティブ版は未更新）", "error", err)
	}
}

// alias は条件 DSL・検査結果のキーに使うため、識別子として安全な形に限定する。
var aliasPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// 変数キーは ${VAR} の VAR。環境変数慣習に合わせ英大文字始まりも許可する。
var varKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ─── ワーカーインスタンス ─────────────────────────────────────────────

type workerInstanceRequest struct {
	Alias                 string         `json:"alias"`
	DisplayName           string         `json:"display_name"`
	WorkerType            string         `json:"worker_type"`
	Kind                  string         `json:"kind"`
	Config                map[string]any `json:"config"`
	DefaultTimeoutSeconds int            `json:"default_timeout_seconds"`
	IsEnabled             *bool          `json:"is_enabled"`
}

func (h *ConfigHandler) HandleListWorkerInstances(w http.ResponseWriter, r *http.Request) {
	list, err := h.repo.ListWorkerInstances(r.Context())
	if err != nil {
		slog.Error("ワーカーインスタンス一覧取得失敗", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "取得に失敗しました")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": list, "meta": map[string]int{"total": len(list)}})
}

func (h *ConfigHandler) HandleCreateWorkerInstance(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeWorkerInstance(w, r)
	if !ok {
		return
	}
	inst := &domain.WorkerInstance{
		Alias: req.Alias, DisplayName: req.DisplayName, WorkerType: req.WorkerType,
		Kind: domain.WorkerKind(req.Kind), Config: req.Config,
		DefaultTimeoutSeconds: req.DefaultTimeoutSeconds, IsEnabled: req.IsEnabled == nil || *req.IsEnabled,
	}
	if inst.Config == nil {
		inst.Config = map[string]any{}
	}
	if err := h.repo.CreateWorkerInstance(r.Context(), inst); err != nil {
		writeConfigWriteError(w, err, "ワーカーインスタンス作成失敗")
		return
	}
	h.audit(r, "config.worker_instance.create", inst.ID)
	writeJSON(w, http.StatusCreated, inst)
}

func (h *ConfigHandler) HandleUpdateWorkerInstance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := h.repo.GetWorkerInstance(r.Context(), id)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "取得に失敗しました")
		return
	}
	if existing == nil {
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "見つかりません")
		return
	}
	req, ok := decodeWorkerInstance(w, r)
	if !ok {
		return
	}
	existing.Alias = req.Alias
	existing.DisplayName = req.DisplayName
	existing.WorkerType = req.WorkerType
	existing.Kind = domain.WorkerKind(req.Kind)
	existing.Config = req.Config
	if existing.Config == nil {
		existing.Config = map[string]any{}
	}
	existing.DefaultTimeoutSeconds = req.DefaultTimeoutSeconds
	if req.IsEnabled != nil {
		existing.IsEnabled = *req.IsEnabled
	}
	if err := h.repo.UpdateWorkerInstance(r.Context(), existing); err != nil {
		writeConfigWriteError(w, err, "ワーカーインスタンス更新失敗")
		return
	}
	h.audit(r, "config.worker_instance.update", id)
	writeJSON(w, http.StatusOK, existing)
}

func (h *ConfigHandler) HandleDeleteWorkerInstance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.repo.DeleteWorkerInstance(r.Context(), id); err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "削除に失敗しました")
		return
	}
	h.audit(r, "config.worker_instance.delete", id)
	w.WriteHeader(http.StatusNoContent)
}

func decodeWorkerInstance(w http.ResponseWriter, r *http.Request) (workerInstanceRequest, bool) {
	var req workerInstanceRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "リクエストの解析に失敗しました")
		return req, false
	}
	req.Alias = strings.TrimSpace(req.Alias)
	req.WorkerType = strings.TrimSpace(req.WorkerType)
	if !aliasPattern.MatchString(req.Alias) {
		writeErrorResponse(w, http.StatusBadRequest, "INVALID_ALIAS",
			"alias は英小文字で始まり、英小文字・数字・_ のみ使えます（条件式で参照するため）")
		return req, false
	}
	if req.WorkerType == "" {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "worker_type は必須です")
		return req, false
	}
	if req.Kind != string(domain.WorkerKindInspect) && req.Kind != string(domain.WorkerKindTransform) {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "kind は inspect または transform です")
		return req, false
	}
	if req.DefaultTimeoutSeconds < 0 {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "default_timeout_seconds は 0 以上です")
		return req, false
	}
	return req, true
}

// ─── 設定変数 ─────────────────────────────────────────────────────────

type configVariableRequest struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description"`
}

func (h *ConfigHandler) HandleListConfigVariables(w http.ResponseWriter, r *http.Request) {
	list, err := h.repo.ListConfigVariables(r.Context())
	if err != nil {
		slog.Error("設定変数一覧取得失敗", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "取得に失敗しました")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": list, "meta": map[string]int{"total": len(list)}})
}

func (h *ConfigHandler) HandleCreateConfigVariable(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeConfigVariable(w, r)
	if !ok {
		return
	}
	v := &domain.ConfigVariable{Key: req.Key, Value: req.Value, Description: req.Description}
	if err := h.repo.CreateConfigVariable(r.Context(), v); err != nil {
		writeConfigWriteError(w, err, "設定変数作成失敗")
		return
	}
	h.audit(r, "config.variable.create", v.ID)
	writeJSON(w, http.StatusCreated, v)
}

func (h *ConfigHandler) HandleUpdateConfigVariable(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := h.repo.GetConfigVariable(r.Context(), id)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "取得に失敗しました")
		return
	}
	if existing == nil {
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "見つかりません")
		return
	}
	req, ok := decodeConfigVariable(w, r)
	if !ok {
		return
	}
	existing.Key = req.Key
	existing.Value = req.Value
	existing.Description = req.Description
	if err := h.repo.UpdateConfigVariable(r.Context(), existing); err != nil {
		writeConfigWriteError(w, err, "設定変数更新失敗")
		return
	}
	h.audit(r, "config.variable.update", id)
	writeJSON(w, http.StatusOK, existing)
}

func (h *ConfigHandler) HandleDeleteConfigVariable(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.repo.DeleteConfigVariable(r.Context(), id); err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "削除に失敗しました")
		return
	}
	h.audit(r, "config.variable.delete", id)
	w.WriteHeader(http.StatusNoContent)
}

func decodeConfigVariable(w http.ResponseWriter, r *http.Request) (configVariableRequest, bool) {
	var req configVariableRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "リクエストの解析に失敗しました")
		return req, false
	}
	req.Key = strings.TrimSpace(req.Key)
	if !varKeyPattern.MatchString(req.Key) {
		writeErrorResponse(w, http.StatusBadRequest, "INVALID_KEY",
			"key は英字・_ で始まり、英数字・_ のみ使えます")
		return req, false
	}
	return req, true
}

// ─── 共通ヘルパ ───────────────────────────────────────────────────────

// writeConfigWriteError は書き込み系エラーを返す。UNIQUE 制約違反（alias/key 重複）は 409。
func writeConfigWriteError(w http.ResponseWriter, err error, logMsg string) {
	slog.Error(logMsg, "error", err)
	if strings.Contains(err.Error(), "Duplicate entry") {
		writeErrorResponse(w, http.StatusConflict, "CONFLICT", "alias または key が既に使われています")
		return
	}
	writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "保存に失敗しました")
}

// audit は設定変更を監査ログに記録し、続けてスナップショットを publish する。
// 設定エンティティの全ミューテーション（作成/更新/削除）成功後にのみ呼ばれるため、
// ここを唯一の「変更確定フック」として使い、アクティブ版の再生成を一箇所に集約する。
func (h *ConfigHandler) audit(r *http.Request, event, targetID string) {
	session := middleware.GetSession(r.Context())
	if session != nil && h.auditLogger != nil {
		h.auditLogger.Log(domain.AuditLog{
			EventType:  event,
			ActorID:    audit.StrPtr(session.User.Sub),
			ActorEmail: audit.StrPtr(session.User.Email),
			TargetType: audit.StrPtr("config"),
			TargetID:   audit.StrPtr(targetID),
		})
	}
	h.publish(r)
}

// ─── ルーティング ─────────────────────────────────────────────────────

type routingRequest struct {
	Name      string                 `json:"name"`
	Priority  int                    `json:"priority"`
	MatchExpr string                 `json:"match_expr"`
	Direction string                 `json:"direction"`
	IsEnabled *bool                  `json:"is_enabled"`
	PolicyRef string                 `json:"policy_ref"`
	Inspect   []domain.WorkerBinding `json:"inspect"`
	Transform []domain.WorkerBinding `json:"transform"`
}

func (h *ConfigHandler) HandleListRoutings(w http.ResponseWriter, r *http.Request) {
	// ルーティングはすべてデータ。空状態も正当（マッチしないメールは拒否＝Postfix へ 550）。
	// 「すべてに一致」させたい場合はユーザーが match_expr: "true" のルーティングを 1 つ作る。
	list, err := h.repo.ListRoutings(r.Context())
	if err != nil {
		slog.Error("ルーティング一覧取得失敗", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "取得に失敗しました")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": list, "meta": map[string]int{"total": len(list)}})
}

func (h *ConfigHandler) HandleCreateRouting(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeRouting(w, r)
	if !ok {
		return
	}
	rt := &domain.Routing{
		Name: req.Name, Priority: req.Priority, MatchExpr: req.MatchExpr, Direction: req.Direction,
		IsCatchAll: false, IsEnabled: req.IsEnabled == nil || *req.IsEnabled,
		PolicyRef: req.PolicyRef, Inspect: req.Inspect, Transform: req.Transform,
	}
	if err := h.repo.CreateRouting(r.Context(), rt); err != nil {
		writeConfigWriteError(w, err, "ルーティング作成失敗")
		return
	}
	h.audit(r, "config.routing.create", rt.ID)
	writeJSON(w, http.StatusCreated, rt)
}

func (h *ConfigHandler) HandleUpdateRouting(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := h.repo.GetRouting(r.Context(), id)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "取得に失敗しました")
		return
	}
	if existing == nil {
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "見つかりません")
		return
	}
	req, ok := decodeRouting(w, r)
	if !ok {
		return
	}
	existing.Name = req.Name
	existing.Direction = req.Direction
	existing.PolicyRef = req.PolicyRef
	existing.Inspect = req.Inspect
	existing.Transform = req.Transform
	existing.MatchExpr = req.MatchExpr
	existing.Priority = req.Priority
	if req.IsEnabled != nil {
		existing.IsEnabled = *req.IsEnabled
	}
	if err := h.repo.UpdateRouting(r.Context(), existing); err != nil {
		writeConfigWriteError(w, err, "ルーティング更新失敗")
		return
	}
	h.audit(r, "config.routing.update", id)
	writeJSON(w, http.StatusOK, existing)
}

func (h *ConfigHandler) HandleDeleteRouting(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := h.repo.GetRouting(r.Context(), id)
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "取得に失敗しました")
		return
	}
	if existing == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.repo.DeleteRouting(r.Context(), id); err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "削除に失敗しました")
		return
	}
	h.audit(r, "config.routing.delete", id)
	w.WriteHeader(http.StatusNoContent)
}

func decodeRouting(w http.ResponseWriter, r *http.Request) (routingRequest, bool) {
	var req routingRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "リクエストの解析に失敗しました")
		return req, false
	}
	req.MatchExpr = strings.TrimSpace(req.MatchExpr)
	if req.MatchExpr == "" {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST",
			"match_expr は必須です（すべてに一致させるなら \"true\"）")
		return req, false
	}
	switch req.Direction {
	case "":
		req.Direction = "inbound" // 省略時
	case "inbound", "outbound", "internal":
	default:
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST",
			"direction は inbound / outbound / internal のいずれかです")
		return req, false
	}
	for _, b := range append(append([]domain.WorkerBinding{}, req.Inspect...), req.Transform...) {
		if !aliasPattern.MatchString(b.Alias) {
			writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST",
				"束ねるワーカーインスタンスの alias が不正です: "+b.Alias)
			return req, false
		}
	}
	return req, true
}
