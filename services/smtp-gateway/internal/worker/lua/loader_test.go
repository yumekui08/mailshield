package lua_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
	luaworker "github.com/koizumib/mailshield/services/smtp-gateway/internal/worker/lua"
)

// ─── テスト用スクリプト ──────────────────────────────────────

const inspectorSrc = `
local M = {}
M.name = "subject-virus-inspector"
M.type = "inspect"
function M.inspect(mail, config)
    local keywords = config.keywords or { "virus" }
    local score    = config.score    or 100
    local subject  = mail.subject    or ""
    local lower    = string.lower(subject)
    for _, kw in ipairs(keywords) do
        if string.find(lower, kw, 1, true) then
            return { detected = true, score = score, details = { reason = "subject contains '" .. kw .. "'" } }
        end
    end
    return { detected = false, score = 0, details = {} }
end
return M
`

const transformerSrc = `
local M = {}
M.name = "subject-virus-transformer"
M.type = "transform"
function M.transform(mail, config)
    local keywords = config.keywords or { "virus" }
    local prefix   = config.prefix   or "[迷惑メール注意] "
    local subject  = mail.subject    or ""
    local lower    = string.lower(subject)
    for _, kw in ipairs(keywords) do
        if string.find(lower, kw, 1, true) then
            mail.subject = prefix .. subject
            break
        end
    end
    return mail
end
return M
`

// ─── ヘルパー ───────────────────────────────────────────────

// writeWorker は workersDir/<name>/init.lua を作成する。
func writeWorker(t *testing.T, workersDir, name, src string) {
	t.Helper()
	dir := filepath.Join(workersDir, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "init.lua"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
}

// writeConfig は configDir/<name>.yaml を作成する。
func writeConfig(t *testing.T, configDir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(configDir, name+".yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// ─── LoadFromDir テスト ──────────────────────────────────────

func TestLoadFromDir_Empty(t *testing.T) {
	workersDir := t.TempDir()
	ins, tra, err := luaworker.LoadFromDir(workersDir, "")
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if len(ins) != 0 || len(tra) != 0 {
		t.Errorf("空ディレクトリから何かロードされた: ins=%d tra=%d", len(ins), len(tra))
	}
}

func TestLoadFromDir_NotExist(t *testing.T) {
	ins, tra, err := luaworker.LoadFromDir("/nonexistent/workers", "")
	if err != nil {
		t.Fatalf("存在しないディレクトリはエラーにしてはいけない: %v", err)
	}
	if len(ins) != 0 || len(tra) != 0 {
		t.Error("存在しないディレクトリから何かロードされた")
	}
}

func TestLoadFromDir_LoadsBothTypes(t *testing.T) {
	workersDir := t.TempDir()
	writeWorker(t, workersDir, "subject-virus-inspector", inspectorSrc)
	writeWorker(t, workersDir, "subject-virus-transformer", transformerSrc)

	ins, tra, err := luaworker.LoadFromDir(workersDir, "")
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if len(ins) != 1 {
		t.Errorf("検査ワーカー数 = %d, want 1", len(ins))
	}
	if len(tra) != 1 {
		t.Errorf("変換ワーカー数 = %d, want 1", len(tra))
	}
	if _, ok := ins["subject-virus-inspector"]; !ok {
		t.Error("subject-virus-inspector が見つからない")
	}
	if _, ok := tra["subject-virus-transformer"]; !ok {
		t.Error("subject-virus-transformer が見つからない")
	}
}

func TestLoadFromDir_WorkerNameIsDirectoryName(t *testing.T) {
	workersDir := t.TempDir()
	// M.name と異なるディレクトリ名を使う → ディレクトリ名が正
	writeWorker(t, workersDir, "my-custom-name", inspectorSrc)

	ins, _, err := luaworker.LoadFromDir(workersDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ins["my-custom-name"]; !ok {
		t.Error("ワーカー名はディレクトリ名になるはず")
	}
	if ins["my-custom-name"].Name() != "my-custom-name" {
		t.Errorf("Name() = %q, want %q", ins["my-custom-name"].Name(), "my-custom-name")
	}
}

func TestLoadFromDir_SkipsNoInitLua(t *testing.T) {
	workersDir := t.TempDir()
	// init.lua のないサブディレクトリはスキップされること
	if err := os.MkdirAll(filepath.Join(workersDir, "empty-worker"), 0755); err != nil {
		t.Fatal(err)
	}
	writeWorker(t, workersDir, "subject-virus-inspector", inspectorSrc)

	ins, _, err := luaworker.LoadFromDir(workersDir, "")
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if len(ins) != 1 {
		t.Errorf("検査ワーカー数 = %d, want 1（init.lua なしはスキップ）", len(ins))
	}
}

func TestLoadFromDir_IgnoresFiles(t *testing.T) {
	workersDir := t.TempDir()
	writeWorker(t, workersDir, "subject-virus-inspector", inspectorSrc)
	// ファイル（ディレクトリでないもの）は無視される
	if err := os.WriteFile(filepath.Join(workersDir, "readme.txt"), []byte("ignore me"), 0644); err != nil {
		t.Fatal(err)
	}

	ins, _, err := luaworker.LoadFromDir(workersDir, "")
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if len(ins) != 1 {
		t.Errorf("検査ワーカー数 = %d, want 1", len(ins))
	}
}

// ─── ワーカー設定ファイル（configDir）テスト ─────────────────

func TestLoadFromDir_LoadsWorkerConfig(t *testing.T) {
	workersDir := t.TempDir()
	configDir := t.TempDir()
	writeWorker(t, workersDir, "subject-virus-inspector", inspectorSrc)
	writeConfig(t, configDir, "subject-virus-inspector", `
keywords:
  - malware
  - trojan
score: 80
`)

	ins, _, err := luaworker.LoadFromDir(workersDir, configDir)
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	w := ins["subject-virus-inspector"]

	ctx := context.Background()

	// config.keywords から malware が検知されること
	r, err := w.Inspect(ctx, &domain.Mail{Subject: "malware detected"})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Detected {
		t.Error("malware は検知されるべき（config.keywords から）")
	}
	if r.Score != 80 {
		t.Errorf("Score = %d, want 80（config.score から）", r.Score)
	}

	// config に含まれない virus は検知されないこと
	r2, err := w.Inspect(ctx, &domain.Mail{Subject: "virus test"})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Detected {
		t.Error("virus は config.keywords に含まれていないので検知されないはず")
	}
}

func TestLoadFromDir_NoConfigFileUsesDefaults(t *testing.T) {
	workersDir := t.TempDir()
	writeWorker(t, workersDir, "subject-virus-inspector", inspectorSrc)
	// configDir なし → config は空テーブル → Lua 側のデフォルト値が使われる

	ins, _, err := luaworker.LoadFromDir(workersDir, "")
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	r, err := ins["subject-virus-inspector"].Inspect(context.Background(), &domain.Mail{Subject: "virus test"})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Detected {
		t.Error("virus はデフォルトキーワードで検知されるべき")
	}
	if r.Score != 100 {
		t.Errorf("Score = %d, want 100（Lua デフォルト）", r.Score)
	}
}

func TestLoadFromDir_ConfigNestedTable(t *testing.T) {
	workersDir := t.TempDir()
	configDir := t.TempDir()
	nestedSrc := `
local M = {}
M.name = "nested-worker"
M.type = "inspect"
function M.inspect(mail, config)
    local threshold = config.thresholds and config.thresholds.spam or 50
    return { detected = threshold > 30, score = threshold, details = {} }
end
return M
`
	writeWorker(t, workersDir, "nested-worker", nestedSrc)
	writeConfig(t, configDir, "nested-worker", `
thresholds:
  spam: 70
  phishing: 90
`)

	ins, _, err := luaworker.LoadFromDir(workersDir, configDir)
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	r, err := ins["nested-worker"].Inspect(context.Background(), &domain.Mail{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Score != 70 {
		t.Errorf("Score = %d, want 70（ネストした config.thresholds.spam から）", r.Score)
	}
}

// ─── InspectWorker テスト ────────────────────────────────────

func TestInspectWorker_Inspect(t *testing.T) {
	workersDir := t.TempDir()
	writeWorker(t, workersDir, "subject-virus-inspector", inspectorSrc)
	ins, _, err := luaworker.LoadFromDir(workersDir, "")
	if err != nil {
		t.Fatal(err)
	}
	w := ins["subject-virus-inspector"]

	tests := []struct {
		subject      string
		wantDetected bool
		wantScore    int
	}{
		{"virus test mail", true, 100},
		{"VIRUS DETECTED", true, 100},
		{"This is a Virus alert", true, 100},
		{"Hello World", false, 0},
		{"", false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.subject, func(t *testing.T) {
			r, err := w.Inspect(context.Background(), &domain.Mail{Subject: tt.subject})
			if err != nil {
				t.Fatalf("Inspect: %v", err)
			}
			if r.Detected != tt.wantDetected {
				t.Errorf("Detected = %v, want %v", r.Detected, tt.wantDetected)
			}
			if r.Score != tt.wantScore {
				t.Errorf("Score = %d, want %d", r.Score, tt.wantScore)
			}
		})
	}
}

// ─── TransformWorker テスト ──────────────────────────────────

func TestTransformWorker_Transform(t *testing.T) {
	workersDir := t.TempDir()
	writeWorker(t, workersDir, "subject-virus-transformer", transformerSrc)
	_, tra, err := luaworker.LoadFromDir(workersDir, "")
	if err != nil {
		t.Fatal(err)
	}
	w := tra["subject-virus-transformer"]

	tests := []struct {
		subject        string
		rawEML         string
		wantSubject    string
		wantEMLContain string
	}{
		{
			subject:        "virus test mail",
			rawEML:         "Subject: virus test mail\r\nFrom: a@b.com\r\n\r\nBody",
			wantSubject:    "[迷惑メール注意] virus test mail",
			wantEMLContain: "Subject: [迷惑メール注意] virus test mail",
		},
		{
			subject:        "Hello World",
			rawEML:         "Subject: Hello World\r\nFrom: a@b.com\r\n\r\nBody",
			wantSubject:    "Hello World",
			wantEMLContain: "Subject: Hello World",
		},
	}
	for _, tt := range tests {
		t.Run(tt.subject, func(t *testing.T) {
			original := &domain.Mail{Subject: tt.subject, RawEML: []byte(tt.rawEML)}
			result, err := w.Transform(context.Background(), original)
			if err != nil {
				t.Fatalf("Transform: %v", err)
			}
			if result.Subject != tt.wantSubject {
				t.Errorf("Subject = %q, want %q", result.Subject, tt.wantSubject)
			}
			if !bytes.Contains(result.RawEML, []byte(tt.wantEMLContain)) {
				t.Errorf("RawEML に %q が含まれていない", tt.wantEMLContain)
			}
			if !strings.Contains(string(result.RawEML), "Body") {
				t.Error("RawEML のボディが消えた")
			}
			if original.Subject != tt.subject {
				t.Errorf("元の Mail が変更された: %q", original.Subject)
			}
		})
	}
}

func TestTransformWorker_CustomConfig(t *testing.T) {
	workersDir := t.TempDir()
	configDir := t.TempDir()
	writeWorker(t, workersDir, "subject-virus-transformer", transformerSrc)
	writeConfig(t, configDir, "subject-virus-transformer", `
keywords:
  - malware
prefix: "[危険] "
`)

	_, tra, err := luaworker.LoadFromDir(workersDir, configDir)
	if err != nil {
		t.Fatal(err)
	}
	w := tra["subject-virus-transformer"]

	r, err := w.Transform(context.Background(), &domain.Mail{
		Subject: "malware detected",
		RawEML:  []byte("Subject: malware detected\r\n\r\nBody"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Subject != "[危険] malware detected" {
		t.Errorf("Subject = %q, want %q", r.Subject, "[危険] malware detected")
	}

	// config.keywords に含まれない virus は変換されないこと
	r2, err := w.Transform(context.Background(), &domain.Mail{
		Subject: "virus test",
		RawEML:  []byte("Subject: virus test\r\n\r\nBody"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Subject != "virus test" {
		t.Errorf("virus は変換されないはず: %q", r2.Subject)
	}
}
