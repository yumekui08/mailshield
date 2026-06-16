// Package worker は組み込みワーカーと Lua ワーカーを統合し、
// 設定に従って有効なワーカーを管理するマネージャーを提供する。
package worker

import (
	"fmt"
	"log/slog"
	"sort"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/config"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
	luaworker "github.com/koizumib/mailshield/services/smtp-gateway/internal/worker/lua"
)

// Manager は有効なワーカーの一覧を管理する。
type Manager struct {
	inspectWorkers   []domain.InspectWorker
	transformWorkers []domain.TransformWorker
}

// New は組み込みワーカーと Lua ワーカーを統合して Manager を構築する。
//
// builtinInspect / builtinTransform は Go で実装された組み込みワーカー（ClamAV・Tika 等）。
// workers_dir の Lua ワーカーと名前が衝突した場合は組み込みワーカーが優先される。
//
// 設定で enabled=true のワーカーだけが有効になる。
// transform ワーカーは order 順にソートされる。
func New(
	cfg *config.WorkersConfig,
	builtinInspect []domain.InspectWorker,
	builtinTransform []domain.TransformWorker,
) (*Manager, error) {
	// Lua ワーカーをロード
	luaInspect, luaTransform, err := luaworker.LoadFromDir(cfg.WorkersDir, cfg.WorkerConfigDir)
	if err != nil {
		return nil, fmt.Errorf("Luaワーカーロード失敗: %w", err)
	}

	// 統合レジストリを構築（Lua を先に登録し、組み込みで上書き）
	inspectRegistry := make(map[string]domain.InspectWorker, len(luaInspect)+len(builtinInspect))
	for name, w := range luaInspect {
		inspectRegistry[name] = w
	}
	for _, w := range builtinInspect {
		if _, exists := inspectRegistry[w.Name()]; exists {
			slog.Warn("組み込み検査ワーカーが同名のLuaワーカーを上書きします", "name", w.Name())
		}
		inspectRegistry[w.Name()] = w
	}

	transformRegistry := make(map[string]domain.TransformWorker, len(luaTransform)+len(builtinTransform))
	for name, w := range luaTransform {
		transformRegistry[name] = w
	}
	for _, w := range builtinTransform {
		if _, exists := transformRegistry[w.Name()]; exists {
			slog.Warn("組み込み変換ワーカーが同名のLuaワーカーを上書きします", "name", w.Name())
		}
		transformRegistry[w.Name()] = w
	}

	m := &Manager{}

	// 検査ワーカー（有効なもののみ）
	for _, wCfg := range cfg.Inspect {
		if !wCfg.Enabled {
			continue
		}
		w, ok := inspectRegistry[wCfg.Name]
		if !ok {
			slog.Warn("設定された検査ワーカーが見つかりません",
				"name", wCfg.Name,
				"workers_dir", cfg.WorkersDir)
			continue
		}
		m.inspectWorkers = append(m.inspectWorkers, w)
	}

	// 変換ワーカー（有効なもののみ・order 順）
	type orderedTransform struct {
		order  int
		worker domain.TransformWorker
	}
	var ordered []orderedTransform
	for _, wCfg := range cfg.Transform {
		if !wCfg.Enabled {
			continue
		}
		w, ok := transformRegistry[wCfg.Name]
		if !ok {
			slog.Warn("設定された変換ワーカーが見つかりません",
				"name", wCfg.Name,
				"workers_dir", cfg.WorkersDir)
			continue
		}
		ordered = append(ordered, orderedTransform{order: wCfg.Order, worker: w})
	}
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].order < ordered[j].order
	})
	for _, o := range ordered {
		m.transformWorkers = append(m.transformWorkers, o.worker)
	}

	return m, nil
}

// InspectWorkers は有効な検査ワーカーの一覧を返す。
func (m *Manager) InspectWorkers() []domain.InspectWorker {
	return m.inspectWorkers
}

// TransformWorkers は有効な変換ワーカーの一覧を order 順で返す。
func (m *Manager) TransformWorkers() []domain.TransformWorker {
	return m.transformWorkers
}
