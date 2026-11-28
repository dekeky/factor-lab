// Package agent 实现 AI 助手的模型接入与对话式因子编写。
//
// 组成：
//   - eino.go 的 LLM 抽象与 eino ChatModel（OpenAI 协议兼容端点）；
//   - settings.go 的模型配置（界面「设置」维护，保存即热生效）；
//   - chatmodel.go 的 ChatModelAgent（字段/算子/因子库作为工具，一次返回完整目录）；
//   - assist.go 的对话式辅助（产出候选表达式或组合，经引擎静态校验）。
//
// 纪律：提议/裁决分离 —— LLM 只产出思路与表达式草稿；校验与评价
// 由引擎硬编码执行，LLM 无法看到或修改评价口径；模型未配置时
// AI 功能明确报错，不做任何模板降级。
package agent

import (
	"context"
	"strings"
)

// LLM 是模型接入的抽象（eino 实现见 eino.go）。
type LLM interface {
	Name() string
	Chat(ctx context.Context, system, user string) (string, error)
}

// Streamer 是可选的流式补全。实现后 AI 助手可通过 WebSocket 边生成边推 token。
// 未实现时 RunAssistStream 退回一次 Chat，再把整段 reply 当作一条 delta。
type Streamer interface {
	Stream(ctx context.Context, system, user string, emit func(chunk string) error) (full string, err error)
}

// extractJSON 提取首个平衡的 {...} 块（容忍 markdown 代码围栏与前后说明文字）。
func extractJSON(raw string) string {
	start := strings.Index(raw, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(raw); i++ {
		c := raw[i]
		if inStr {
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return raw[start : i+1]
			}
		}
	}
	return ""
}
