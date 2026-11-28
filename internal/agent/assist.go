// Assist：对话式研究助手。
//
// 因子工厂：人机协作写 DSL。LLM 回复思路并生成表达式；引擎做字段/算子校验，
// 失败则带错误重试；校验通过的表达式由用户确认后才写入编辑器。
// 因子组合：LLM 只从现有因子库挑 id 做线性组合，禁止写新表达式；校验通过后
// 方案直接写入组合编辑器。
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"factor-lab/internal/ops"
	"factor-lab/internal/panel"
)

// AssistMsg 是一条对话消息。
type AssistMsg struct {
	Role    string `json:"role"` // user | assistant
	Content string `json:"content"`
}

// AssistResult 是一次辅助的结果。
type AssistResult struct {
	Reply    string         `json:"reply"`
	Expr     string         `json:"expr,omitempty"`  // 因子工厂：候选 DSL
	Combo    *ComboProposal `json:"combo,omitempty"` // 因子组合：从现有因子库挑出的方案
	Valid    bool           `json:"valid"`
	Errors   []string       `json:"errors,omitempty"`
	Attempts int            `json:"attempts"`
}

// FactorRef 是组合助手看到的因子库条目（只给 id/名称/指标，不给表达式）。
type FactorRef struct {
	ID        int
	Name      string
	Category  string
	RankIC    float64
	ICIR      float64
	Available bool
}

// ComboProposal 是从现有因子构建的组合方案。
type ComboProposal struct {
	Name   string              `json:"name"`
	Method string              `json:"method"` // equal | manual
	Items  []ComboProposalItem `json:"items"`
}

// ComboProposalItem 是组合里对一个已有因子的引用。
type ComboProposalItem struct {
	ID        int     `json:"id"`
	Name      string  `json:"name"`
	Weight    float64 `json:"weight"`
	Direction int     `json:"direction"`
}

type assistOut struct {
	Reply string         `json:"reply"`
	Expr  string         `json:"expr"`
	Combo *ComboProposal `json:"combo"`
}

const assistJSONFormat = `{"reply": "给用户的中文回复（解释思路或修改点）", "expr": "完整的因子表达式；仅当需要生成或修改表达式时给出，否则为空字符串"}`

const comboJSONFormat = `{"reply": "给用户的中文回复（说明为何选这些因子、怎么配）", "combo": {"name": "组合名称", "method": "equal 或 manual", "items": [{"id": 1, "weight": 1, "direction": 1}]}}`

const assistMaxRetries = 2

const (
	AssistModeFactor = "factor"
	AssistModeCombo  = "combo"
)

func assistSystemPrompt(p *panel.Panel, catalogInPrompt bool) string {
	var sb strings.Builder
	sb.WriteString(`你是 A 股量化因子研究助手，正在与用户对话式编写因子。
用户可能是量化新手：回复要用平实的中文解释思路；涉及术语时顺带一句话解释。

DSL 规则（必须严格遵守）：
- 每行一个赋值：变量 = 表达式；最后一行是因子值（会被引擎取用）。
- ts_ 前缀算子沿单只股票时间轴滚动，窗口长度必须显式给出（1~512 的整数）。
- cs_ 前缀算子在每个交易日的横截面上计算。
- 算术运算 + - * /；比较返回 0/1；if(cond, a, b) 做条件选择。
- 只能使用工具返回的字段与算子，不得臆造。写表达式前先调用 list_fields 与 list_ops（一次即完整目录）；提交前用 check_expr 自检。

输出格式：严格 JSON（不要 markdown 围栏），` + assistJSONFormat + `
expr 的要求：完整可独立运行（包含全部中间变量与赋值行，换行用 \n）；若用户只是提问、没有要求表达式，则 expr 给空字符串。
修改请求：基于「当前编辑器表达式」改，不要丢掉用户已有的中间变量。`)

	if catalogInPrompt && p != nil {
		sb.WriteString("\n\n可用字段（面板列）：\n")
		sb.WriteString(strings.Join(p.Fields(), ", "))
		sb.WriteString("\n\n可用算子：\n")
		for name, sig := range ops.Sigs() {
			sb.WriteString(fmt.Sprintf("- %s/%d", name, sig.MinArgs))
		}
		sb.WriteString("\n\n字段语义：close 为后复权收盘价；pct_chg 为日收益率；vol 成交量；amount 成交额；turnover_rate / turnover_rate_f 换手率(%)；volume_ratio 量比；pe / pe_ttm / pb / ps / ps_ttm 估值(倍)；dv_ratio / dv_ttm 股息率(%)；total_share / float_share / free_share 股本(万股)；total_mv / circ_mv 市值(万元)；limit_status 收盘涨跌状态(0–6)；roe 为 ROE（PIT 口径，百分数）。")
	}
	return sb.String()
}

// RunAssist 跑一轮对话辅助（非流式，给 HTTP / 测试用）。
func RunAssist(ctx context.Context, llm LLM, msgs []AssistMsg, currentExpr string, p *panel.Panel) (*AssistResult, error) {
	return runAssist(ctx, llm, msgs, currentExpr, p, nil, false, AssistModeFactor, nil)
}

// RunComboAssist 从现有因子库构建组合（不写新 DSL）。
func RunComboAssist(ctx context.Context, llm LLM, msgs []AssistMsg, current string, factors []FactorRef) (*AssistResult, error) {
	return runAssist(ctx, llm, msgs, current, nil, nil, false, AssistModeCombo, factors)
}

func parseAssist(raw string) (*assistOut, error) {
	obj := extractJSON(raw)
	if obj == "" {
		// 容错：纯文本回复也接受（当纯对话处理）
		return &assistOut{Reply: strings.TrimSpace(raw)}, nil
	}
	var out assistOut
	if err := json.Unmarshal([]byte(obj), &out); err != nil {
		return nil, fmt.Errorf("JSON 解析失败: %w", err)
	}
	if strings.TrimSpace(out.Reply) == "" && strings.TrimSpace(out.Expr) == "" && (out.Combo == nil || len(out.Combo.Items) == 0) {
		return nil, fmt.Errorf("reply、expr 与 combo 均为空")
	}
	return &out, nil
}

func comboSystemPrompt(factors []FactorRef, catalogInPrompt bool) string {
	var sb strings.Builder
	sb.WriteString(`你是 A 股量化因子组合助手。任务是从现有因子库里挑选因子，做成线性组合，而不是写新的因子表达式。

规则：
- 只能使用 list_factors 返回的因子 id，禁止发明因子、禁止输出 DSL / expr。挑选前先调用 list_factors（一次即完整目录）；提交前用 check_combo 自检。
- 至少 2 个、建议 3～5 个经济含义互补、低相关的因子。
- 不可用的因子不能选。
- method 只能是 equal（等权）或 manual（自定义权重，weight>0）。
- direction：1 正向，-1 反向（RankIC 为负且经济含义是「越小越好」时用反向）。
- 用户只是提问、不要求改组合时，combo 省略或 items 为空。

输出格式：严格 JSON（不要 markdown 围栏），` + comboJSONFormat)

	if catalogInPrompt {
		sb.WriteString("\n\n因子库：\n")
		if len(factors) == 0 {
			sb.WriteString("（空）\n")
		}
		for _, f := range factors {
			avail := "可用"
			if !f.Available {
				avail = "不可用"
			}
			line := fmt.Sprintf("id=%d %s（%s）%s", f.ID, f.Name, f.Category, avail)
			if f.RankIC != 0 || f.ICIR != 0 {
				line += fmt.Sprintf(" RankIC %.3f ICIR %.2f", f.RankIC, f.ICIR)
			}
			sb.WriteString("- ")
			sb.WriteString(line)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func validateCombo(in *ComboProposal, catalog []FactorRef) (*ComboProposal, []string) {
	if in == nil || len(in.Items) == 0 {
		return nil, nil
	}
	byID := make(map[int]FactorRef, len(catalog))
	for _, f := range catalog {
		byID[f.ID] = f
	}
	var errs []string
	method := strings.ToLower(strings.TrimSpace(in.Method))
	if method == "" {
		method = "equal"
	}
	if method != "equal" && method != "manual" {
		errs = append(errs, "method 只能是 equal 或 manual")
	}
	seen := map[int]bool{}
	items := make([]ComboProposalItem, 0, len(in.Items))
	for i, it := range in.Items {
		if seen[it.ID] {
			errs = append(errs, fmt.Sprintf("因子 id=%d 重复", it.ID))
			continue
		}
		seen[it.ID] = true
		f, ok := byID[it.ID]
		if !ok {
			errs = append(errs, fmt.Sprintf("因子 id=%d 不存在于因子库", it.ID))
			continue
		}
		if !f.Available {
			errs = append(errs, fmt.Sprintf("「%s」当前不可用", f.Name))
			continue
		}
		w := it.Weight
		if method == "equal" || w == 0 {
			w = 1
		}
		if w < 0 {
			errs = append(errs, fmt.Sprintf("第 %d 个因子权重不能为负", i+1))
			continue
		}
		dir := it.Direction
		if dir == 0 {
			dir = 1
		}
		if dir != 1 && dir != -1 {
			errs = append(errs, fmt.Sprintf("「%s」direction 只能是 1 或 -1", f.Name))
			continue
		}
		items = append(items, ComboProposalItem{ID: f.ID, Name: f.Name, Weight: w, Direction: dir})
	}
	if len(items) < 2 && len(errs) == 0 {
		errs = append(errs, "组合至少需要 2 个因子")
	}
	if len(errs) > 0 {
		return nil, errs
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "未命名组合"
	}
	return &ComboProposal{Name: name, Method: method, Items: items}, nil
}
