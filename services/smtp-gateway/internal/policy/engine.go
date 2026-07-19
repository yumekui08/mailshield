// Package policy は YAML ルールファイルを読み込み、検査結果から
// アクション（deliver / reject / quarantine / approval）を決定する。
package policy

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/eml"
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
	// 終端アクション: 適用するとルール評価を停止する。
	ActionDeliver    ActionType = "deliver"
	ActionReject     ActionType = "reject"
	ActionQuarantine ActionType = "quarantine"
	ActionApproval   ActionType = "approval"
	ActionDelay      ActionType = "delay"
	// ActionRedirect は宛先を差し替えて配送する終端アクション（誤送信の受け皿等）。
	ActionRedirect ActionType = "redirect"

	// 非終端アクション: メールを加工して次のルール評価へ続行する。
	ActionAddHeader        ActionType = "add_header"
	ActionRemoveHeader     ActionType = "remove_header"
	ActionAddSubjectPrefix ActionType = "add_subject_prefix"
	ActionStripAttachments ActionType = "strip_attachments"
	ActionLogOnly          ActionType = "log_only"
)

// isTerminalAction は指定アクションが終端（ルール評価を止める）かを返す。
func isTerminalAction(t string) bool {
	switch ActionType(t) {
	case ActionDeliver, ActionReject, ActionQuarantine, ActionApproval, ActionDelay, ActionRedirect:
		return true
	}
	return false
}

// isKnownAction は指定アクションがサポートされているかを返す。
func isKnownAction(t string) bool {
	switch ActionType(t) {
	case ActionDeliver, ActionReject, ActionQuarantine, ActionApproval, ActionDelay, ActionRedirect,
		ActionAddHeader, ActionRemoveHeader, ActionAddSubjectPrefix, ActionStripAttachments, ActionLogOnly:
		return true
	}
	return false
}

// ActionSpec は 1 つのアクションとそのパラメータを表す（actions: リスト要素）。
type ActionSpec struct {
	Type string `yaml:"type"`
	// Destination は deliver 時の宛先（deliverer 名 または host:port）。
	Destination string `yaml:"destination"`
	// DelayMinutes は delay 時の保留分数（0 以下は既定 5 分）。
	DelayMinutes int `yaml:"delay_minutes"`
	// Name は add_header / remove_header のヘッダー名。
	Name string `yaml:"name"`
	// Value は add_header / add_subject_prefix の値。
	Value string `yaml:"value"`
}

// Rule は policy.yaml の単一ルールを表す。
type Rule struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	// Enabled が false のルールは評価対象から除外される（nil / 省略時は有効）。
	Enabled *bool `yaml:"enabled"`
	// Priority は評価順（昇順・小さいほど先）。同値はファイル定義順を保持する。
	Priority  int      `yaml:"priority"`
	Tags      []string `yaml:"tags"`
	Condition string   `yaml:"condition"`

	// 単一アクション（後方互換）。actions: が指定されている場合はそちらを優先する。
	Action      string `yaml:"action"`
	Destination string `yaml:"destination"` // deliver 時の宛先（host:port）
	// DelayMinutes は delay アクション時の保留時間（分）。0 以下の場合は既定 5 分。
	DelayMinutes int `yaml:"delay_minutes"`
	// Value は単一 action: 形式での redirect（差し替え先）/ add_subject_prefix の値。
	// ヘッダー名が必要なアクション（add_header 等）は actions: リスト形式を使う
	//（トップレベル name: はルール名に予約済みのため）。
	Value string `yaml:"value"`

	// Actions は複数アクション（非終端アクション + 終端アクション）を順に適用する。
	Actions []ActionSpec `yaml:"actions"`
}

// specs はルールのアクションを ActionSpec のスライスに正規化する。
// actions: が指定されていればそれを、なければ単一 action: を 1 要素として返す。
func (r *Rule) specs() []ActionSpec {
	if len(r.Actions) > 0 {
		return r.Actions
	}
	if r.Action != "" {
		return []ActionSpec{{
			Type:         r.Action,
			Destination:  r.Destination,
			DelayMinutes: r.DelayMinutes,
			Value:        r.Value,
		}}
	}
	return nil
}

// isEnabled はルールが有効か（enabled 省略時は有効）を返す。
func (r *Rule) isEnabled() bool {
	return r.Enabled == nil || *r.Enabled
}

// ActionResult は EvaluateAndAct が返すアクションと付随パラメータ。
type ActionResult struct {
	Action ActionType
	// DelayMinutes は Action が delay のときの保留時間（分）。
	DelayMinutes int
	// MatchedRule は終端アクションを決定したルール名（ヒット計測・ログ用）。
	MatchedRule string
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

	rules, err := prepareRules(pr.Rules)
	if err != nil {
		return nil, fmt.Errorf("policy.yaml (%s): %w", rulesFile, err)
	}

	return &Engine{rules: rules, lists: lists, deliverer: deliverer}, nil
}

// NewFromContent は policy.yaml と同形の内容（バイト列）からエンジンを構築する（ADR 008 ③-2b・
// DB ポリシーインスタンス用）。内容が空なら既定（全配送）。名前付きリストはインライン定義のみ
// 対応（file: 参照は未対応）。
func NewFromContent(content []byte, deliverer Deliverer) (*Engine, error) {
	if len(strings.TrimSpace(string(content))) == 0 {
		return New("", deliverer)
	}
	var pr PolicyRules
	if err := yaml.Unmarshal(content, &pr); err != nil {
		return nil, fmt.Errorf("ポリシー内容のパース失敗: %w", err)
	}
	lists, err := loadLists(pr.Lists, "")
	if err != nil {
		return nil, err
	}
	rules, err := prepareRules(pr.Rules)
	if err != nil {
		return nil, fmt.Errorf("ポリシー内容の検証失敗: %w", err)
	}
	return &Engine{rules: rules, lists: lists, deliverer: deliverer}, nil
}

// prepareRules は enabled=false を除外し、priority 昇順（同値はファイル順）に安定ソートし、
// 各ルールのアクション種別を検証する。
func prepareRules(raw []Rule) ([]Rule, error) {
	var rules []Rule
	for _, r := range raw {
		if !r.isEnabled() {
			continue
		}
		if err := ValidateCondition(r.Condition); err != nil {
			return nil, fmt.Errorf("ルール %q の条件式が不正です: %w", r.Name, err)
		}
		specs := r.specs()
		if len(specs) == 0 {
			return nil, fmt.Errorf("ルール %q にアクションがありません", r.Name)
		}
		for _, s := range specs {
			if !isKnownAction(s.Type) {
				return nil, fmt.Errorf("ルール %q の未知のアクション: %q", r.Name, s.Type)
			}
		}
		rules = append(rules, r)
	}
	sort.SliceStable(rules, func(i, j int) bool {
		return rules[i].Priority < rules[j].Priority
	})
	return rules, nil
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

// decide はルールを優先度順に評価する。マッチしたルールの非終端アクション
// （add_header / add_subject_prefix / remove_header / log_only）は mail を加工して
// 次のルール評価へ続行し、最初の終端アクション（deliver/reject/quarantine/approval/delay）
// で停止してそのルールと ActionSpec を返す。
// 非終端アクションによる mail の変更は後続ルールの条件評価にも反映される。
// 終端アクションに到達しなかった場合は matched=false を返す。
func (e *Engine) decide(mail *domain.Mail, results []*domain.InspectResult) (rule *Rule, terminal ActionSpec, matched bool) {
	ec := evalContext{facts: buildFacts(mail, results), lists: e.lists}
	for i := range e.rules {
		r := &e.rules[i]
		ok, err := evalCondition(r.Condition, ec)
		if err != nil {
			slog.Warn("ルール評価エラー（スキップ）",
				"rule", r.Name, "message_id", mail.MessageID, "error", err)
			continue
		}
		if !ok {
			continue
		}

		mutated := false
		for _, spec := range r.specs() {
			if isTerminalAction(spec.Type) {
				return r, spec, true
			}
			if e.applyNonTerminal(mail, r, spec) {
				mutated = true
			}
		}
		// このルールは非終端アクションのみ → 変更を反映して次のルールへ続行
		if mutated {
			ec.facts = buildFacts(mail, results)
		}
	}
	return nil, ActionSpec{}, false
}

// applyNonTerminal は非終端アクションを mail に適用する。mail を変更した場合 true を返す。
func (e *Engine) applyNonTerminal(mail *domain.Mail, rule *Rule, spec ActionSpec) bool {
	switch ActionType(spec.Type) {
	case ActionAddSubjectPrefix:
		mail.RawEML = eml.PrependSubjectPrefix(mail.RawEML, spec.Value)
		mail.Subject = spec.Value + mail.Subject
		slog.Info("ポリシー: 件名プレフィックス付与",
			"rule", rule.Name, "message_id", mail.MessageID, "prefix", spec.Value)
		return true
	case ActionAddHeader:
		mail.RawEML = eml.AddHeaderTop(mail.RawEML, spec.Name, spec.Value)
		slog.Info("ポリシー: ヘッダー追加",
			"rule", rule.Name, "message_id", mail.MessageID, "header", spec.Name)
		return true
	case ActionRemoveHeader:
		mail.RawEML = eml.RemoveHeader(mail.RawEML, spec.Name)
		slog.Info("ポリシー: ヘッダー削除",
			"rule", rule.Name, "message_id", mail.MessageID, "header", spec.Name)
		return true
	case ActionStripAttachments:
		var exts []string
		if v := strings.TrimSpace(spec.Value); v != "" {
			exts = strings.Split(v, ",")
		}
		out, changed, err := eml.StripAttachments(
			mail.RawEML, mail.FromAddress, mail.ToAddresses, mail.Subject, mail.ReceivedAt, exts)
		if err != nil {
			slog.Warn("ポリシー: 添付除去失敗（続行）",
				"rule", rule.Name, "message_id", mail.MessageID, "error", err)
			return false
		}
		if !changed {
			return false
		}
		mail.RawEML = out
		mail.HasAttachment = false
		slog.Info("ポリシー: 添付ファイル除去",
			"rule", rule.Name, "message_id", mail.MessageID, "extensions", spec.Value)
		return true
	case ActionLogOnly:
		slog.Info("ポリシー: log_only マッチ",
			"rule", rule.Name, "message_id", mail.MessageID)
		return false
	}
	return false
}

// Evaluate は検査結果とメール属性を評価してアクション種別とマッチしたルール名を返す。
// 終端アクション（SMTP 配送・拒否等）の実行は行わないが、非終端アクション
// （タグ付け等）は mail に適用されるため、シミュレーターは加工後の EML を報告できる。
func (e *Engine) Evaluate(mail *domain.Mail, results []*domain.InspectResult) (action ActionType, matchedRule string) {
	rule, spec, ok := e.decide(mail, results)
	if !ok {
		return "", ""
	}
	return ActionType(spec.Type), rule.Name
}

// EvaluateAndAct は検査結果を評価してアクションを実行し、実行したアクションと
// 付随パラメータ（delay 時の保留分数など）を返す。
// 非終端アクションは mail を加工しつつ次のルールへ続行し、最初の終端アクションで停止する。
// マッチする終端ルールがない場合は空の ActionResult と nil を返す。
func (e *Engine) EvaluateAndAct(ctx context.Context, mail *domain.Mail, results []*domain.InspectResult) (ActionResult, error) {
	rule, spec, ok := e.decide(mail, results)
	if !ok {
		slog.Warn("マッチするルールがありません（デフォルト配送なし）",
			"message_id", mail.MessageID)
		return ActionResult{}, nil
	}

	action := ActionType(spec.Type)
	slog.Info("ポリシーマッチ",
		"rule", rule.Name, "action", spec.Type, "message_id", mail.MessageID)

	switch action {
	case ActionDeliver:
		if err := e.deliver(ctx, mail, spec.Destination); err != nil {
			return ActionResult{}, err
		}
		return ActionResult{Action: ActionDeliver, MatchedRule: rule.Name}, nil
	case ActionRedirect:
		// 宛先を差し替えて配送する（誤送信の受け皿・監査用アドレス等）。
		// value に差し替え先アドレス（カンマ区切りで複数可）を指定する。
		targets := splitAddresses(spec.Value)
		if len(targets) == 0 {
			return ActionResult{}, fmt.Errorf("redirect アクションに value（差し替え先アドレス）が必要です")
		}
		mail.ToAddresses = targets
		slog.Info("ポリシー: 宛先を差し替えて配送", "message_id", mail.MessageID, "rule", rule.Name, "redirect_to", targets)
		if err := e.deliver(ctx, mail, spec.Destination); err != nil {
			return ActionResult{}, err
		}
		return ActionResult{Action: ActionRedirect, MatchedRule: rule.Name}, nil
	case ActionReject:
		slog.Info("メール拒否", "message_id", mail.MessageID, "rule", rule.Name)
		return ActionResult{Action: ActionReject, MatchedRule: rule.Name}, nil
	case ActionQuarantine:
		slog.Info("メール隔離", "message_id", mail.MessageID, "rule", rule.Name)
		return ActionResult{Action: ActionQuarantine, MatchedRule: rule.Name}, nil
	case ActionApproval:
		slog.Info("承認フロー保留", "message_id", mail.MessageID, "rule", rule.Name)
		return ActionResult{Action: ActionApproval, MatchedRule: rule.Name}, nil
	case ActionDelay:
		delay := spec.DelayMinutes
		if delay <= 0 {
			delay = 5
		}
		slog.Info("送信ディレイ保留", "message_id", mail.MessageID, "rule", rule.Name, "delay_minutes", delay)
		return ActionResult{Action: ActionDelay, DelayMinutes: delay, MatchedRule: rule.Name}, nil
	default:
		return ActionResult{}, fmt.Errorf("未知のアクション: %s", spec.Type)
	}
}

// Rules は現在のルール一覧（優先度順・enabled のみ）を返す。ヒット件数の初期化等に使う。
func (e *Engine) Rules() []Rule {
	return e.rules
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
		facts["mail.recipient_count"] = len(mail.ToAddresses)
		if !mail.ReceivedAt.IsZero() {
			facts["mail.hour"] = mail.ReceivedAt.UTC().Hour()
			facts["mail.weekday"] = weekdayShort(mail.ReceivedAt.UTC().Weekday())
		}
		// ヘッダー由来の fact（mail.has_header.<name> / mail.header.<name>）。
		for name, val := range parseHeaderFacts(mail.RawEML) {
			facts["mail.has_header."+name] = true
			facts["mail.header."+name] = val
		}
	}
	return facts
}

// weekdayShort は曜日を小文字 3 文字（sun..sat）で返す（条件式での比較用）。
func weekdayShort(d time.Weekday) string {
	return [...]string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}[d]
}

// parseHeaderFacts は EML のヘッダーブロックを走査し、ヘッダー名（小文字）→ 値のマップを返す。
// 折り畳み継続行を結合する。同名ヘッダーは最初の 1 つを採用する。本文は読まない。
func parseHeaderFacts(rawEML []byte) map[string]string {
	out := make(map[string]string)
	s := string(rawEML)
	// ヘッダー部（最初の空行まで）を切り出す
	if i := strings.Index(s, "\r\n\r\n"); i >= 0 {
		s = s[:i]
	} else if i := strings.Index(s, "\n\n"); i >= 0 {
		s = s[:i]
	}
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	curName, curVal := "", ""
	commit := func() {
		if curName != "" {
			if _, exists := out[curName]; !exists {
				out[curName] = strings.TrimSpace(curVal)
			}
		}
	}
	for _, line := range lines {
		if line == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			curVal += " " + strings.TrimSpace(line) // 折り畳み継続行
			continue
		}
		commit()
		if colon := strings.IndexByte(line, ':'); colon >= 0 {
			curName = strings.ToLower(strings.TrimSpace(line[:colon]))
			curVal = line[colon+1:]
		} else {
			curName, curVal = "", ""
		}
	}
	commit()
	return out
}

func domainOf(addr string) string {
	addr = strings.ToLower(strings.TrimSpace(addr))
	if at := strings.LastIndex(addr, "@"); at >= 0 {
		return addr[at+1:]
	}
	return ""
}

// splitAddresses はカンマ区切りのアドレス文字列を配列に分解する（空要素は除外）。
func splitAddresses(s string) []string {
	var out []string
	for _, a := range strings.Split(s, ",") {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
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
