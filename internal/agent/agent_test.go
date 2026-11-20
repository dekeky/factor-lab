package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"factor-lab/internal/data"
)

// fakeLLM 是测试用的确定性模型：模拟真实端点返回的 JSON / 纯文本两类回复。
type fakeLLM struct{}

func (f *fakeLLM) Name() string { return "fake" }

func (f *fakeLLM) Chat(ctx context.Context, system, user string) (string, error) {
	if strings.Contains(user, "【对话记录】") {
		return `{"reply": "低波动因子：取 20 日日收益率标准差的负排名。", "expr": "vol20 = ts_std_dev(pct_chg, 20)\nalpha = -1 * cs_rank(vol20)"}`, nil
	}
	return "纯文本说明：IC 是因子值与下期收益的秩相关。", nil
}

// plainLLM 模拟模型只解释、不给表达式的场景。
type plainLLM struct{}

func (p *plainLLM) Name() string { return "plain" }

func (p *plainLLM) Chat(ctx context.Context, system, user string) (string, error) {
	return "这是一个解释性回复，不含表达式。", nil
}

func TestExtractJSON(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`{"expr": "a"}`, `{"expr": "a"}`},
		{"说明文字\n```json\n{\"a\": \"嵌套\"}\n```\n收尾", `{"a": "嵌套"}`},
		{`{"a": {"b": 1}}`, `{"a": {"b": 1}}`},
		{`没有 JSON`, ``},
		{`{"unterminated": 1`, ``},
	}
	for _, c := range cases {
		got := extractJSON(c.in)
		if got != c.want {
			t.Errorf("extractJSON(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}

func TestRunAssist(t *testing.T) {
	src := &data.SyntheticSource{Days: 120, Stocks: 30, Seed: 7}
	p, err := src.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	llm := &fakeLLM{}

	// ① 生成类请求：应产出通过校验的表达式
	res, err := RunAssist(context.Background(), llm, []AssistMsg{
		{Role: "user", Content: "帮我写一个低波因子"},
	}, "", p)
	if err != nil {
		t.Fatal(err)
	}
	if res.Reply == "" {
		t.Error("reply 为空")
	}
	if res.Expr == "" {
		t.Fatal("生成类请求应返回表达式")
	}
	if !res.Valid {
		t.Fatalf("表达式应通过校验: %v", res.Errors)
	}

	// ② 纯文本回复：不应强行给表达式
	res2, err := RunAssist(context.Background(), &plainLLM{}, []AssistMsg{
		{Role: "user", Content: "帮我解释一下 IC 是什么意思"},
	}, "", p)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Reply == "" {
		t.Error("纯提问也应有回复")
	}
	if res2.Expr != "" || res2.Valid {
		t.Errorf("纯提问不应产出表达式: %q", res2.Expr)
	}

	// ③ 多轮对话上下文应被带入（带当前表达式修改）
	res3, err := RunAssist(context.Background(), llm, []AssistMsg{
		{Role: "user", Content: "写一个低波因子"},
		{Role: "assistant", Content: "好，我给出了 20 日波动率因子"},
		{Role: "user", Content: "保持思路，重新生成"},
	}, "alpha = -1 * cs_rank(ts_std_dev(pct_chg, 20))", p)
	if err != nil {
		t.Fatal(err)
	}
	if res3.Expr == "" || !res3.Valid {
		t.Fatalf("多轮修改应产出有效表达式: %v", res3.Errors)
	}
}

func TestSettings(t *testing.T) {
	// 本机端点（如 Ollama）允许无密钥直接构造
	def := DefaultSettings()
	def.BaseURL = "http://127.0.0.1:11434/v1"
	def = def.ApplyFlat()
	if err := def.Validate(); err != nil {
		t.Fatalf("默认配置不合法: %v", err)
	}
	if _, err := def.NewLLM(context.Background()); err != nil {
		t.Fatalf("本机端点应允许无密钥构造: %v", err)
	}

	// 非本机端点必须提供密钥
	s := Settings{BaseURL: "https://api.example.com/v1", Model: "m", Temperature: 0.3, TimeoutSec: 30}
	if _, err := s.NewLLM(context.Background()); err == nil {
		t.Fatal("远程端点无密钥应报错")
	}

	// 非法 base_url / 温度越界
	if err := (Settings{BaseURL: "ftp://x", Model: "m"}).Normalize().Validate(); err == nil {
		t.Error("ftp:// 应被拒绝")
	}
	if err := (Settings{BaseURL: "https://a.com/v1", Model: "m", Temperature: 3}).Normalize().Validate(); err != nil {
		t.Errorf("温度越界应被 Normalize 夹回: %v", err)
	}

	got := (Settings{TushareToken: "  abc  "}).Normalize()
	if got.TushareToken != "abc" {
		t.Errorf("Tushare token 应去掉空白，实际 %q", got.TushareToken)
	}
}

type comboLLM struct {
	reply string
}

func (c *comboLLM) Name() string { return "combo-fake" }

func (c *comboLLM) Chat(ctx context.Context, system, user string) (string, error) {
	if !strings.Contains(system, "现有因子") && !strings.Contains(system, "因子库") {
		return "", fmt.Errorf("组合助手 prompt 应基于现有因子库，system=%s", system[:min(120, len(system))])
	}
	return c.reply, nil
}

type seqLLM struct {
	replies []string
	i       int
}

func (s *seqLLM) Name() string { return "seq-fake" }

func (s *seqLLM) Chat(ctx context.Context, system, user string) (string, error) {
	if s.i >= len(s.replies) {
		return s.replies[len(s.replies)-1], nil
	}
	r := s.replies[s.i]
	s.i++
	return r, nil
}

func testFactorRefs() []FactorRef {
	return []FactorRef{
		{ID: 1, Name: "短期反转", Category: "reversal", Available: true},
		{ID: 2, Name: "低波动", Category: "volatility", Available: true},
		{ID: 3, Name: "ROE质量", Category: "quality", Available: true},
		{ID: 4, Name: "停用动量", Category: "momentum", Available: false},
	}
}

func TestRunComboAssistPicksExistingFactors(t *testing.T) {
	llm := &comboLLM{reply: `{"reply":"选了低相关的反转与低波。","combo":{"name":"反转+低波","method":"equal","items":[{"id":1,"weight":1,"direction":1},{"id":2,"weight":1,"direction":-1}]}}`}
	res, err := RunComboAssist(context.Background(), llm, []AssistMsg{
		{Role: "user", Content: "帮我挑几个互补因子做组合"},
	}, "组合「未命名组合」（等权）\n【已选因子】0 个", testFactorRefs())
	if err != nil {
		t.Fatal(err)
	}
	if res.Expr != "" {
		t.Fatalf("组合助手不应产出新表达式: %q", res.Expr)
	}
	if !res.Valid || res.Combo == nil {
		t.Fatalf("应产出有效组合: valid=%v errors=%v", res.Valid, res.Errors)
	}
	if res.Combo.Name != "反转+低波" || res.Combo.Method != "equal" {
		t.Fatalf("组合元数据不对: %+v", res.Combo)
	}
	if len(res.Combo.Items) != 2 {
		t.Fatalf("应有 2 个成分: %+v", res.Combo.Items)
	}
	if res.Combo.Items[0].Name != "短期反转" || res.Combo.Items[1].Name != "低波动" {
		t.Fatalf("成分名应从因子库回填: %+v", res.Combo.Items)
	}
	if res.Combo.Items[1].Direction != -1 {
		t.Fatalf("应保留反向: %+v", res.Combo.Items[1])
	}
}

func TestRunComboAssistRejectsUnknownAndUnavailable(t *testing.T) {
	llm := &comboLLM{reply: `{"reply":"用了库外因子。","combo":{"name":"坏组合","method":"equal","items":[{"id":99,"weight":1,"direction":1},{"id":4,"weight":1,"direction":1}]}}`}
	res, err := RunComboAssist(context.Background(), llm, []AssistMsg{
		{Role: "user", Content: "随便组两个"},
	}, "", testFactorRefs())
	if err != nil {
		t.Fatal(err)
	}
	if res.Valid {
		t.Fatalf("未知/不可用因子应判无效: %+v", res.Combo)
	}
	if len(res.Errors) == 0 {
		t.Fatal("应返回校验错误")
	}
}

func TestRunComboAssistNeedsTwoFactors(t *testing.T) {
	llm := &comboLLM{reply: `{"reply":"只选了一个。","combo":{"name":"单因子","method":"equal","items":[{"id":1,"weight":1,"direction":1}]}}`}
	res, err := RunComboAssist(context.Background(), llm, []AssistMsg{
		{Role: "user", Content: "组一个组合"},
	}, "", testFactorRefs())
	if err != nil {
		t.Fatal(err)
	}
	if res.Valid || res.Combo != nil && len(res.Combo.Items) >= 2 {
		t.Fatalf("少于 2 个因子不应通过: %+v", res)
	}
}

func TestRunComboAssistRetryUnknownID(t *testing.T) {
	llm := &seqLLM{replies: []string{
		`{"reply":"先给一个不存在的。","combo":{"name":"坏","method":"equal","items":[{"id":99,"weight":1,"direction":1},{"id":1,"weight":1,"direction":1}]}}`,
		`{"reply":"已改用库内因子。","combo":{"name":"反转+低波","method":"equal","items":[{"id":1,"weight":1,"direction":1},{"id":2,"weight":1,"direction":1}]}}`,
	}}
	res, err := RunComboAssist(context.Background(), llm, []AssistMsg{
		{Role: "user", Content: "组两个因子"},
	}, "", testFactorRefs())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid || res.Combo == nil || len(res.Combo.Items) != 2 {
		t.Fatalf("未知 id 应触发重试并改用库内因子: %+v %v", res.Combo, res.Errors)
	}
	if res.Combo.Items[0].ID != 1 || res.Combo.Items[1].ID != 2 {
		t.Fatalf("重试后成分不对: %+v", res.Combo.Items)
	}
	if res.Attempts < 2 {
		t.Fatalf("应至少重试一次, attempts=%d", res.Attempts)
	}
}
