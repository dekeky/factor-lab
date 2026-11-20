package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ModelEntry 是一条可切换的模型配置。Name 仅用于界面展示。
type ModelEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key,omitempty"`
	Model   string `json:"model"`
}

// Settings 是运行期配置，由界面「设置」维护并落盘 settings.json。
// BaseURL / APIKey / Model 始终等于当前选中的模型，方便旧代码与环境变量路径读取。
type Settings struct {
	BaseURL     string  `json:"base_url"`
	APIKey      string  `json:"api_key,omitempty"`
	Model       string  `json:"model"`
	Temperature float64 `json:"temperature"`
	// MaxTokens 刻意不设：由模型自身支持的上限决定，agent 层不做截断
	TimeoutSec int `json:"timeout_sec"`
	// TushareToken 是 Tushare Pro 取数凭证；与 APIKey 一样只落盘、接口只回掩码。
	TushareToken string       `json:"tushare_token,omitempty"`
	Models       []ModelEntry `json:"models"`
	ActiveID     string       `json:"active_id,omitempty"`
}

func defaultModel() ModelEntry {
	return ModelEntry{
		ID:      "m1",
		Name:    "DeepSeek",
		BaseURL: "https://api.deepseek.com/v1",
		Model:   "deepseek-chat",
	}
}

func cloneModels(in []ModelEntry) []ModelEntry {
	if in == nil {
		return nil
	}
	out := make([]ModelEntry, len(in))
	copy(out, in)
	return out
}

// DefaultSettings 返回一份可直接用的默认配置（DeepSeek 协议端点）。
func DefaultSettings() Settings {
	m := defaultModel()
	return Settings{
		BaseURL:     m.BaseURL,
		Model:       m.Model,
		Temperature: 0.3,
		TimeoutSec:  90,
		Models:      []ModelEntry{m},
		ActiveID:    m.ID,
	}
}

// ApplyFlat 把顶层 BaseURL / APIKey / Model 写进当前选中的模型（没有则新建一条）。
// 给环境变量覆盖、旧版单模型请求用。
func (s Settings) ApplyFlat() Settings {
	out := s
	out.Models = cloneModels(s.Models)
	if len(out.Models) == 0 {
		out.Models = []ModelEntry{{
			ID:      "m1",
			BaseURL: out.BaseURL,
			APIKey:  out.APIKey,
			Model:   out.Model,
		}}
		out.ActiveID = "m1"
		return out
	}
	idx := 0
	for i, m := range out.Models {
		if m.ID == out.ActiveID {
			idx = i
			break
		}
	}
	out.Models[idx].BaseURL = out.BaseURL
	out.Models[idx].APIKey = out.APIKey
	out.Models[idx].Model = out.Model
	out.ActiveID = out.Models[idx].ID
	return out
}

func (s Settings) activeIndex() int {
	for i, m := range s.Models {
		if m.ID == s.ActiveID {
			return i
		}
	}
	if len(s.Models) > 0 {
		return 0
	}
	return -1
}

// Active 返回当前选中的模型。
func (s Settings) Active() (ModelEntry, bool) {
	i := s.activeIndex()
	if i < 0 {
		return ModelEntry{}, false
	}
	return s.Models[i], true
}

func trimEntry(m ModelEntry) ModelEntry {
	m.ID = strings.TrimSpace(m.ID)
	m.Name = strings.TrimSpace(m.Name)
	m.BaseURL = strings.TrimRight(strings.TrimSpace(m.BaseURL), "/")
	m.APIKey = strings.TrimSpace(m.APIKey)
	m.Model = strings.TrimSpace(m.Model)
	return m
}

func entryLabel(m ModelEntry) string {
	if m.Name != "" {
		return m.Name
	}
	if m.Model != "" {
		return m.Model
	}
	return m.ID
}

// Normalize 去掉首尾空白并补齐越界字段，返回清洗后的副本。
// Models 是源；顶层 BaseURL / APIKey / Model 从选中项同步出去。
func (s Settings) Normalize() Settings {
	out := s
	out.Models = cloneModels(s.Models)
	out.TushareToken = strings.TrimSpace(out.TushareToken)
	if out.Temperature < 0 {
		out.Temperature = 0
	}
	if out.Temperature > 2 {
		out.Temperature = 2
	}
	if out.TimeoutSec <= 0 {
		out.TimeoutSec = DefaultSettings().TimeoutSec
	}
	if out.TimeoutSec > 600 {
		out.TimeoutSec = 600
	}

	if len(out.Models) == 0 {
		base := strings.TrimRight(strings.TrimSpace(out.BaseURL), "/")
		model := strings.TrimSpace(out.Model)
		key := strings.TrimSpace(out.APIKey)
		if base != "" || model != "" || key != "" {
			if base == "" {
				base = defaultModel().BaseURL
			}
			if model == "" {
				model = defaultModel().Model
			}
			out.Models = []ModelEntry{{ID: "m1", BaseURL: base, APIKey: key, Model: model}}
			out.ActiveID = "m1"
		}
	}

	seen := map[string]int{}
	for i := range out.Models {
		m := trimEntry(out.Models[i])
		if m.ID == "" {
			m.ID = fmt.Sprintf("m%d", i+1)
		}
		if n, ok := seen[m.ID]; ok {
			m.ID = fmt.Sprintf("%s-%d", m.ID, n+1)
		}
		seen[m.ID]++
		out.Models[i] = m
	}

	if len(out.Models) == 0 {
		out.ActiveID = ""
		out.BaseURL = ""
		out.APIKey = ""
		out.Model = ""
		return out
	}
	idx := out.activeIndex()
	if idx < 0 {
		idx = 0
	}
	out.ActiveID = out.Models[idx].ID
	out.BaseURL = out.Models[idx].BaseURL
	out.APIKey = out.Models[idx].APIKey
	out.Model = out.Models[idx].Model
	return out
}

// Validate 校验配置是否可用（不做网络请求）。无模型视为未配置，允许保存。
func (s Settings) Validate() error {
	if s.Temperature < 0 || s.Temperature > 2 {
		return fmt.Errorf("温度需要在 0 ~ 2 之间")
	}
	if len(s.Models) == 0 {
		return nil
	}
	for _, m := range s.Models {
		if !strings.HasPrefix(m.BaseURL, "http://") && !strings.HasPrefix(m.BaseURL, "https://") {
			return fmt.Errorf("%s：地址需要以 http:// 或 https:// 开头", entryLabel(m))
		}
		if m.Model == "" {
			return fmt.Errorf("%s：模型名不能为空", entryLabel(m))
		}
	}
	return nil
}

// NewLLM 按当前配置构造 eino ChatModel。
// 本机端点（Ollama / vLLM 等）通常不校验密钥，允许 key 留空。
func (s Settings) NewLLM(ctx context.Context) (LLM, error) {
	s = s.Normalize()
	if len(s.Models) == 0 {
		return nil, fmt.Errorf("未配置模型")
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	key := s.APIKey
	if key == "" {
		if !isLocalEndpoint(s.BaseURL) {
			return nil, fmt.Errorf("API Key 未配置")
		}
		key = "local"
	}
	return NewEinoLLM(ctx, EinoConfig{
		BaseURL:     s.BaseURL,
		APIKey:      key,
		Model:       s.Model,
		Temperature: s.Temperature,
		Timeout:     time.Duration(s.TimeoutSec) * time.Second,
	})
}

func isLocalEndpoint(u string) bool {
	u = strings.ToLower(u)
	return strings.Contains(u, "127.0.0.1") ||
		strings.Contains(u, "localhost") ||
		strings.Contains(u, "0.0.0.0") ||
		strings.HasPrefix(u, "http://[::1]")
}

// IsLocalEndpoint 判断端点是否指向本机（本机端点允许不填密钥）。
func IsLocalEndpoint(u string) bool { return isLocalEndpoint(u) }

// TestResult 是一次连通性测试的结果。
type TestResult struct {
	Engine    string `json:"engine"`
	LatencyMS int64  `json:"latency_ms"`
	Reply     string `json:"reply"`
}

// Test 发一条最短对话验证端点、密钥与模型名是否可用。
func (s Settings) Test(ctx context.Context) (*TestResult, error) {
	s = s.Normalize()
	llm, err := s.NewLLM(ctx)
	if err != nil {
		return nil, err
	}
	// 测试用的超时比正式调用更紧，避免用户在弹窗里干等
	tmo := time.Duration(s.TimeoutSec) * time.Second
	if tmo <= 0 || tmo > 45*time.Second {
		tmo = 45 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, tmo)
	defer cancel()
	t0 := time.Now()
	reply, err := llm.Chat(cctx, "你是连通性测试助手。", "只回复两个字：可用")
	if err != nil {
		return nil, fmt.Errorf("调用失败: %w", err)
	}
	return &TestResult{
		Engine:    llm.Name(),
		LatencyMS: time.Since(t0).Milliseconds(),
		Reply:     truncateRunes(strings.TrimSpace(reply), 120),
	}, nil
}

// MaskKey 返回可安全展示的密钥（前 4 位 + 后 4 位）。
func MaskKey(k string) string {
	k = strings.TrimSpace(k)
	if k == "" {
		return ""
	}
	if len(k) <= 8 {
		return "••••••"
	}
	return k[:4] + "••••••" + k[len(k)-4:]
}

// LoadSettings 读取配置文件。第二个返回值表示文件是否存在。
func LoadSettings(path string) (Settings, bool, error) {
	if path == "" {
		return DefaultSettings(), false, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultSettings(), false, nil
		}
		return DefaultSettings(), false, err
	}
	var s Settings
	if err := json.Unmarshal(b, &s); err != nil {
		return DefaultSettings(), true, fmt.Errorf("模型配置文件损坏（%s）：%w", path, err)
	}
	if s.Models == nil {
		s.Models = []ModelEntry{{
			ID:      "m1",
			BaseURL: s.BaseURL,
			APIKey:  s.APIKey,
			Model:   s.Model,
		}}
		s.ActiveID = "m1"
	}
	return s.Normalize(), true, nil
}

// SaveSettings 原子写入配置文件（含密钥，权限 0600）。
func SaveSettings(path string, s Settings) error {
	if path == "" {
		return nil
	}
	b, err := json.MarshalIndent(s.Normalize(), "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".settings-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	tmp.Close()
	_ = os.Chmod(name, 0o600)
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
