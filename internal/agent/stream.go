package agent

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"factor-lab/internal/engine"
	"factor-lab/internal/panel"
)

// AssistEvent 是流式辅助的一条推送。
type AssistEvent struct {
	Type     string         `json:"type"` // delta | retry | done | error
	Text     string         `json:"text,omitempty"`
	Attempt  int            `json:"attempt,omitempty"`
	Message  string         `json:"message,omitempty"`
	Reply    string         `json:"reply,omitempty"`
	Expr     string         `json:"expr,omitempty"`
	Combo    *ComboProposal `json:"combo,omitempty"`
	Valid    bool           `json:"valid"`
	Errors   []string       `json:"errors,omitempty"`
	Attempts int            `json:"attempts,omitempty"`
}

const (
	AssistEventDelta = "delta"
	AssistEventRetry = "retry"
	AssistEventDone  = "done"
	AssistEventError = "error"
)

// RunAssistStream 与 RunAssist 同一套校验/重试，但把 reply 字段（或纯文本）按增量推给 emit。
// emit 返回错误时中止（用于 WebSocket 断开或客户端 stop）。
func RunAssistStream(ctx context.Context, llm LLM, msgs []AssistMsg, currentExpr string, p *panel.Panel, emit func(AssistEvent) error) (*AssistResult, error) {
	return runAssist(ctx, llm, msgs, currentExpr, p, emit, true, AssistModeFactor, nil)
}

// RunComboAssistStream 与 RunComboAssist 同一套校验，组合方案随 done 事件推送。
func RunComboAssistStream(ctx context.Context, llm LLM, msgs []AssistMsg, current string, factors []FactorRef, emit func(AssistEvent) error) (*AssistResult, error) {
	return runAssist(ctx, llm, msgs, current, nil, emit, true, AssistModeCombo, factors)
}

func runAssist(ctx context.Context, llm LLM, msgs []AssistMsg, currentExpr string, p *panel.Panel, emit func(AssistEvent) error, stream bool, mode string, factors []FactorRef) (*AssistResult, error) {
	if emit == nil {
		emit = func(AssistEvent) error { return nil }
	}
	combo := mode == AssistModeCombo
	user := buildAssistUser(msgs, currentExpr)
	var sys string
	if combo {
		sys = comboSystemPrompt(factors)
	} else {
		sys = assistSystemPrompt(p)
	}
	res := &AssistResult{}
	feedback := ""
	for attempt := 1; attempt <= assistMaxRetries+1; attempt++ {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		res.Attempts = attempt
		if attempt > 1 {
			msg := "表达式校验未通过，正在重试"
			if combo {
				msg = "组合方案未通过校验，正在重试"
			}
			if err := emit(AssistEvent{Type: AssistEventRetry, Attempt: attempt, Message: msg}); err != nil {
				return res, err
			}
		}
		prompt := user + "\n\n上次尝试的反馈：\n" + feedback
		ex := newReplyExtractor()
		raw, err := completeLLM(ctx, llm, sys, prompt, stream, func(chunk string) error {
			if d := ex.Feed(chunk); d != "" {
				return emit(AssistEvent{Type: AssistEventDelta, Text: d})
			}
			return nil
		})
		if err != nil {
			return res, fmt.Errorf("LLM 调用失败: %w", err)
		}
		out, err := parseAssist(raw)
		if err != nil {
			format := assistJSONFormat
			if combo {
				format = comboJSONFormat
			}
			feedback = "JSON 解析失败，请严格输出 " + format
			res.Errors = []string{err.Error()}
			continue
		}
		res.Reply = out.Reply
		if combo {
			normalized, errs := validateCombo(out.Combo, factors)
			if normalized == nil && len(errs) == 0 {
				res.Combo = nil
				res.Expr = ""
				res.Valid = false
				res.Errors = nil
				return finishAssist(res, emit)
			}
			if len(errs) > 0 {
				res.Errors = errs
				feedback = "组合方案未通过校验：\n" + strings.Join(errs, "\n") + "\n请只使用因子库里的 id，至少 2 个可用因子，重新给出 combo。"
				continue
			}
			res.Combo = normalized
			res.Expr = ""
			res.Valid = true
			res.Errors = nil
			return finishAssist(res, emit)
		}
		res.Expr = strings.TrimSpace(out.Expr)
		if res.Expr == "" {
			res.Valid = false
			res.Errors = nil
			return finishAssist(res, emit)
		}
		if errs := engine.Check(res.Expr, p); len(errs) > 0 {
			res.Errors = errs
			feedback = "表达式校验未通过：\n" + strings.Join(res.Errors, "\n") + "\n请修正后重新给出完整表达式。"
			continue
		}
		res.Valid = true
		res.Errors = nil
		return finishAssist(res, emit)
	}
	return finishAssist(res, emit)
}

func finishAssist(res *AssistResult, emit func(AssistEvent) error) (*AssistResult, error) {
	err := emit(AssistEvent{
		Type:     AssistEventDone,
		Reply:    res.Reply,
		Expr:     res.Expr,
		Combo:    res.Combo,
		Valid:    res.Valid,
		Errors:   res.Errors,
		Attempts: res.Attempts,
	})
	return res, err
}

func buildAssistUser(msgs []AssistMsg, currentExpr string) string {
	var convo strings.Builder
	for i, m := range msgs {
		if i > 0 {
			convo.WriteString("\n")
		}
		role := "用户"
		if m.Role == "assistant" {
			role = "助手"
		}
		convo.WriteString(role)
		convo.WriteString("：")
		convo.WriteString(m.Content)
	}
	user := "【对话记录】\n" + convo.String()
	if strings.TrimSpace(currentExpr) != "" {
		user += "\n\n【当前上下文】\n" + currentExpr + "\n（修改类请求请基于它修改）"
	}
	user += "\n\n请按系统约定的 JSON 格式回复。"
	return user
}

func completeLLM(ctx context.Context, llm LLM, sys, user string, stream bool, onChunk func(string) error) (string, error) {
	if stream {
		if s, ok := llm.(Streamer); ok {
			return s.Stream(ctx, sys, user, onChunk)
		}
	}
	raw, err := llm.Chat(ctx, sys, user)
	if err != nil {
		return raw, err
	}
	if onChunk != nil && raw != "" {
		if err := onChunk(raw); err != nil {
			return raw, err
		}
	}
	return raw, nil
}

// replyExtractor 从模型增量输出里抽出给用户看的中文回复。
// JSON 模式只推 "reply" 字段的解码文本；纯文本则原样推。
type replyExtractor struct {
	raw   strings.Builder
	mode  int // 0 unknown, 1 json, 2 plain
	seen  string
	start int // json 字符串内容在 raw 中的起点；-1 未找到
}

func newReplyExtractor() *replyExtractor {
	return &replyExtractor{start: -1}
}

func (e *replyExtractor) Feed(chunk string) string {
	if chunk == "" {
		return ""
	}
	prev := e.raw.Len()
	e.raw.WriteString(chunk)
	s := e.raw.String()

	if e.mode == 0 {
		i := 0
		for i < len(s) {
			c := s[i]
			if c == ' ' || c == '\n' || c == '\r' || c == '\t' {
				i++
				continue
			}
			if c == '{' {
				e.mode = 1
			} else {
				e.mode = 2
			}
			break
		}
		if e.mode == 0 {
			return ""
		}
	}
	if e.mode == 2 {
		return s[prev:]
	}
	if e.start < 0 {
		e.start = findJSONReplyStart(s)
		if e.start < 0 {
			return ""
		}
	}
	decoded, _, _ := decodeJSONStringPrefix(s[e.start:])
	if len(decoded) <= len(e.seen) {
		return ""
	}
	delta := decoded[len(e.seen):]
	e.seen = decoded
	return delta
}

func findJSONReplyStart(s string) int {
	from := 0
	key := `"reply"`
	for {
		i := strings.Index(s[from:], key)
		if i < 0 {
			return -1
		}
		k := from + i + len(key)
		for k < len(s) && isJSONSpace(s[k]) {
			k++
		}
		if k >= len(s) || s[k] != ':' {
			from = from + i + 1
			continue
		}
		k++
		for k < len(s) && isJSONSpace(s[k]) {
			k++
		}
		if k >= len(s) {
			return -1
		}
		if s[k] == '"' {
			return k + 1
		}
		from = from + i + 1
	}
}

func isJSONSpace(c byte) bool {
	return c == ' ' || c == '\n' || c == '\r' || c == '\t'
}

func decodeJSONStringPrefix(s string) (decoded string, consumed int, closed bool) {
	var b strings.Builder
	i := 0
	for i < len(s) {
		c := s[i]
		if c == '"' {
			return b.String(), i, true
		}
		if c != '\\' {
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && size == 1 {
				return b.String(), i, false
			}
			b.WriteRune(r)
			i += size
			continue
		}
		if i+1 >= len(s) {
			return b.String(), i, false
		}
		n := s[i+1]
		if n == 'u' {
			if i+6 > len(s) {
				return b.String(), i, false
			}
			v, err := strconv.ParseUint(s[i+2:i+6], 16, 32)
			if err != nil {
				i += 6
				continue
			}
			b.WriteRune(rune(v))
			i += 6
			continue
		}
		switch n {
		case '"', '\\', '/':
			b.WriteByte(n)
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		default:
			b.WriteByte(n)
		}
		i += 2
	}
	return b.String(), i, false
}
