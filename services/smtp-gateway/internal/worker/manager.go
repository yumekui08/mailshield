// Package worker は組み込みワーカーと Lua ワーカーを統合し、
// 設定に従って有効なワーカーを管理するマネージャーを提供する。
package worker

import (
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/config"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
	luaworker "github.com/koizumib/mailshield/services/smtp-gateway/internal/worker/lua"
)

// Manager は有効なワーカーの一覧を管理する。
type Manager struct {
	inspectEntries   []domain.InspectEntry
	transformWorkers []domain.TransformWorker
}

// New は組み込みワーカーと Lua ワーカーを統合して Manager を構築する。
//
// workersDir / workerConfigDir はグローバル設定（config.WorkersGlobal）から渡す。
// builtinInspect / builtinTransform は Go で実装された組み込みワーカー（ClamAV・Tika 等）。
// workers_dir の Lua ワーカーと名前が衝突した場合は組み込みワーカーが優先される。
//
// 設定で enabled=true のワーカーだけが有効になる。
// transform ワーカーは order 順にソートされる。
func New(
	workersDir string,
	workerConfigDir string,
	cfg *config.WorkersConfig,
	builtinInspect []domain.InspectWorker,
	builtinTransform []domain.TransformWorker,
) (*Manager, error) {
	// Lua ワーカーをロード
	luaInspect, luaTransform, err := luaworker.LoadFromDir(workersDir, workerConfigDir)
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
				"workers_dir", workersDir)
			continue
		}
		m.inspectEntries = append(m.inspectEntries, domain.InspectEntry{
			Worker:  w,
			Timeout: time.Duration(wCfg.TimeoutSeconds) * time.Second,
		})
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
				"workers_dir", workersDir)
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

// InspectEntries は有効な検査ワーカーとそのタイムアウト設定の一覧を返す。
func (m *Manager) InspectEntries() []domain.InspectEntry {
	return m.inspectEntries
}

// InspectWorkers は有効な検査ワーカーの一覧を返す（テスト・後方互換用）。
func (m *Manager) InspectWorkers() []domain.InspectWorker {
	workers := make([]domain.InspectWorker, len(m.inspectEntries))
	for i, e := range m.inspectEntries {
		workers[i] = e.Worker
	}
	return workers
}

// TransformWorkers は有効な変換ワーカーの一覧を order 順で返す。
func (m *Manager) TransformWorkers() []domain.TransformWorker {
	return m.transformWorkers
}
