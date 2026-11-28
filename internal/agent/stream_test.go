package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"

	"factor-lab/internal/data"
)

func TestReplyExtractorJSON(t *testing.T) {
	ex := newReplyExtractor()
	chunks := []string{`{"re`, `ply": "低波`, `动\n因子", "expr": "x"}`}
	var got strings.Builder
	for _, c := range chunks {
		got.WriteString(ex.Feed(c))
	}
	if got.String() != "低波动\n因子" {
		t.Fatalf("got %q", got.String())
	}
}

func TestReplyExtractorPlain(t *testing.T) {
	ex := newReplyExtractor()
	if d := ex.Feed("这是"); d != "这是" {
		t.Fatalf("first %q", d)
	}
	if d := ex.Feed("解释"); d != "解释" {
		t.Fatalf("second %q", d)
	}
}

func TestReplyExtractorUnicodeEscape(t *testing.T) {
	ex := newReplyExtractor()
	got := ex.Feed(`{"reply": "A\u4e2dB"}`)
	if got != "A中B" {
		t.Fatalf("got %q", got)
	}
}

func TestRunAssistStreamDeltas(t *testing.T) {
	src := &data.SyntheticSource{Days: 80, Stocks: 20, Seed: 3}
	p, err := src.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	llm := &chunkLLM{
		chunks: []string{
			`{"reply": "`,
			`低波动因子。", "expr": "vol20 = ts_std_dev(pct_chg, 20)\nalpha = -1 * cs_rank(vol20)"}`,
		},
	}
	var events []AssistEvent
	res, err := RunAssistStream(context.Background(), llm, []AssistMsg{
		{Role: "user", Content: "写一个低波因子"},
	}, "", p, func(ev AssistEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid || res.Expr == "" {
		t.Fatalf("expected valid expr, got %+v", res)
	}
	var text strings.Builder
	var sawDone bool
	for _, ev := range events {
		switch ev.Type {
		case AssistEventDelta:
			text.WriteString(ev.Text)
		case AssistEventDone:
			sawDone = true
		}
	}
	if text.String() != "低波动因子。" {
		t.Fatalf("streamed reply %q", text.String())
	}
	if !sawDone {
		t.Fatal("missing done event")
	}
}

type chunkLLM struct{ chunks []string }

func (c *chunkLLM) Name() string { return "chunk" }

func (c *chunkLLM) Chat(ctx context.Context, system, user string) (string, error) {
	return strings.Join(c.chunks, ""), nil
}

func (c *chunkLLM) Stream(ctx context.Context, system, user string, emit func(string) error) (string, error) {
	var b strings.Builder
	for _, ch := range c.chunks {
		b.WriteString(ch)
		if err := emit(ch); err != nil {
			return b.String(), err
		}
	}
	return b.String(), nil
}

func TestRunAssistStreamChatModelAgent(t *testing.T) {
	src := &data.SyntheticSource{Days: 80, Stocks: 20, Seed: 3}
	p, err := src.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	llm := &scriptedLLM{replies: []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{{
			ID: "call_fields", Type: "function",
			Function: schema.FunctionCall{Name: toolListFields, Arguments: "{}"},
		}}),
		schema.AssistantMessage(`{"reply":"低波动因子。","expr":"vol20 = ts_std_dev(pct_chg, 20)\nalpha = -1 * cs_rank(vol20)"}`, nil),
	}}
	var text strings.Builder
	var sawDone bool
	res, err := RunAssistStream(context.Background(), llm, []AssistMsg{
		{Role: "user", Content: "写一个低波因子"},
	}, "", p, func(ev AssistEvent) error {
		switch ev.Type {
		case AssistEventDelta:
			text.WriteString(ev.Text)
		case AssistEventDone:
			sawDone = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid || res.Expr == "" {
		t.Fatalf("expected valid expr, got %+v", res)
	}
	if text.String() != "低波动因子。" {
		t.Fatalf("streamed reply %q", text.String())
	}
	if !sawDone {
		t.Fatal("missing done event")
	}
}
