package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	"factor-lab/internal/agent"
	"factor-lab/internal/data"
)

type wsLLM struct{}

func (w *wsLLM) Name() string { return "ws-fake" }

func (w *wsLLM) Chat(ctx context.Context, system, user string) (string, error) {
	return `{"reply":"你好","expr":""}`, nil
}

func (w *wsLLM) Stream(ctx context.Context, system, user string, emit func(string) error) (string, error) {
	parts := []string{`{"reply":"`, `你好"}`}
	var b strings.Builder
	for _, p := range parts {
		if err := ctx.Err(); err != nil {
			return b.String(), err
		}
		b.WriteString(p)
		if err := emit(p); err != nil {
			return b.String(), err
		}
	}
	return b.String(), nil
}

func TestAssistWebSocketStream(t *testing.T) {
	src := &data.SyntheticSource{Days: 40, Stocks: 10, Seed: 1}
	p, err := src.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(p, &wsLLM{}, "", "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	u := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/assist/ws"
	c, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if err := c.WriteJSON(map[string]any{
		"type":     "chat",
		"messages": []agent.AssistMsg{{Role: "user", Content: "你好"}},
		"expr":     "",
	}); err != nil {
		t.Fatal(err)
	}

	var text strings.Builder
	var done agent.AssistEvent
	for {
		var ev agent.AssistEvent
		if err := c.ReadJSON(&ev); err != nil {
			t.Fatal(err)
		}
		switch ev.Type {
		case agent.AssistEventDelta:
			text.WriteString(ev.Text)
		case agent.AssistEventDone:
			done = ev
			goto end
		case agent.AssistEventError:
			t.Fatalf("error event: %s", ev.Message)
		}
	}
end:
	if text.String() != "你好" {
		t.Fatalf("streamed %q", text.String())
	}
	if done.Reply != "你好" {
		t.Fatalf("done reply %q", done.Reply)
	}
}

type comboWSLLM struct{}

func (c *comboWSLLM) Name() string { return "combo-ws" }

func (c *comboWSLLM) Chat(ctx context.Context, system, user string) (string, error) {
	return `{"reply":"选了两个库内因子。","combo":{"name":"测试组合","method":"equal","items":[{"id":1,"weight":1,"direction":1},{"id":2,"weight":1,"direction":1}]}}`, nil
}

func TestAssistWebSocketComboMode(t *testing.T) {
	src := &data.SyntheticSource{Days: 40, Stocks: 10, Seed: 1}
	p, err := src.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(p, &comboWSLLM{}, "", "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	f1 := postFactor(t, ts, "rev5", `alpha = -1 * cs_rank(close / ts_delay(close, 5) - 1)`)
	f2 := postFactor(t, ts, "mom10", `alpha = cs_rank(close / ts_delay(close, 10) - 1)`)
	if f1.ID != 1 || f2.ID != 2 {
		t.Fatalf("期望因子 id 1/2，实际 %d/%d", f1.ID, f2.ID)
	}

	u := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/assist/ws"
	c, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if err := c.WriteJSON(map[string]any{
		"type":     "chat",
		"mode":     "combo",
		"messages": []agent.AssistMsg{{Role: "user", Content: "帮我组一个组合"}},
		"expr":     "",
	}); err != nil {
		t.Fatal(err)
	}

	var done agent.AssistEvent
	for {
		var ev agent.AssistEvent
		if err := c.ReadJSON(&ev); err != nil {
			t.Fatal(err)
		}
		switch ev.Type {
		case agent.AssistEventDone:
			done = ev
			goto end
		case agent.AssistEventError:
			t.Fatalf("error event: %s", ev.Message)
		}
	}
end:
	if !done.Valid || done.Combo == nil {
		t.Fatalf("组合模式应直接返回可应用的组合: %+v", done)
	}
	if done.Expr != "" {
		t.Fatalf("组合模式不应写新表达式: %q", done.Expr)
	}
	if done.Combo.Name != "测试组合" || len(done.Combo.Items) != 2 {
		t.Fatalf("组合内容不对: %+v", done.Combo)
	}
	if done.Combo.Items[0].Name != "rev5" || done.Combo.Items[1].Name != "mom10" {
		t.Fatalf("成分名应从因子库回填: %+v", done.Combo.Items)
	}
}
