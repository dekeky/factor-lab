package agent

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// EinoConfig 是 eino ChatModel 的连接参数（OpenAI 协议兼容端点：
// DeepSeek / Qwen / Kimi / GPT / 本地 Ollama 等都能接）。
type EinoConfig struct {
	BaseURL     string
	APIKey      string
	Model       string
	Temperature float64 // < 0 表示不下发，用服务端默认
	// MaxTokens 刻意不设：不限制输出长度，交给模型自身支持的上限
	Timeout time.Duration
}

// EinoLLM 基于 eino + OpenAI 协议兼容端点。
type EinoLLM struct {
	name  string
	model *openai.ChatModel
}

// NewEinoLLM 创建 eino ChatModel。注意：只构造客户端，不发起网络请求，
// 真正连通性由 Settings.Test 验证。
func NewEinoLLM(ctx context.Context, cfg EinoConfig) (*EinoLLM, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("API Key 未配置")
	}
	base := cfg.BaseURL
	if base == "" {
		base = "https://api.deepseek.com/v1"
	}
	model := cfg.Model
	if model == "" {
		model = "deepseek-chat"
	}
	tmo := cfg.Timeout
	if tmo <= 0 {
		tmo = 90 * time.Second
	}
	c := &openai.ChatModelConfig{
		APIKey:  cfg.APIKey,
		BaseURL: base,
		Model:   model,
		Timeout: tmo,
	}
	if cfg.Temperature >= 0 {
		t := float32(cfg.Temperature)
		c.Temperature = &t
	}
	cm, err := openai.NewChatModel(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("创建 eino ChatModel 失败: %w", err)
	}
	return &EinoLLM{name: model, model: cm}, nil
}

func (e *EinoLLM) Name() string { return e.name }

func (e *EinoLLM) ChatModel() model.BaseChatModel { return e.model }

func (e *EinoLLM) Chat(ctx context.Context, system, user string) (string, error) {
	msgs := []*schema.Message{
		schema.SystemMessage(system),
		schema.UserMessage(user),
	}
	out, err := e.model.Generate(ctx, msgs)
	if err != nil {
		return "", err
	}
	return out.Content, nil
}

func (e *EinoLLM) Stream(ctx context.Context, system, user string, emit func(chunk string) error) (string, error) {
	msgs := []*schema.Message{
		schema.SystemMessage(system),
		schema.UserMessage(user),
	}
	sr, err := e.model.Stream(ctx, msgs)
	if err != nil {
		return "", err
	}
	defer sr.Close()
	var b strings.Builder
	for {
		msg, err := sr.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return b.String(), err
		}
		if msg == nil || msg.Content == "" {
			continue
		}
		b.WriteString(msg.Content)
		if emit != nil {
			if err := emit(msg.Content); err != nil {
				return b.String(), err
			}
		}
	}
	return b.String(), nil
}
