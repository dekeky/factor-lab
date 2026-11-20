package api

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"factor-lab/internal/agent"
)

type modelView struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	BaseURL      string `json:"base_url"`
	Model        string `json:"model"`
	APIKeyMasked string `json:"api_key_masked"`
	HasKey       bool   `json:"has_key"`
}

// settingsResp 是 GET/PUT /api/settings 的响应。
// 密钥永远只回掩码：避免接口被随手截图/复制时泄露 key。
type settingsResp struct {
	BaseURL            string      `json:"base_url"`
	Model              string      `json:"model"`
	Temperature        float64     `json:"temperature"`
	TimeoutSec         int         `json:"timeout_sec"`
	APIKeyMasked       string      `json:"api_key_masked"`
	HasKey             bool        `json:"has_key"`
	TushareTokenMasked string      `json:"tushare_token_masked"`
	HasTushareToken    bool        `json:"has_tushare_token"`
	Engine             string      `json:"engine"`
	Online             bool        `json:"online"`    // 是否已接入可用模型（false = 未配置，AI 功能不可用）
	Persisted          bool        `json:"persisted"` // 是否已写入 settings.json
	ReloadError        string      `json:"reload_error,omitempty"`
	ActiveID           string      `json:"active_id"`
	Models             []modelView `json:"models"`
}

type modelReq struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	BaseURL  string `json:"base_url"`
	Model    string `json:"model"`
	APIKey   string `json:"api_key"`
	ClearKey bool   `json:"clear_key"`
}

// settingsReq 是保存/测试请求体。api_key 留空表示「沿用已保存的密钥」，
// clear_key 为 true 才显式清空。Models 为空时走旧版单模型字段。
type settingsReq struct {
	BaseURL           string     `json:"base_url"`
	Model             string     `json:"model"`
	Temperature       float64    `json:"temperature"`
	TimeoutSec        int        `json:"timeout_sec"`
	APIKey            string     `json:"api_key"`
	ClearKey          bool       `json:"clear_key"`
	TushareToken      string     `json:"tushare_token"`
	ClearTushareToken bool       `json:"clear_tushare_token"`
	ActiveID          string     `json:"active_id"`
	Models            []modelReq `json:"models"`
}

func (r settingsReq) merge(cur agent.Settings) agent.Settings {
	next := cur
	if r.TimeoutSec > 0 {
		next.TimeoutSec = r.TimeoutSec
	}
	if r.Temperature != 0 || next.Temperature == 0 {
		next.Temperature = r.Temperature
	}
	if k := strings.TrimSpace(r.TushareToken); k != "" {
		next.TushareToken = k
	}
	if r.ClearTushareToken {
		next.TushareToken = ""
	}

	if len(r.Models) > 0 {
		oldByID := map[string]agent.ModelEntry{}
		for _, m := range cur.Models {
			oldByID[m.ID] = m
		}
		models := make([]agent.ModelEntry, 0, len(r.Models))
		for _, in := range r.Models {
			e := agent.ModelEntry{
				ID:      strings.TrimSpace(in.ID),
				Name:    strings.TrimSpace(in.Name),
				BaseURL: strings.TrimSpace(in.BaseURL),
				Model:   strings.TrimSpace(in.Model),
			}
			old, ok := oldByID[e.ID]
			if k := strings.TrimSpace(in.APIKey); k != "" {
				e.APIKey = k
			} else if in.ClearKey {
				e.APIKey = ""
			} else if ok {
				e.APIKey = old.APIKey
			}
			models = append(models, e)
		}
		next.Models = models
		next.ActiveID = strings.TrimSpace(r.ActiveID)
		return next.Normalize()
	}

	next.BaseURL = r.BaseURL
	next.Model = r.Model
	if k := strings.TrimSpace(r.APIKey); k != "" {
		next.APIKey = k
	}
	if r.ClearKey {
		next.APIKey = ""
	}
	return next.ApplyFlat().Normalize()
}

func (s *Server) settingsView() settingsResp {
	s.llmMu.RLock()
	cfg, llm := s.llmCfg, s.llm
	s.llmMu.RUnlock()
	engine := "unconfigured"
	online := false
	if llm != nil {
		engine = llm.Name()
		online = true
	}
	models := make([]modelView, 0, len(cfg.Models))
	for _, m := range cfg.Models {
		models = append(models, modelView{
			ID:           m.ID,
			Name:         m.Name,
			BaseURL:      m.BaseURL,
			Model:        m.Model,
			APIKeyMasked: agent.MaskKey(m.APIKey),
			HasKey:       m.APIKey != "",
		})
	}
	return settingsResp{
		BaseURL:            cfg.BaseURL,
		Model:              cfg.Model,
		Temperature:        cfg.Temperature,
		TimeoutSec:         cfg.TimeoutSec,
		APIKeyMasked:       agent.MaskKey(cfg.APIKey),
		HasKey:             cfg.APIKey != "",
		TushareTokenMasked: agent.MaskKey(cfg.TushareToken),
		HasTushareToken:    cfg.TushareToken != "",
		Engine:             engine,
		Online:             online,
		Persisted:          fileExists(s.llmPath),
		ActiveID:           cfg.ActiveID,
		Models:             models,
	}
}

func fileExists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.settingsView())
}

func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	var req settingsReq
	if !readJSON(w, r, &req) {
		return
	}
	s.llmMu.RLock()
	cur := s.llmCfg
	s.llmMu.RUnlock()

	next := req.merge(cur)
	if err := next.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// 先构造模型：配置不可用就不落盘、不替换，避免把能用的旧配置覆盖成坏配置。
	// 例外：没有密钥且不是本机端点 = 用户主动清空，等价于「回到未配置」。
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var llm agent.LLM
	if len(next.Models) == 0 || (next.APIKey == "" && !agent.IsLocalEndpoint(next.BaseURL)) {
		llm = nil // 无密钥 = 显式回到「未配置」，AI 功能明确报错，不做降级
	} else {
		l, err := next.NewLLM(ctx)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		llm = l
	}

	if s.llmPath != "" {
		if err := agent.SaveSettings(s.llmPath, next); err != nil {
			writeErr(w, http.StatusInternalServerError, "保存配置失败："+err.Error())
			return
		}
	}

	s.llmMu.Lock()
	s.llmCfg = next
	s.llm = llm
	s.llmMu.Unlock()

	if s.Opt.ApplyTushareToken != nil {
		s.Opt.ApplyTushareToken(next.TushareToken)
	}

	view := s.settingsView()
	// 面板还是空的且刚写入了 token：立刻拉数，避免用户还要再调热加载。
	if next.TushareToken != "" && next.TushareToken != cur.TushareToken &&
		s.Opt.Reload != nil && s.panel() != nil && s.panel().T() == 0 {
		rctx, rcancel := context.WithTimeout(r.Context(), 10*time.Minute)
		defer rcancel()
		np, err := s.Opt.Reload(rctx)
		if err != nil {
			view.ReloadError = err.Error()
		} else {
			s.p.Store(np)
			s.clearUniverseCache()
		}
	}

	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleTestSettings(w http.ResponseWriter, r *http.Request) {
	var req settingsReq
	if !readJSON(w, r, &req) {
		return
	}
	s.llmMu.RLock()
	cur := s.llmCfg
	s.llmMu.RUnlock()

	next := req.merge(cur)
	if err := next.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 50*time.Second)
	defer cancel()
	res, err := next.Test(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"engine":     res.Engine,
		"latency_ms": res.LatencyMS,
		"reply":      res.Reply,
	})
}
