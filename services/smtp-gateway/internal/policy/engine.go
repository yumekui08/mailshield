// Package policy は YAML ルールファイルを読み込み、検査結果から
// アクション（deliver / reject / quarantine / approval）を決定する。
package policy

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

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

// PolicyRules は policy.yaml のトップレベル構造。
type PolicyRules struct {
	Rules []Rule `yaml:"rules"`
}

// Engine はポリシーエンジンの実装である。
type Engine struct {
	rules []Rule
}

// New は policy.yaml を読み込んで Engine を構築する。
func New(rulesFile string) (*Engine, error) {
	data, err := os.ReadFile(rulesFile)
	if err != nil {
		return nil, fmt.Errorf("policy.yaml 読み込み失敗 (%s): %w", rulesFile, err)
	}

	var pr PolicyRules
	if err := yaml.Unmarshal(data, &pr); err != nil {
		return nil, fmt.Errorf("policy.yaml パース失敗: %w", err)
	}

	return &Engine{rules: pr.Rules}, nil
}

// Evaluate は検査結果を評価してアクション種別とマッチしたルール名を返す。
// アクション（SMTP 配送・拒否等）の実行は行わない（シミュレーター用）。
func (e *Engine) Evaluate(results []*domain.InspectResult) (action ActionType, matchedRule string) {
	facts := buildFacts(results)
	for _, rule := range e.rules {
		matched, err := evaluate(rule.Condition, facts)
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
	facts := buildFacts(results)

	for _, rule := range e.rules {
		matched, err := evaluate(rule.Condition, facts)
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

// deliver は宛先 MTA へ SMTP でメールを送信する。
// ctx のデッドラインを TCP コネクションに伝播させることで、宛先 MTA がハングしても
// smtp.SendMail と異なり呼び出し元の goroutine がブロックし続けることを防ぐ。
func (e *Engine) deliver(ctx context.Context, mail *domain.Mail, destination string) error {
	if destination == "" {
		return fmt.Errorf("deliver アクションに destination が指定されていません")
	}

	// 宛先アドレスが host:port 形式でない場合は :25 を付加
	if !strings.Contains(destination, ":") {
		destination = destination + ":25"
	}

	host, _, err := net.SplitHostPort(destination)
	if err != nil {
		return fmt.Errorf("宛先アドレスのパース失敗 (%s): %w", destination, err)
	}

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", destination)
	if err != nil {
		return fmt.Errorf("SMTP 接続失敗 (destination=%s): %w", destination, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			_ = conn.Close()
			return fmt.Errorf("SMTP デッドライン設定失敗: %w", err)
		}
	}

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("SMTP クライアント作成失敗: %w", err)
	}
	defer c.Close()

	if err := c.Mail(mail.FromAddress); err != nil {
		return fmt.Errorf("MAIL FROM 失敗: %w", err)
	}
	for _, to := range mail.ToAddresses {
		if err := c.Rcpt(to); err != nil {
			return fmt.Errorf("RCPT TO 失敗 (%s): %w", to, err)
		}
	}
	wc, err := c.Data()
	if err != nil {
		return fmt.Errorf("DATA コマンド失敗: %w", err)
	}
	if _, err := wc.Write(mail.RawEML); err != nil {
		return fmt.Errorf("メールデータ送信失敗: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("DATA 完了失敗: %w", err)
	}
	if err := c.Quit(); err != nil {
		return fmt.Errorf("QUIT 失敗 (destination=%s, message_id=%s): %w",
			destination, mail.MessageID, err)
	}

	slog.Info("メール配送完了",
		"message_id", mail.MessageID,
		"destination", destination)
	return nil
}

// buildFacts は InspectResult の一覧から条件評価用のマップを構築する。
// キー形式: "{worker_name}.detected" / "{worker_name}.score"
func buildFacts(results []*domain.InspectResult) map[string]any {
	facts := make(map[string]any)
	for _, r := range results {
		facts[r.WorkerName+".detected"] = r.Detected
		facts[r.WorkerName+".score"] = r.Score
		for k, v := range r.Details {
			facts[r.WorkerName+"."+k] = v
		}
	}
	return facts
}

// evaluate はシンプルな条件式を評価する（フェーズ1用の最小実装）。
// 対応する条件:
//   - "true"
//   - "{key} == {value}"
//   - "{key} >= {int}"
func evaluate(condition string, facts map[string]any) (bool, error) {
	condition = strings.TrimSpace(condition)

	if condition == "true" {
		return true, nil
	}
	if condition == "false" {
		return false, nil
	}

	// "{key} == {value}" の評価（ブール値は大文字小文字を区別しない）
	if parts := strings.SplitN(condition, " == ", 2); len(parts) == 2 {
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		fact, ok := facts[key]
		if !ok {
			return false, nil
		}
		return strings.EqualFold(fmt.Sprintf("%v", fact), val), nil
	}

	// "{key} >= {int}" の評価
	if parts := strings.SplitN(condition, " >= ", 2); len(parts) == 2 {
		key := strings.TrimSpace(parts[0])
		var threshold int
		if _, err := fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &threshold); err != nil {
			return false, fmt.Errorf("threshold パース失敗: %w", err)
		}
		fact, ok := facts[key]
		if !ok {
			return false, nil
		}
		switch v := fact.(type) {
		case int:
			return v >= threshold, nil
		case float64:
			return int(v) >= threshold, nil
		}
		return false, nil
	}

	return false, fmt.Errorf("未対応の条件式: %s", condition)
}
