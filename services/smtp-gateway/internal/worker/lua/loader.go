package lua

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	glua "github.com/yuin/gopher-lua"
	"gopkg.in/yaml.v3"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

// LoadFromDir は workersDir 配下のサブディレクトリをスキャンし、
// 各ディレクトリの init.lua をワーカースクリプトとしてロードする。
// configDir が指定された場合、<configDir>/<worker名>.yaml をワーカー設定として読み込む。
// workersDir が存在しない場合は空のマップを返す（エラーにしない）。
func LoadFromDir(workersDir, configDir string) (map[string]domain.InspectWorker, map[string]domain.TransformWorker, error) {
	entries, err := os.ReadDir(workersDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return make(map[string]domain.InspectWorker), make(map[string]domain.TransformWorker), nil
		}
		return nil, nil, fmt.Errorf("ワーカーディレクトリ読み取り失敗 (%s): %w", workersDir, err)
	}

	inspects := make(map[string]domain.InspectWorker)
	transforms := make(map[string]domain.TransformWorker)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		workerName := entry.Name()
		initPath := filepath.Join(workersDir, workerName, "init.lua")

		src, err := os.ReadFile(initPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				slog.Warn("init.lua が見つかりません（スキップ）", "worker", workerName, "path", initPath)
				continue
			}
			return nil, nil, fmt.Errorf("Luaスクリプト読み込み失敗 (%s): %w", initPath, err)
		}

		workerType, err := probeScript(string(src), initPath)
		if err != nil {
			return nil, nil, err
		}

		cfg, err := loadWorkerConfig(configDir, workerName)
		if err != nil {
			return nil, nil, err
		}

		switch workerType {
		case "inspect":
			if _, exists := inspects[workerName]; exists {
				return nil, nil, fmt.Errorf("検査ワーカー名が重複しています: %s", workerName)
			}
			inspects[workerName] = &inspectWorker{name: workerName, source: string(src), config: cfg}
		case "transform":
			if _, exists := transforms[workerName]; exists {
				return nil, nil, fmt.Errorf("変換ワーカー名が重複しています: %s", workerName)
			}
			transforms[workerName] = &transformWorker{name: workerName, source: string(src), config: cfg}
		default:
			return nil, nil, fmt.Errorf("不明なワーカータイプ '%s' (%s)", workerType, initPath)
		}
	}

	return inspects, transforms, nil
}

// loadWorkerConfig は configDir/<workerName>.yaml を読み込んでワーカー設定を返す。
// ファイルが存在しない場合は空のマップを返す（エラーにしない）。
// configDir が空文字列の場合も空のマップを返す。
func loadWorkerConfig(configDir, workerName string) (map[string]any, error) {
	if configDir == "" {
		return make(map[string]any), nil
	}

	yamlPath := filepath.Join(configDir, workerName+".yaml")

	data, err := os.ReadFile(yamlPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return make(map[string]any), nil
		}
		return nil, fmt.Errorf("ワーカー設定ファイル読み込み失敗 (%s): %w", yamlPath, err)
	}

	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("ワーカー設定ファイルパース失敗 (%s): %w", yamlPath, err)
	}
	if cfg == nil {
		cfg = make(map[string]any)
	}
	return cfg, nil
}

// probeScript はスクリプトを一度実行して type メタデータを取得する。
// ワーカー名はディレクトリ名が正であるため、M.name は参照しない。
func probeScript(source, path string) (workerType string, err error) {
	L := glua.NewState()
	defer L.Close()

	module, err := loadModule(L, source, path)
	if err != nil {
		return "", err
	}

	typeVal := module.RawGetString("type")
	if typeVal == glua.LNil {
		return "", fmt.Errorf("Luaワーカーに type フィールドがありません (%s)", path)
	}

	return typeVal.String(), nil
}

// loadModule はソースを実行してトップレベルの return 値（テーブル）を返す共通ヘルパー。
func loadModule(L *glua.LState, source, label string) (*glua.LTable, error) {
	fn, err := L.LoadString(source)
	if err != nil {
		return nil, fmt.Errorf("Lua構文エラー (%s): %w", label, err)
	}
	L.Push(fn)
	if err := L.PCall(0, 1, nil); err != nil {
		return nil, fmt.Errorf("Luaスクリプト実行失敗 (%s): %w", label, err)
	}
	module, ok := L.Get(-1).(*glua.LTable)
	L.Pop(1)
	if !ok {
		return nil, fmt.Errorf("Luaスクリプトがテーブルを返しませんでした (%s)", label)
	}
	return module, nil
}
