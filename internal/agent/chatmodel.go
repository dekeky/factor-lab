package agent

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"factor-lab/internal/panel"
)

const chatModelMaxIterations = 8

// ChatModeler 能提供 eino ChatModel，走 ChatModelAgent（工具调用）。
// 测试用的假模型只实现 Chat，仍走原来的单轮 JSON 补全。
type ChatModeler interface {
	ChatModel() model.BaseChatModel
}

func runChatModelAssist(ctx context.Context, cm model.BaseChatModel, sys string, msgs []AssistMsg, current, feedback string, stream bool, onChunk func(string) error, mode string, p *panel.Panel, factors []FactorRef) (string, error) {
	tools, err := assistTools(mode, p, factors)
	if err != nil {
		return "", err
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        assistAgentName(mode),
		Description: assistAgentDesc(mode),
		Instruction: sys,
		Model:       cm,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools:               tools,
				UnknownToolsHandler: unknownToolMessage,
			},
		},
		MaxIterations: chatModelMaxIterations,
		GenModelInput: func(_ context.Context, instruction string, input *adk.AgentInput) ([]adk.Message, error) {
			out := make([]*schema.Message, 0, len(input.Messages)+1)
			if instruction != "" {
				out = append(out, schema.SystemMessage(instruction))
			}
			out = append(out, input.Messages...)
			return out, nil
		},
	})
	if err != nil {
		return "", fmt.Errorf("创建 ChatModelAgent 失败: %w", err)
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		EnableStreaming: stream,
	})
	iter := runner.Run(ctx, assistSchemaMessages(msgs, current, feedback))
	return consumeAgentEvents(iter, onChunk)
}

func assistTools(mode string, p *panel.Panel, factors []FactorRef) ([]tool.BaseTool, error) {
	if mode == AssistModeCombo {
		return comboTools(factors)
	}
	return factorTools(p)
}

func assistAgentName(mode string) string {
	if mode == AssistModeCombo {
		return "factor_combo_assistant"
	}
	return "factor_factory_assistant"
}

func assistAgentDesc(mode string) string {
	if mode == AssistModeCombo {
		return "从现有因子库挑选因子做线性组合"
	}
	return "对话式编写因子 DSL 表达式"
}

func assistSchemaMessages(msgs []AssistMsg, current, feedback string) []*schema.Message {
	out := make([]*schema.Message, 0, len(msgs)+2)
	for _, m := range msgs {
		switch m.Role {
		case "assistant":
			out = append(out, schema.AssistantMessage(m.Content, nil))
		default:
			out = append(out, schema.UserMessage(m.Content))
		}
	}
	if extra := stringsJoinContext(current, feedback); extra != "" {
		out = append(out, schema.UserMessage(extra))
	}
	return out
}

func stringsJoinContext(current, feedback string) string {
	var b strings.Builder
	if strings.TrimSpace(current) != "" {
		b.WriteString("【当前上下文】\n")
		b.WriteString(current)
		b.WriteString("\n（修改类请求请基于它修改）")
	}
	if feedback != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("【上次尝试的反馈】\n")
		b.WriteString(feedback)
	}
	if b.Len() == 0 {
		return "请按系统约定的 JSON 格式回复。写表达式或组合前先调用对应的 list_* 工具查看完整目录。"
	}
	b.WriteString("\n\n请按系统约定的 JSON 格式回复。")
	return b.String()
}

func consumeAgentEvents(iter *adk.AsyncIterator[*adk.AgentEvent], onChunk func(string) error) (string, error) {
	var last string
	for {
		ev, ok := iter.Next()
		if !ok {
			break
		}
		if ev == nil {
			continue
		}
		if ev.Err != nil {
			if last != "" {
				return last, ev.Err
			}
			return "", ev.Err
		}
		if ev.Output == nil || ev.Output.MessageOutput == nil {
			continue
		}
		mv := ev.Output.MessageOutput
		if mv.Role == schema.Tool {
			if mv.IsStreaming && mv.MessageStream != nil {
				mv.MessageStream.SetAutomaticClose()
			}
			continue
		}
		msg, err := readAssistantMessage(mv, onChunk)
		if err != nil {
			return last, err
		}
		if msg == nil || len(msg.ToolCalls) > 0 {
			continue
		}
		if msg.Content != "" {
			last = msg.Content
		}
	}
	return last, nil
}

func readAssistantMessage(mv *adk.MessageVariant, onChunk func(string) error) (*schema.Message, error) {
	if mv == nil {
		return nil, nil
	}
	if !mv.IsStreaming {
		if mv.Message != nil && len(mv.Message.ToolCalls) == 0 && mv.Message.Content != "" && onChunk != nil {
			if err := onChunk(mv.Message.Content); err != nil {
				return mv.Message, err
			}
		}
		return mv.Message, nil
	}
	if mv.MessageStream == nil {
		return nil, nil
	}
	defer mv.MessageStream.Close()
	var chunks []*schema.Message
	sawToolCall := false
	for {
		c, err := mv.MessageStream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if c == nil {
			continue
		}
		chunks = append(chunks, c)
		if len(c.ToolCalls) > 0 {
			sawToolCall = true
			continue
		}
		if onChunk != nil && c.Content != "" && !sawToolCall {
			if err := onChunk(c.Content); err != nil {
				return nil, err
			}
		}
	}
	if len(chunks) == 0 {
		return nil, nil
	}
	if len(chunks) == 1 {
		return chunks[0], nil
	}
	return schema.ConcatMessages(chunks)
}
