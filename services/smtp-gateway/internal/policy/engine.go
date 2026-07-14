// Package policy は YAML ルールファイルを読み込み、検査結果から
// アクション（deliver / reject / quarantine / approval）を決定する。
package policy

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

// Deliverer は deliver アクション実行時にメールを配送先へ送信する。
// destination はルールの destination フィールドの値
// （deliverer 名または host:port。空文字列はデフォルト配送先を意味する）。
// 実装は internal/deliver.Registry が提供する。
type Deliverer interface {
	Deliver(ctx context.Context, mail *domain.Mail, destination string) error
}

// ActionType はポリシーエンジンが決定するアクションの種類。
type ActionType string

const (
	ActionDeliver    ActionType = "deliver"
	ActionReject     ActionType = "reject"
	ActionQuarantine ActionType = "quarantine"
	ActionApproval   ActionType = "approval"
)

// Rule は policy.yaml の単一ルールを表す。
type Rule struct {
	Name        string `yaml:"name"`
	Condition   string `yaml:"condition"`
	Action      string `yaml:"action"`
	Destination string `yaml:"destination"` // deliver 時の宛先（host:port）
}

// ListConfig は名前付きリストの定義。values（インライン）と file（1 行 1 要素の
// 外部ファイル）のいずれか、または両方を指定できる。読み込み時に和集合を取る。
type ListConfig struct {
	Values []string `yaml:"values"`
	File   string   `yaml:"file"`
}

// PolicyRules は policy.yaml のトップレベル構造。
type PolicyRules struct {
	// Lists は condition の in_list で参照する名前付き集合。
	Lists map[string]ListConfig `yaml:"lists"`
	Rules []Rule                `yaml:"rules"`
}

// Engine はポリシーエンジンの実装である。
type Engine struct {
	rules []Rule
	// lists は名前付き集合（すべて小文字で保持）。in_list 条件で参照する。
	lists map[string]map[string]bool
	// deliverer は deliver アクションの配送を実行する。
	// destination の解決（deliverer 名 / host:port / デフォルト）は deliverer 側が行う。
	// nil の場合、deliver アクションはエラーになる（Evaluate のみ使う用途では nil 可）。
	deliverer Deliverer
}

// New は policy.yaml を読み込んで Engine を構築する。
// deliverer は deliver アクション実行時に使う配送トランスポート。
// ルールの destination（deliverer 名または host:port）の解決は deliverer に委譲する。
// rulesFile が空の場合はポリシーファイル未指定として全メールをデフォルト宛先に配送する。
func New(rulesFile string, deliverer Deliverer) (*Engine, error) {
	if rulesFile == "" {
		return &Engine{
			rules:     []Rule{{Name: "default", Condition: "true", Action: "deliver"}},
			lists:     map[string]map[string]bool{},
			deliverer: deliverer,
		}, nil
	}
	data, err := os.ReadFile(rulesFile)
	if err != nil {
		return nil, fmt.Errorf("policy.yaml 読み込み失敗 (%s): %w", rulesFile, err)
	}

	var pr PolicyRules
	if err := yaml.Unmarshal(data, &pr); err != nil {
		return nil, fmt.Errorf("policy.yaml パース失敗: %w", err)
	}

	// リストの外部ファイルは policy.yaml と同じディレクトリからの相対で解決する。
	lists, err := loadLists(pr.Lists, filepath.Dir(rulesFile))
	if err != nil {
		return nil, err
	}

	return &Engine{rules: pr.Rules, lists: lists, deliverer: deliverer}, nil
}

// loadLists は名前付きリストを小文字正規化した集合に変換する。
func loadLists(configs map[string]ListConfig, baseDir string) (map[string]map[string]bool, error) {
	lists := make(map[string]map[string]bool, len(configs))
	for name, lc := range configs {
		set := make(map[string]bool)
		for _, v := range lc.Values {
			if v = strings.TrimSpace(strings.ToLower(v)); v != "" {
				set[v] = true
			}
		}
		if lc.File != "" {
			path := lc.File
			if !filepath.IsAbs(path) {
				path = filepath.Join(baseDir, path)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("リスト %q のファイル読み込み失敗 (%s): %w", name, path, err)
			}
			for _, line := range strings.Split(string(content), "\n") {
				line = strings.TrimSpace(strings.ToLower(line))
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				set[line] = true
			}
		}
		lists[strings.ToLower(name)] = set
	}
	return lists, nil
}

// Evaluate は検査結果とメール属性を評価してアクション種別とマッチしたルール名を返す。
// アクション（SMTP 配送・拒否等）の実行は行わない（シミュレーター用）。
func (e *Engine) Evaluate(mail *domain.Mail, results []*domain.InspectResult) (action ActionType, matchedRule string) {
	ctx := evalContext{facts: buildFacts(mail, results), lists: e.lists}
	for _, rule := range e.rules {
		matched, err := evalCondition(rule.Condition, ctx)
		if err != nil || !matched {
			continue
		}
		return ActionType(rule.Action), rule.Name
	}
	return "", ""
}

// EvaluateAndAct は検査結果を評価してアクションを実行し、実行したアクション種別を返す。
// ルールは上から順に評価し、最初にマッチしたルールのアクションを実行する。
// マッチするルールがない場合は空文字列の ActionType と nil を返す。
func (e *Engine) EvaluateAndAct(ctx context.Context, mail *domain.Mail, results []*domain.InspectResult) (ActionType, error) {
	ec := evalContext{facts: buildFacts(mail, results), lists: e.lists}

	for _, rule := range e.rules {
		matched, err := evalCondition(rule.Condition, ec)
		if err != nil {
			slog.Warn("ルール評価エラー（スキップ）",
				"rule", rule.Name,
				"message_id", mail.MessageID,
				"error", err)
			continue
		}
		if !matched {
			continue
		}

		action := ActionType(rule.Action)
		slog.Info("ポリシーマッチ",
			"rule", rule.Name,
			"action", rule.Action,
			"message_id", mail.MessageID)

		switch action {
		case ActionDeliver:
			if err := e.deliver(ctx, mail, rule.Destination); err != nil {
				return "", err
			}
			return ActionDeliver, nil
		case ActionReject:
			slog.Info("メール拒否", "message_id", mail.MessageID, "rule", rule.Name)
			return ActionReject, nil
		case ActionQuarantine:
			slog.Info("メール隔離", "message_id", mail.MessageID, "rule", rule.Name)
			return ActionQuarantine, nil
		case ActionApproval:
			slog.Info("承認フロー保留", "message_id", mail.MessageID, "rule", rule.Name)
			return ActionApproval, nil
		default:
			return "", fmt.Errorf("未知のアクション: %s", rule.Action)
		}
	}

	slog.Warn("マッチするルールがありません（デフォルト配送なし）",
		"message_id", mail.MessageID)
	return "", nil
}

// deliver は注入された Deliverer 経由でメールを配送する。
// destination はルールの destination フィールドの値（空文字列はデフォルト配送先）。
func (e *Engine) deliver(ctx context.Context, mail *domain.Mail, destination string) error {
	if e.deliverer == nil {
		return fmt.Errorf("deliverer が設定されていません（deliver アクションを実行できません）")
	}
	return e.deliverer.Deliver(ctx, mail, destination)
}

// buildFacts はメール属性と InspectResult の一覧から条件評価用のマップを構築する。
//
// ワーカー由来のキー: "{worker_name}.detected" / "{worker_name}.score" / "{worker_name}.{detail}"
// メール属性のキー:
//
//	mail.from            エンベロープ送信者アドレス（小文字）
//	mail.from_domain     送信者のドメイン部（小文字）
//	mail.to              全宛先をカンマ連結（小文字）
//	mail.to_domains      全宛先のドメインをカンマ連結（小文字）
//	mail.subject         件名
//	mail.size_bytes      サイズ（int）
//	mail.has_attachment  添付有無（bool）
//	mail.direction       inbound / outbound
//
// 集約キー: "total_score" は全ワーカーの score 合計（Mail Detox 的な閾値運用に使う）。
func buildFacts(mail *domain.Mail, results []*domain.InspectResult) map[string]any {
	facts := make(map[string]any)
	totalScore := 0
	for _, r := range results {
		facts[r.WorkerName+".detected"] = r.Detected
		facts[r.WorkerName+".score"] = r.Score
		totalScore += r.Score
		for k, v := range r.Details {
			facts[r.WorkerName+"."+k] = v
		}
	}
	facts["total_score"] = totalScore

	if mail != nil {
		facts["mail.from"] = strings.ToLower(mail.FromAddress)
		facts["mail.from_domain"] = domainOf(mail.FromAddress)
		facts["mail.to"] = strings.ToLower(strings.Join(mail.ToAddresses, ","))
		facts["mail.to_domains"] = joinDomains(mail.ToAddresses)
		facts["mail.subject"] = mail.Subject
		facts["mail.size_bytes"] = int(mail.SizeBytes)
		facts["mail.has_attachment"] = mail.HasAttachment
		facts["mail.direction"] = string(mail.Direction)
	}
	return facts
}

func domainOf(addr string) string {
	addr = strings.ToLower(strings.TrimSpace(addr))
	if at := strings.LastIndex(addr, "@"); at >= 0 {
		return addr[at+1:]
	}
	return ""
}

func joinDomains(addrs []string) string {
	domains := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if d := domainOf(a); d != "" {
			domains = append(domains, d)
		}
	}
	return strings.Join(domains, ",")
}
