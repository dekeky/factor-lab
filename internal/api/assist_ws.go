package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"factor-lab/internal/agent"
)

type assistWSReq struct {
	Type     string            `json:"type"` // chat | stop
	Mode     string            `json:"mode"` // factor | combo
	Messages []agent.AssistMsg `json:"messages"`
	Expr     string            `json:"expr"`
}

func (s *Server) assistUpgrader() websocket.Upgrader {
	allow := map[string]bool{}
	for _, o := range s.Opt.CORSOrigins {
		if o = strings.TrimSpace(o); o != "" {
			allow[o] = true
		}
	}
	return websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true
			}
			if allow[origin] {
				return true
			}
			u, err := url.Parse(origin)
			if err != nil {
				return false
			}
			return strings.EqualFold(u.Host, r.Host)
		},
	}
}

func (s *Server) handleAssistWS(w http.ResponseWriter, r *http.Request) {
	if s.currentLLM() == nil {
		writeErr(w, http.StatusServiceUnavailable, "模型未配置：请点左下角「设置」配置模型后再使用 AI 助手")
		return
	}
	up := s.assistUpgrader()
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("assist ws 升级失败", "err", err)
		return
	}
	defer conn.Close()

	conn.SetReadLimit(64 << 10)

	var (
		mu     sync.Mutex
		cancel context.CancelFunc
		busy   bool
	)
	writeEvent := func(ev agent.AssistEvent) error {
		mu.Lock()
		defer mu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
		return conn.WriteJSON(ev)
	}
	stop := func() {
		mu.Lock()
		c := cancel
		mu.Unlock()
		if c != nil {
			c()
		}
	}
	defer stop()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var req assistWSReq
		if err := json.Unmarshal(data, &req); err != nil {
			_ = writeEvent(agent.AssistEvent{Type: agent.AssistEventError, Message: "消息不是合法 JSON"})
			continue
		}
		switch strings.ToLower(strings.TrimSpace(req.Type)) {
		case "stop":
			stop()
		case "", "chat":
			if err := validateAssistMessages(req.Messages); err != nil {
				_ = writeEvent(agent.AssistEvent{Type: agent.AssistEventError, Message: err.Error()})
				continue
			}
			mu.Lock()
			if busy {
				mu.Unlock()
				_ = writeEvent(agent.AssistEvent{Type: agent.AssistEventError, Message: "上一轮还在生成，请先停止"})
				continue
			}
			busy = true
			ctx, c := context.WithTimeout(r.Context(), 90*time.Second)
			cancel = c
			llm := s.currentLLM()
			p := s.panel()
			mode := strings.ToLower(strings.TrimSpace(req.Mode))
			var catalog []agent.FactorRef
			if mode == agent.AssistModeCombo {
				catalog = s.assistFactorRefs()
			}
			mu.Unlock()
			if llm == nil {
				mu.Lock()
				busy = false
				cancel = nil
				mu.Unlock()
				_ = writeEvent(agent.AssistEvent{Type: agent.AssistEventError, Message: "模型未配置：请点左下角「设置」配置模型后再使用 AI 助手"})
				continue
			}
			go func(req assistWSReq, ctx context.Context, catalog []agent.FactorRef) {
				defer func() {
					c()
					mu.Lock()
					busy = false
					cancel = nil
					mu.Unlock()
				}()
				var err error
				if mode == agent.AssistModeCombo {
					_, err = agent.RunComboAssistStream(ctx, llm, req.Messages, req.Expr, catalog, writeEvent)
				} else {
					_, err = agent.RunAssistStream(ctx, llm, req.Messages, req.Expr, p, writeEvent)
				}
				if err != nil && ctx.Err() == nil {
					_ = writeEvent(agent.AssistEvent{Type: agent.AssistEventError, Message: "辅助失败: " + err.Error()})
				}
			}(req, ctx, catalog)
		default:
			_ = writeEvent(agent.AssistEvent{Type: agent.AssistEventError, Message: "未知消息类型"})
		}
	}
}

func validateAssistMessages(msgs []agent.AssistMsg) error {
	if len(msgs) == 0 || len(msgs) > 40 {
		return fmt.Errorf("messages 需要 1~40 条")
	}
	for i, m := range msgs {
		if m.Role != "user" && m.Role != "assistant" {
			return fmt.Errorf("第 %d 条消息角色非法（仅 user/assistant）", i+1)
		}
		if len(m.Content) > 4000 {
			return fmt.Errorf("单条消息过长（上限 4000 字符）")
		}
	}
	return nil
}
