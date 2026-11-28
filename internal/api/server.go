// Package api 提供 factor-lab 的 HTTP 接口与静态前端托管。
package api

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"factor-lab/internal/agent"
	"factor-lab/internal/compose"
	"factor-lab/internal/data"
	"factor-lab/internal/engine"
	"factor-lab/internal/eval"
	"factor-lab/internal/factors"
	"factor-lab/internal/ops"
	"factor-lab/internal/panel"
	"factor-lab/internal/paper/jobs"
	pstore "factor-lab/internal/paper/store"
)

// Summary 是因子指标的落盘摘要（列表与榜单够用，且避免把整条时序塞进 JSON）。
type Summary struct {
	Horizon       int       `json:"horizon"`
	NDates        int       `json:"n_dates"`
	ICMean        float64   `json:"ic_mean"`
	RankICMean    float64   `json:"rank_ic_mean"`
	RankICIR      float64   `json:"rank_ic_ir"`
	RankICT       float64   `json:"rank_ic_t"`
	PIC002        float64   `json:"pic_002"`
	Coverage      float64   `json:"coverage"`
	TopExcessCum  float64   `json:"top_excess_cum"`
	LongShortCum  float64   `json:"long_short_cum"`
	TurnoverDaily float64   `json:"turnover_daily"`
	CostAnnual    float64   `json:"cost_annual"`
	LayerMean     []float64 `json:"layer_mean"`
	LayerCum      []float64 `json:"layer_cum"`
	EvaluatedAt   time.Time `json:"evaluated_at"`
	From          string    `json:"from"`
	To            string    `json:"to"`
}

// Summarize 把完整评价结果压成落盘摘要。
func Summarize(m *eval.Metrics, from, to string, at time.Time) *Summary {
	return &Summary{
		Horizon: m.Config.Horizon, NDates: m.NDates,
		ICMean: m.ICMean, RankICMean: m.RankICMean, RankICIR: m.RankICIR, RankICT: m.RankICT,
		PIC002: m.PIC002, Coverage: m.Coverage,
		TopExcessCum: m.TopExcessCum, LongShortCum: m.LongShortCum,
		TurnoverDaily: m.TurnoverDaily, CostAnnual: m.CostAnnual,
		LayerMean: m.LayerMean, LayerCum: m.LayerCum,
		EvaluatedAt: at, From: from, To: to,
	}
}

// Factor 是因子实体（全部落盘，含内置因子——内置只是首次启动的种子数据）。
type Factor struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Category  string    `json:"category"`
	Expr      string    `json:"expr"`
	Source    string    `json:"source"` // classic | user | llm | composed
	Desc      string    `json:"desc,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Summary   *Summary  `json:"summary,omitempty"`

	// 运行期字段：当前面板是否支持该表达式（换数据源后可能缺字段/算子）
	Available   bool   `json:"available"`
	Unavailable string `json:"unavailable_reason,omitempty"`
}

// markAvailability 用当前面板校验每个因子能否计算（数据源切换后字段可能缺失）。
func (s *Server) markAvailability(fs []Factor) {
	for i := range fs {
		if errs := engine.Check(fs[i].Expr, s.panel()); len(errs) > 0 {
			fs[i].Available = false
			n := len(errs)
			if n > 2 {
				n = 2
			}
			parts := make([]string, n)
			for k := 0; k < n; k++ {
				parts[k] = errs[k]
			}
			fs[i].Unavailable = strings.Join(parts, "; ")
		} else {
			fs[i].Available = true
			fs[i].Unavailable = ""
		}
	}
}

// ComboItem 是组合里的一个因子引用（表达式在评价时从因子库实时解析，避免漂移）。
type ComboItem struct {
	ID        int     `json:"id"`
	Name      string  `json:"name"`
	Weight    float64 `json:"weight"`
	Direction int     `json:"direction"` // 1 = 正向使用，-1 = 反向使用

	// 运行期字段：引用的因子是否仍存在且当前面板可用
	Available bool   `json:"available"`
	Missing   bool   `json:"missing"` // 引用的因子已被删除
	Reason    string `json:"reason,omitempty"`
}

// Combo 是因子组合（卡片式管理：保存后可反复评价，指标摘要随最近一次评价刷新）。
type Combo struct {
	ID        int         `json:"id"`
	Name      string      `json:"name"`
	Method    string      `json:"method"` // equal | manual
	Items     []ComboItem `json:"items"`
	Desc      string      `json:"desc,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
	Summary   *Summary    `json:"summary,omitempty"`
}

// Options 是服务的运行期配置。
type Options struct {
	DataSource  string           // 数据源标识（如 tushare:000300.SH）
	StockPool   string           // 股票池标识（ALL = 全市场，或 000300.SH 等指数）
	Report      *data.LoadReport // 数据质量摘要（可选）
	APIToken    string           // 访问令牌；空 = 不鉴权
	CORSOrigins []string         // 允许跨域的来源（空 = 同源专用，不下发 CORS 头）
	// LLMSettings / SettingsPath：模型配置的初值与其落盘路径（界面「设置」可热改）
	LLMSettings  agent.Settings
	SettingsPath string
	// Reload 用于热加载数据面板（每日行情更新后无需重启进程）
	Reload func(ctx context.Context) (*panel.Panel, error)
	// ApplyTushareToken 把界面保存的 token 写回数据源（下次 Load / 热加载生效）。
	ApplyTushareToken func(token string)
	// LoadUniverse 按指数代码加载另一份成分面板（评价时切换股票池）。空 = 不支持切换。
	LoadUniverse func(ctx context.Context, index string) (*panel.Panel, error)
	// PaperDB 模拟盘 SQLite（与 River 共用）。空且因子库有目录时用同目录 paper.db。
	PaperDB string
	// PaperWorkers 为 true 时启动 River 消费 paper.generate / paper.execute。
	PaperWorkers bool
	// OpenDates 已公布的开市日（YYYY-MM-DD）。空则模拟盘退回面板日期轴。
	OpenDates func() []string
}

// Server 持有全部运行时依赖。
type Server struct {
	p   atomic.Pointer[panel.Panel] // 原子替换，支持热加载
	Web string                      // web/dist 目录（可选）
	Opt Options

	llmMu   sync.RWMutex // 保护 llm / llmCfg（界面改配置后热替换）
	llm     agent.LLM
	llmCfg  agent.Settings
	llmPath string // settings.json 路径；空 = 不持久化

	mu      sync.Mutex
	factors []Factor
	store   string // factors.json 路径；空 = 不持久化
	nextID  int

	combos      []Combo
	comboStore  string // combos.json 路径（与 factors.json 同目录）
	nextComboID int

	paperJobs *jobs.Deps

	uniMu sync.Mutex
	uni   map[string]*uniPanel // 按指数缓存的评价股票池
}

type uniPanel struct {
	p     *panel.Panel
	err   error
	ready chan struct{}
}

// panel 返回当前数据面板（原子读，热加载期间其他请求仍读旧面板，不会读到半成品）。
func (s *Server) panel() *panel.Panel { return s.p.Load() }

// currentLLM 返回当前生效的模型。界面「设置」保存后可热替换，读路径必须走它。
// 返回 nil 表示模型未配置（不做降级，AI 功能由调用方明确报错）。
func (s *Server) currentLLM() agent.LLM {
	s.llmMu.RLock()
	defer s.llmMu.RUnlock()
	return s.llm
}

// llmName 返回模型名（未配置时为 unconfigured，供界面展示状态）。
func (s *Server) llmName() string {
	l := s.currentLLM()
	if l == nil {
		return "unconfigured"
	}
	return l.Name()
}

// New 组装服务。因子库持久化到 store；内置经典因子仅作为首次播种数据，
// 之后与用户因子同等对待（可编辑、可删除、可导出），播种记录写在 <store>.seeded。
func New(p *panel.Panel, llm agent.LLM, store, web string, opt Options) (*Server, error) {
	s := &Server{
		llm: llm, llmCfg: opt.LLMSettings.Normalize(), llmPath: opt.SettingsPath,
		Web: web, store: store, nextID: 1, nextComboID: 1, Opt: opt,
		uni: make(map[string]*uniPanel),
	}
	s.p.Store(p)

	if store != "" {
		if b, err := os.ReadFile(store); err == nil {
			var fs []Factor
			if err := json.Unmarshal(b, &fs); err != nil {
				return nil, fmt.Errorf("因子库文件损坏（%s）：%w", store, err)
			}
			s.factors = fs
			for _, f := range fs {
				if f.ID >= s.nextID {
					s.nextID = f.ID + 1
				}
			}
		} else if !os.IsNotExist(err) {
			return nil, err
		}

		// 因子组合单独落盘（combos.json），与因子库互不干扰
		s.comboStore = filepath.Join(filepath.Dir(store), "combos.json")
		if b, err := os.ReadFile(s.comboStore); err == nil {
			var cs []Combo
			if err := json.Unmarshal(b, &cs); err != nil {
				return nil, fmt.Errorf("组合文件损坏（%s）：%w", s.comboStore, err)
			}
			s.combos = cs
			for _, c := range cs {
				if c.ID >= s.nextComboID {
					s.nextComboID = c.ID + 1
				}
			}
		} else if !os.IsNotExist(err) {
			return nil, err
		}

		// 只播种一次：避免用户删除内置因子后重启又复活
		flag := store + ".seeded"
		if _, err := os.Stat(flag); os.IsNotExist(err) {
			have := make(map[string]bool, len(s.factors))
			for _, f := range s.factors {
				have[f.Name] = true
			}
			now := time.Now()
			for _, d := range factors.Classics {
				if have[d.Name] {
					continue
				}
				s.factors = append(s.factors, Factor{
					ID: s.nextID, Name: d.Name, Category: d.Category, Expr: d.Expr,
					Source: "classic", Desc: d.Desc, CreatedAt: now, UpdatedAt: now,
				})
				s.nextID++
			}
			s.persist()
			if err := os.MkdirAll(filepath.Dir(flag), 0o755); err == nil {
				_ = os.WriteFile(flag, []byte(now.Format(time.RFC3339)), 0o644)
			}
		}
	}
	if err := s.initPaper(opt); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Server) initPaper(opt Options) error {
	path := strings.TrimSpace(opt.PaperDB)
	if path == "" && s.store != "" {
		path = filepath.Join(filepath.Dir(s.store), "paper.db")
	}
	if path == "" {
		return nil
	}
	st, err := pstore.Open(path)
	if err != nil {
		return fmt.Errorf("打开模拟盘数据库：%w", err)
	}
	if err := jobs.MigrateRiver(context.Background(), st.DB()); err != nil {
		st.Close()
		return fmt.Errorf("模拟盘队列迁移：%w", err)
	}
	reload := opt.Reload
	if strings.Contains(strings.ToLower(opt.DataSource), "csv") {
		reload = nil
	}
	deps := &jobs.Deps{Store: st, Panel: s.panel, Reload: reload, OpenDates: opt.OpenDates}
	client, err := jobs.NewClient(st.DB(), deps)
	if err != nil {
		st.Close()
		return fmt.Errorf("模拟盘队列：%w", err)
	}
	deps.Client = client
	s.paperJobs = deps
	if opt.PaperWorkers {
		if err := client.Start(context.Background()); err != nil {
			st.Close()
			return fmt.Errorf("启动模拟盘队列：%w", err)
		}
	}
	return nil
}

func (s *Server) Close() error {
	if s.paperJobs == nil {
		return nil
	}
	if s.paperJobs.Client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_ = s.paperJobs.Client.Stop(ctx)
	}
	if s.paperJobs.Store != nil {
		return s.paperJobs.Store.Close()
	}
	return nil
}

// persist 必须在持有 mu 时调用（New 阶段除外）。
// 采用「写临时文件 + 原子替换」，避免进程崩溃时把因子库写成半截文件。
func (s *Server) persist() {
	if s.store == "" {
		return
	}
	b, err := json.MarshalIndent(s.factors, "", "  ")
	if err != nil {
		// 指标摘要里可能有 NaN/Inf，走清洗路径，否则整库写入会静默失败
		b, err = json.MarshalIndent(toJSONAny(reflect.ValueOf(s.factors)), "", "  ")
		if err != nil {
			return
		}
	}
	writeAtomic(s.store, b)
}

// persistCombos 必须在持有 mu 时调用。
func (s *Server) persistCombos() {
	if s.comboStore == "" {
		return
	}
	b, err := json.MarshalIndent(s.combos, "", "  ")
	if err != nil {
		b, err = json.MarshalIndent(toJSONAny(reflect.ValueOf(s.combos)), "", "  ")
		if err != nil {
			return
		}
	}
	writeAtomic(s.comboStore, b)
}

// writeAtomic 写临时文件后原子替换，避免进程崩溃写坏落盘文件。
func writeAtomic(path string, b []byte) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, ".combo-*.tmp")
	if err != nil {
		return
	}
	name := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(name)
		return
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(name)
		return
	}
	tmp.Close()
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
	}
}

// Handler 组装路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("/api/session", s.handleSession)
	mux.HandleFunc("GET /api/meta", s.handleMeta)
	mux.HandleFunc("GET /api/help", s.handleHelp)
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("POST /api/check", s.handleCheck)
	mux.HandleFunc("POST /api/evaluate", s.handleEvaluate)
	mux.HandleFunc("POST /api/compose", s.handleCompose)
	mux.HandleFunc("GET /api/combos", s.handleListCombos)
	mux.HandleFunc("POST /api/combos", s.handleCreateCombo)
	mux.HandleFunc("PUT /api/combos/{id}", s.handleUpdateCombo)
	mux.HandleFunc("DELETE /api/combos/{id}", s.handleDeleteCombo)
	mux.HandleFunc("POST /api/combos/{id}/evaluate", s.handleEvaluateCombo)
	mux.HandleFunc("GET /api/kline", s.handleKLine)
	mux.HandleFunc("GET /api/paper", s.handleListPaper)
	mux.HandleFunc("POST /api/paper", s.handleCreatePaper)
	mux.HandleFunc("POST /api/paper/sync", s.handleSyncAllPaper)
	mux.HandleFunc("GET /api/paper/{id}", s.handleGetPaper)
	mux.HandleFunc("DELETE /api/paper/{id}", s.handleDeletePaper)
	mux.HandleFunc("POST /api/paper/{id}/sync", s.handleSyncPaper)
	mux.HandleFunc("GET /api/factors", s.handleListFactors)
	mux.HandleFunc("POST /api/factors", s.handleCreateFactor)
	mux.HandleFunc("GET /api/factors/export", s.handleExport)
	mux.HandleFunc("POST /api/factors/import", s.handleImport)
	mux.HandleFunc("POST /api/factors/batch-evaluate", s.handleBatchEvaluate)
	mux.HandleFunc("GET /api/factors/{id}", s.handleGetFactor)
	mux.HandleFunc("PUT /api/factors/{id}", s.handleUpdateFactor)
	mux.HandleFunc("DELETE /api/factors/{id}", s.handleDeleteFactor)
	mux.HandleFunc("POST /api/factors/{id}/copy", s.handleCopyFactor)
	mux.HandleFunc("POST /api/assist", s.handleAssist)
	mux.HandleFunc("GET /api/assist/ws", s.handleAssistWS)
	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("PUT /api/settings", s.handleSaveSettings)
	mux.HandleFunc("POST /api/settings/test", s.handleTestSettings)
	mux.HandleFunc("POST /api/admin/reload", s.handleReload)
	if s.Web != "" {
		mux.HandleFunc("/", s.handleStatic)
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"service": "factor-lab", "api": "/api/*"})
		})
	}
	return s.accessLog(s.recoverPanic(securityHeaders(s.auth(cors(s.Opt.CORSOrigins)(mux)))))
}

// statusWriter 记录响应状态码，用于访问日志。
type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status = code
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.status = http.StatusOK
		w.wrote = true
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("hijack 不被支持")
	}
	w.status = http.StatusSwitchingProtocols
	w.wrote = true
	return h.Hijack()
}

// accessLog 记录 API 访问（生产排障必需）。静态资源不打日志，避免噪音。
// 每个请求带 request_id 并在响应头回传，便于把前端报错与服务端日志对上。
func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		rid := newRequestID()
		w.Header().Set("X-Request-Id", rid)
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		slog.Info("api",
			"request_id", rid,
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"ms", time.Since(start).Milliseconds(),
			"ip", clientIP(r),
		)
	})
}

// newRequestID 生成 16 位十六进制请求 ID。
func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b[:])
}

// handleReload 热加载数据面板（行情更新后无需重启）。
func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	if s.Opt.Reload == nil {
		writeErr(w, http.StatusNotImplemented, "当前数据源不支持热加载")
		return
	}
	// 未配置 token 时，只允许本机调用，避免把重载接口暴露出去
	if s.Opt.APIToken == "" {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		if host != "127.0.0.1" && host != "::1" {
			writeErr(w, http.StatusForbidden, "热加载需要配置 -api-token")
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	p, err := s.Opt.Reload(ctx)
	if err != nil {
		slog.Error("热加载失败", "err", err)
		writeErr(w, http.StatusInternalServerError, "热加载失败: "+err.Error())
		return
	}
	s.p.Store(p)
	s.clearUniverseCache()
	from, to := p.Range()
	slog.Info("数据面板已热加载", "dates", p.T(), "stocks", p.N(), "from", from, "to", to)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "dates": p.T(), "stocks": p.N(), "fields": len(p.Fields()),
		"from": from, "to": to,
	})
}

// recoverPanic 兜住 handler 里的 panic，避免单个请求把整个进程带崩。
func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("请求处理 panic", "path", r.URL.Path, "panic", fmt.Sprint(rec), "stack", string(debug.Stack()))
				writeErr(w, http.StatusInternalServerError, "服务内部错误")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ---- 基础信息 ----

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	from, to := s.panel().Range()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"panel":       map[string]any{"dates": s.panel().T(), "stocks": s.panel().N(), "fields": len(s.panel().Fields())},
		"range":       map[string]any{"from": from, "to": to},
		"llm":         s.llmName(),
		"data_source": s.Opt.DataSource,
		"stock_pool":  s.Opt.StockPool,
		"data_report": s.Opt.Report,
		"auth":        s.Opt.APIToken != "",
	})
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	sigs := ops.Sigs()
	names := make([]string, 0, len(sigs))
	for n := range sigs {
		// and/or/not 是 DSL 关键字，只能使用中缀/前缀语法，不能按函数调用。
		if n == "and" || n == "or" || n == "not" {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)
	type sigInfo struct {
		Name string `json:"name"`
		Args int    `json:"args"`
		Min  int    `json:"min_args"`
		Max  int    `json:"max_args"`
	}
	opList := make([]sigInfo, 0, len(sigs))
	for _, n := range names {
		opList = append(opList, sigInfo{Name: n, Args: sigs[n].MinArgs, Min: sigs[n].MinArgs, Max: sigs[n].MaxArgs})
	}
	from, to := s.panel().Range()
	fields := make([]string, 0, len(s.panel().Fields()))
	for _, f := range s.panel().Fields() {
		if hiddenPanelFields[f] {
			continue
		}
		fields = append(fields, f)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"fields":              fields,
		"operators":           opList,
		"eval_default":        eval.DefaultConfig(),
		"range":               map[string]any{"from": from, "to": to},
		"universes":           data.Universes,
		"universe_switchable": s.Opt.LoadUniverse != nil,
		"stock_pool":          s.Opt.StockPool,
	})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	byCat := map[string]int{}
	bySrc := map[string]int{}
	evaluated := 0
	var rics []float64
	for _, f := range s.factors {
		byCat[f.Category]++
		bySrc[f.Source]++
		if f.Summary != nil {
			evaluated++
			rics = append(rics, f.Summary.RankICMean)
		}
	}
	// 榜单：按 |RankIC| 取前 5
	ranked := make([]Factor, 0, len(s.factors))
	for _, f := range s.factors {
		if f.Summary != nil {
			ranked = append(ranked, f)
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		return abs(ranked[i].Summary.RankICMean) > abs(ranked[j].Summary.RankICMean)
	})
	top := ranked
	if len(top) > 5 {
		top = top[:5]
	}
	from, to := s.panel().Range()
	writeJSON(w, http.StatusOK, map[string]any{
		"total":       len(s.factors),
		"evaluated":   evaluated,
		"by_category": byCat,
		"by_source":   bySrc,
		"top":         top,
		"panel":       map[string]any{"dates": s.panel().T(), "stocks": s.panel().N(), "fields": len(s.panel().Fields()), "from": from, "to": to},
		"operators":   len(ops.Sigs()),
		"llm":         s.llmName(),
	})
}

// ---- 表达式评价 ----

type evalConfig struct {
	Horizon  int     `json:"horizon"`
	Groups   int     `json:"groups"`
	Cost     float64 `json:"cost"`
	Start    string  `json:"start"` // 2006-01-02，可空
	End      string  `json:"end"`
	Universe string  `json:"universe"` // 指数代码，空 = 当前启动股票池
}

type checkReq struct {
	Expr string `json:"expr"`
}

func (s *Server) handleCheck(w http.ResponseWriter, r *http.Request) {
	var req checkReq
	if !readJSON(w, r, &req) {
		return
	}
	errs := engine.Check(req.Expr, s.panel())
	writeJSON(w, http.StatusOK, map[string]any{"ok": len(errs) == 0, "errors": errs})
}

func (s *Server) parseRange(start, end string) (time.Time, time.Time, error) {
	var t0, t1 time.Time
	var err error
	if strings.TrimSpace(start) != "" {
		if t0, err = time.Parse("2006-01-02", strings.TrimSpace(start)); err != nil {
			return t0, t1, fmt.Errorf("起始日期格式应为 YYYY-MM-DD")
		}
	}
	if strings.TrimSpace(end) != "" {
		if t1, err = time.Parse("2006-01-02", strings.TrimSpace(end)); err != nil {
			return t0, t1, fmt.Errorf("结束日期格式应为 YYYY-MM-DD")
		}
	}
	return t0, t1, nil
}

func (s *Server) clearUniverseCache() {
	s.uniMu.Lock()
	s.uni = make(map[string]*uniPanel)
	s.uniMu.Unlock()
}

// universePanel 返回指定指数的成分面板。与启动股票池相同则复用当前面板；否则懒加载并缓存。
func (s *Server) universePanel(ctx context.Context, index string) (*panel.Panel, error) {
	index = data.CanonicalIndex(index)
	pool := data.CanonicalIndex(s.Opt.StockPool)
	if index == "" || index == data.AllMarket || index == pool {
		return s.panel(), nil
	}
	if s.Opt.LoadUniverse == nil {
		return nil, fmt.Errorf("当前数据源不支持切换股票池")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	s.uniMu.Lock()
	if e, ok := s.uni[index]; ok {
		s.uniMu.Unlock()
		select {
		case <-e.ready:
			return e.p, e.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	e := &uniPanel{ready: make(chan struct{})}
	s.uni[index] = e
	s.uniMu.Unlock()

	name := data.UniverseName(index)
	slog.Info("加载评价股票池…", "index", index, "name", name)
	p, err := s.Opt.LoadUniverse(ctx, index)
	if err != nil {
		s.uniMu.Lock()
		delete(s.uni, index)
		s.uniMu.Unlock()
		e.err = fmt.Errorf("加载股票池 %s 失败：%w", name, err)
		close(e.ready)
		return nil, e.err
	}
	e.p = p
	close(e.ready)
	from, to := p.Range()
	slog.Info("评价股票池就绪", "index", index, "name", name, "stocks", p.N(), "dates", p.T(), "from", from, "to", to)
	return p, nil
}

// panelFor 按评价参数裁剪面板（股票池 + 日期区间）。
func (s *Server) panelFor(ctx context.Context, c evalConfig) (*panel.Panel, eval.Config, error) {
	cfg := eval.DefaultConfig()
	if c.Horizon >= 1 {
		cfg.Horizon = c.Horizon
	}
	if c.Groups >= 2 {
		cfg.Groups = c.Groups
	}
	if c.Cost >= 0 {
		cfg.Cost = c.Cost
	}
	t0, t1, err := s.parseRange(c.Start, c.End)
	if err != nil {
		return nil, cfg, err
	}
	p, err := s.universePanel(ctx, c.Universe)
	if err != nil {
		return nil, cfg, err
	}
	if p == nil || p.T() == 0 {
		return nil, cfg, fmt.Errorf("数据面板为空，请先在设置中填写 Tushare Token 并等待数据加载")
	}
	if !t0.IsZero() || !t1.IsZero() {
		if p, err = p.Slice(t0, t1); err != nil {
			return nil, cfg, err
		}
	}
	if p.T() < cfg.Horizon+20 {
		return nil, cfg, fmt.Errorf("区间过短（%d 个交易日），至少需要 %d 个", p.T(), cfg.Horizon+20)
	}
	return p, cfg, nil
}

type evalReq struct {
	Expr string `json:"expr"`
	evalConfig
}

func (s *Server) handleEvaluate(w http.ResponseWriter, r *http.Request) {
	var req evalReq
	if !readJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Expr) == "" {
		writeErr(w, http.StatusBadRequest, "expr 不能为空")
		return
	}
	p, cfg, err := s.panelFor(r.Context(), req.evalConfig)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	col, errs := engine.Run(req.Expr, p)
	if len(errs) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"errors": errsToStrings(errs)})
		return
	}
	m, err := eval.Evaluate(p, col, cfg)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// ---- 因子库 ----

func (s *Server) handleListFactors(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	out := make([]Factor, len(s.factors))
	copy(out, s.factors)
	s.mu.Unlock()
	s.markAvailability(out)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) findLocked(id int) int {
	for i, f := range s.factors {
		if f.ID == id {
			return i
		}
	}
	return -1
}

func (s *Server) handleGetFactor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.findLocked(id); i >= 0 {
		out := []Factor{s.factors[i]}
		s.markAvailability(out)
		writeJSON(w, http.StatusOK, out[0])
		return
	}
	writeErr(w, http.StatusNotFound, "因子不存在")
}

func (s *Server) validateExpr(expr string) []string {
	return engine.Check(expr, s.panel())
}

func (s *Server) handleCreateFactor(w http.ResponseWriter, r *http.Request) {
	var f Factor
	if !readJSON(w, r, &f) {
		return
	}
	f.Name = strings.TrimSpace(f.Name)
	f.Expr = strings.TrimSpace(f.Expr)
	if f.Name == "" || f.Expr == "" {
		writeErr(w, http.StatusBadRequest, "name 与 expr 不能为空")
		return
	}
	if errs := s.validateExpr(f.Expr); len(errs) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"errors": errs})
		return
	}
	if f.Source == "" {
		f.Source = "user"
	}
	if f.Category == "" {
		f.Category = "user"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, x := range s.factors {
		if x.Name == f.Name {
			writeErr(w, http.StatusConflict, "同名因子已存在："+f.Name)
			return
		}
	}
	now := time.Now()
	f.ID = s.nextID
	s.nextID++
	f.CreatedAt, f.UpdatedAt = now, now
	f.Summary = nil
	s.factors = append(s.factors, f)
	s.persist()
	writeJSON(w, http.StatusCreated, f)
}

func (s *Server) handleUpdateFactor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in Factor
	if !readJSON(w, r, &in) {
		return
	}
	if errs := s.validateExpr(in.Expr); len(errs) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"errors": errs})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.findLocked(id)
	if i < 0 {
		writeErr(w, http.StatusNotFound, "因子不存在")
		return
	}
	cur := &s.factors[i]
	changed := false
	if name := strings.TrimSpace(in.Name); name != "" && name != cur.Name {
		for _, x := range s.factors {
			if x.Name == name {
				writeErr(w, http.StatusConflict, "同名因子已存在："+name)
				return
			}
		}
		cur.Name = name
		changed = true
	}
	if expr := strings.TrimSpace(in.Expr); expr != cur.Expr {
		cur.Expr = expr
		cur.Summary = nil // 表达式变了，旧指标作废
		changed = true
	}
	if in.Category != "" && in.Category != cur.Category {
		cur.Category = in.Category
		changed = true
	}
	if in.Desc != cur.Desc {
		cur.Desc = in.Desc
		changed = true
	}
	if changed {
		cur.UpdatedAt = time.Now()
	}
	s.persist()
	writeJSON(w, http.StatusOK, *cur)
}

func (s *Server) handleDeleteFactor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.findLocked(id)
	if i < 0 {
		writeErr(w, http.StatusNotFound, "因子不存在")
		return
	}
	s.factors = append(s.factors[:i], s.factors[i+1:]...)
	s.persist()
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) handleCopyFactor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.findLocked(id)
	if i < 0 {
		writeErr(w, http.StatusNotFound, "因子不存在")
		return
	}
	src := s.factors[i]
	now := time.Now()
	c := src
	c.ID = s.nextID
	s.nextID++
	c.Name = uniqueName(s.factors, src.Name+"_副本")
	c.Source = "user"
	c.CreatedAt, c.UpdatedAt = now, now
	c.Summary = nil
	s.factors = append(s.factors, c)
	s.persist()
	writeJSON(w, http.StatusCreated, c)
}

func uniqueName(existing []Factor, base string) string {
	used := make(map[string]bool, len(existing))
	for _, f := range existing {
		used[f.Name] = true
	}
	if !used[base] {
		return base
	}
	for n := 2; ; n++ {
		cand := fmt.Sprintf("%s%d", base, n)
		if !used[cand] {
			return cand
		}
	}
}

type exportItem struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Expr     string `json:"expr"`
	Desc     string `json:"desc,omitempty"`
	Source   string `json:"source,omitempty"`
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	items := make([]exportItem, 0, len(s.factors))
	for _, f := range s.factors {
		items = append(items, exportItem{Name: f.Name, Category: f.Category, Expr: f.Expr, Desc: f.Desc, Source: f.Source})
	}
	s.mu.Unlock()
	b, _ := json.MarshalIndent(items, "", "  ")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=factors-%s.json", time.Now().Format("20060102")))
	_, _ = w.Write(b)
}

type importReq struct {
	Factors []exportItem `json:"factors"`
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	var body json.RawMessage
	if !readJSON(w, r, &body) {
		return
	}
	// 兼容两种格式：裸数组 / {"factors": [...]}
	var items []exportItem
	if err := json.Unmarshal(body, &items); err != nil {
		var wrap importReq
		if err2 := json.Unmarshal(body, &wrap); err2 != nil {
			writeErr(w, http.StatusBadRequest, "导入格式错误：应为因子数组或 {factors:[...]}")
			return
		}
		items = wrap.Factors
	}
	if len(items) == 0 {
		writeErr(w, http.StatusBadRequest, "导入内容为空")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	added, updated, skipped := 0, 0, 0
	var errs []string
	for _, it := range items {
		name := strings.TrimSpace(it.Name)
		expr := strings.TrimSpace(it.Expr)
		if name == "" || expr == "" {
			skipped++
			continue
		}
		if errs2 := s.validateExpr(expr); len(errs2) > 0 {
			errs = append(errs, fmt.Sprintf("%s: %s", name, strings.Join(errs2, "; ")))
			skipped++
			continue
		}
		cat := it.Category
		if cat == "" {
			cat = "user"
		}
		src := it.Source
		if src == "" {
			src = "user"
		}
		idx := -1
		for i, f := range s.factors {
			if f.Name == name {
				idx = i
				break
			}
		}
		if idx >= 0 {
			// 表达式变了吗？只有变了才作废旧指标，否则一次导入会把全部指标清空
			if s.factors[idx].Expr != expr {
				s.factors[idx].Expr = expr
				s.factors[idx].Summary = nil
				s.factors[idx].UpdatedAt = now
			}
			if s.factors[idx].Category != cat {
				s.factors[idx].Category = cat
				s.factors[idx].UpdatedAt = now
			}
			if it.Desc != "" && s.factors[idx].Desc != it.Desc {
				s.factors[idx].Desc = it.Desc
				s.factors[idx].UpdatedAt = now
			}
			updated++
			continue
		}
		s.factors = append(s.factors, Factor{
			ID: s.nextID, Name: name, Category: cat, Expr: expr, Desc: it.Desc,
			Source: src, CreatedAt: now, UpdatedAt: now,
		})
		s.nextID++
		added++
	}
	s.persist()
	writeJSON(w, http.StatusOK, map[string]any{
		"added": added, "updated": updated, "skipped": skipped, "errors": errs, "total": len(s.factors),
	})
}

type batchEvalReq struct {
	IDs []int `json:"ids"`
	evalConfig
}

// handleBatchEvaluate 批量评价并回写摘要（只是把同一套裁判跑多遍，口径不会因批量而改变）。
func (s *Server) handleBatchEvaluate(w http.ResponseWriter, r *http.Request) {
	var req batchEvalReq
	if !readJSON(w, r, &req) {
		return
	}
	p, cfg, err := s.panelFor(r.Context(), req.evalConfig)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	from, to := p.Range()

	s.mu.Lock()
	targets := make([]Factor, 0, len(s.factors))
	wanted := map[int]bool{}
	for _, id := range req.IDs {
		wanted[id] = true
	}
	for _, f := range s.factors {
		if len(wanted) == 0 || wanted[f.ID] {
			targets = append(targets, f)
		}
	}
	s.mu.Unlock()

	// 先在当前面板上做可用性预检：换数据源后缺字段的因子直接报不可用，不浪费算力
	var preErrs []string
	usable := targets[:0]
	for _, f := range targets {
		if errs := engine.Check(f.Expr, p); len(errs) > 0 {
			preErrs = append(preErrs, fmt.Sprintf("%s: %s（当前数据源不支持）", f.Name, strings.Join(errs, "; ")))
			continue
		}
		usable = append(usable, f)
	}
	targets = usable

	type result struct {
		id  int
		sum *Summary
		err string
	}
	results := make([]result, len(targets))
	sem := make(chan struct{}, 4) // 限并发，避免把 CPU 打满
	var wg sync.WaitGroup
	for i, f := range targets {
		wg.Add(1)
		go func(i int, f Factor) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			col, errs := engine.Run(f.Expr, p)
			if len(errs) > 0 {
				results[i] = result{id: f.ID, err: strings.Join(errsToStrings(errs), "; ")}
				return
			}
			m, err := eval.Evaluate(p, col, cfg)
			if err != nil {
				results[i] = result{id: f.ID, err: err.Error()}
				return
			}
			results[i] = result{id: f.ID, sum: Summarize(m, from, to, time.Now())}
		}(i, f)
	}
	wg.Wait()

	s.mu.Lock()
	ok, failed := 0, 0
	errs := append([]string{}, preErrs...)
	for _, res := range results {
		if res.err != "" {
			failed++
			errs = append(errs, res.err)
			continue
		}
		if i := s.findLocked(res.id); i >= 0 {
			s.factors[i].Summary = res.sum
			ok++
		}
	}
	s.persist()
	out := make([]Factor, len(s.factors))
	copy(out, s.factors)
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"evaluated": ok, "failed": failed, "errors": errs, "factors": out,
	})
}

// ---- 组合构建 ----

type composeReq struct {
	Name   string         `json:"name"`
	Items  []compose.Part `json:"items"`
	Method string         `json:"method"` // equal | manual
	evalConfig
}

type composeResp struct {
	Expr        string        `json:"expr"`
	Label       string        `json:"label"`
	Names       []string      `json:"names"`
	Correlation [][]float64   `json:"correlation"`
	Warnings    []string      `json:"warnings"`
	Errors      []string      `json:"errors"`
	Metrics     *eval.Metrics `json:"metrics,omitempty"`

	// 评价区间（不导出；供组合摘要落盘）
	from, to string
}

func (s *Server) handleCompose(w http.ResponseWriter, r *http.Request) {
	var req composeReq
	if !readJSON(w, r, &req) {
		return
	}
	// 用库内因子表达式覆盖（前端只传 id + 权重，避免表达式漂移）
	s.mu.Lock()
	for i := range req.Items {
		if f := s.factorByIDLocked(req.Items[i].ID); f != nil {
			req.Items[i].Expr = f.Expr
			if req.Items[i].Name == "" {
				req.Items[i].Name = f.Name
			}
		}
	}
	s.mu.Unlock()

	resp := s.runCompose(r.Context(), w, req.Items, req.Method, req.evalConfig)
	if resp == nil {
		return // 出错时响应已写
	}
	writeJSON(w, http.StatusOK, resp)
}

// runCompose 构建并评价一个因子组合；成功返回 resp（调用方负责写 200），
// 出错时已写错误响应并返回 nil。from/to 供调用方落摘要。
func (s *Server) runCompose(ctx context.Context, w http.ResponseWriter, items []compose.Part, method string, c evalConfig) *composeResp {
	if len(items) < 2 {
		writeErr(w, http.StatusBadRequest, "组合至少需要 2 个因子")
		return nil
	}
	expr, err := compose.Build(items, method)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return nil
	}
	resp := &composeResp{
		Expr:  expr,
		Label: compose.Label(items),
		Names: make([]string, len(items)),
	}
	for i, it := range items {
		resp.Names[i] = it.Name
	}

	p, cfg, err := s.panelFor(ctx, c)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return nil
	}
	resp.from, resp.to = p.Range()
	if errs := s.validateExpr(expr); len(errs) > 0 {
		resp.Errors = errs
		writeJSON(w, http.StatusUnprocessableEntity, resp)
		return nil
	}

	// 分因子列 + 两两同质化相关性（>0.7 告警，对应调研结论：组合的价值取决于分因子低相关）
	cols := make([]panel.Col, len(items))
	for i, it := range items {
		col, errs := engine.Run(it.Expr, p)
		if len(errs) > 0 {
			resp.Errors = append(resp.Errors, fmt.Sprintf("%s: %v", it.Name, errsToStrings(errs)))
			continue
		}
		cols[i] = col
	}
	n := len(items)
	resp.Correlation = make([][]float64, n)
	for i := 0; i < n; i++ {
		resp.Correlation[i] = make([]float64, n)
		for j := 0; j < n; j++ {
			switch {
			case i == j:
				resp.Correlation[i][j] = 1
			case cols[i] == nil || cols[j] == nil:
				resp.Correlation[i][j] = 0
			default:
				c, err := compose.CrossCorr(cols[i], cols[j], p.T(), p.N())
				if err != nil || c != c { // err 或 NaN
					resp.Correlation[i][j] = 0
				} else {
					resp.Correlation[i][j] = c
				}
			}
		}
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if v := resp.Correlation[i][j]; v > 0.7 {
				resp.Warnings = append(resp.Warnings,
					fmt.Sprintf("「%s」与「%s」秩相关 %.2f，高度同质（>0.7），组合分散效果有限", resp.Names[i], resp.Names[j], v))
			}
		}
	}

	col, errs := engine.Run(expr, p)
	if len(errs) > 0 {
		resp.Errors = append(resp.Errors, errsToStrings(errs)...)
		writeJSON(w, http.StatusUnprocessableEntity, resp)
		return nil
	}
	m, err := eval.Evaluate(p, col, cfg)
	if err != nil {
		resp.Errors = append(resp.Errors, err.Error())
		writeJSON(w, http.StatusUnprocessableEntity, resp)
		return nil
	}
	resp.Metrics = m
	return resp
}

// ---- 因子组合（卡片式管理） ----

// comboReq 是创建/更新组合的请求体。
type comboReq struct {
	Name   string      `json:"name"`
	Method string      `json:"method"`
	Items  []ComboItem `json:"items"`
	Desc   string      `json:"desc"`
}

// normalizeComboItems 校验条目并把名称对齐到因子库（删除后的因子名字不再权威）。
func (s *Server) normalizeComboItems(items []ComboItem) ([]ComboItem, string) {
	if len(items) < 2 {
		return nil, "组合至少需要 2 个因子"
	}
	seen := make(map[int]bool, len(items))
	for i := range items {
		it := &items[i]
		if seen[it.ID] {
			return nil, fmt.Sprintf("因子 #%d 重复加入", it.ID)
		}
		seen[it.ID] = true
		if it.Weight < 0 {
			return nil, fmt.Sprintf("「%s」权重不能为负", it.Name)
		}
		if it.Direction == 0 {
			it.Direction = 1
		}
		if it.Direction != 1 && it.Direction != -1 {
			return nil, fmt.Sprintf("「%s」方向只能是 1 或 -1", it.Name)
		}
		if f := s.factorByIDLocked(it.ID); f != nil {
			it.Name = f.Name
		}
	}
	return items, ""
}

func (s *Server) handleListCombos(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	out := make([]Combo, len(s.combos))
	copy(out, s.combos)
	// 因子库快照：用于标记每个条目引用的因子是否仍存在/可用
	snap := make(map[int]Factor, len(s.factors))
	for _, f := range s.factors {
		snap[f.ID] = f
	}
	s.mu.Unlock()

	p := s.panel()
	for i := range out {
		for j := range out[i].Items {
			it := &out[i].Items[j]
			f, ok := snap[it.ID]
			switch {
			case !ok:
				it.Missing, it.Available = true, false
				it.Reason = "因子已删除"
			case len(engine.Check(f.Expr, p)) > 0:
				it.Missing, it.Available = false, false
				it.Reason = "当前数据源不可用：" + strings.Join(engine.Check(f.Expr, p), "; ")
			default:
				it.Missing, it.Available, it.Reason = false, true, ""
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateCombo(w http.ResponseWriter, r *http.Request) {
	var req comboReq
	if !readJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "请填写组合名称")
		return
	}
	if req.Method != "equal" && req.Method != "manual" {
		req.Method = "equal"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.combos {
		if c.Name == name {
			writeErr(w, http.StatusConflict, "同名组合已存在："+name)
			return
		}
	}
	items, msg := s.normalizeComboItems(req.Items)
	if msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	now := time.Now()
	c := Combo{
		ID: s.nextComboID, Name: name, Method: req.Method, Items: items,
		Desc: strings.TrimSpace(req.Desc), CreatedAt: now, UpdatedAt: now,
	}
	s.nextComboID++
	s.combos = append(s.combos, c)
	s.persistCombos()
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleUpdateCombo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req comboReq
	if !readJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "请填写组合名称")
		return
	}
	if req.Method != "equal" && req.Method != "manual" {
		req.Method = "equal"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.findComboLocked(id)
	if i < 0 {
		writeErr(w, http.StatusNotFound, "组合不存在")
		return
	}
	for k, c := range s.combos {
		if k != i && c.Name == name {
			writeErr(w, http.StatusConflict, "同名组合已存在："+name)
			return
		}
	}
	items, msg := s.normalizeComboItems(req.Items)
	if msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	cur := &s.combos[i]
	changed := name != cur.Name || req.Method != cur.Method || req.Desc != cur.Desc
	if len(items) != len(cur.Items) {
		changed = true
	} else {
		for k := range items {
			if items[k] != cur.Items[k] {
				changed = true
				break
			}
		}
	}
	if !changed && req.Items != nil {
		writeJSON(w, http.StatusOK, *cur)
		return
	}
	cur.Name, cur.Method, cur.Items, cur.Desc = name, req.Method, items, strings.TrimSpace(req.Desc)
	if changed {
		cur.UpdatedAt = time.Now()
		cur.Summary = nil // 成分变了，旧摘要作废
	}
	s.persistCombos()
	writeJSON(w, http.StatusOK, *cur)
}

func (s *Server) handleDeleteCombo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.findComboLocked(id)
	if i < 0 {
		writeErr(w, http.StatusNotFound, "组合不存在")
		return
	}
	s.combos = append(s.combos[:i], s.combos[i+1:]...)
	s.persistCombos()
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// handleEvaluateCombo 评价一个已保存的组合（参数前端弹窗收集），并把摘要写回组合。
func (s *Server) handleEvaluateCombo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var cfg evalConfig
	if !readJSON(w, r, &cfg) {
		return
	}
	s.mu.Lock()
	i := s.findComboLocked(id)
	if i < 0 {
		s.mu.Unlock()
		writeErr(w, http.StatusNotFound, "组合不存在")
		return
	}
	c := s.combos[i] // 拷贝出来，锁外计算
	items, missing := s.comboPartsLocked(c)
	s.mu.Unlock()
	if len(missing) > 0 {
		writeErr(w, http.StatusUnprocessableEntity, "组合内因子已删除，请先编辑组合："+strings.Join(missing, "、"))
		return
	}

	resp := s.runCompose(r.Context(), w, items, c.Method, cfg)
	if resp == nil {
		return // 出错时响应已写
	}
	if resp.Metrics != nil {
		// 写回摘要：卡片上直接展示最近一次评价的指标
		s.mu.Lock()
		if j := s.findComboLocked(id); j >= 0 {
			s.combos[j].Summary = Summarize(resp.Metrics, resp.from, resp.to, time.Now())
			s.combos[j].UpdatedAt = time.Now()
			s.persistCombos()
		}
		s.mu.Unlock()
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) findComboLocked(id int) int {
	for i := range s.combos {
		if s.combos[i].ID == id {
			return i
		}
	}
	return -1
}

// comboPartsLocked 把组合条目解析成合成用的分因子（表达式取自当前因子库）。
func (s *Server) comboPartsLocked(c Combo) (parts []compose.Part, missing []string) {
	parts = make([]compose.Part, 0, len(c.Items))
	for _, it := range c.Items {
		part := compose.Part{ID: it.ID, Name: it.Name, Weight: it.Weight, Direction: it.Direction}
		if f := s.factorByIDLocked(it.ID); f != nil {
			part.Expr = f.Expr
			part.Name = f.Name
		} else {
			missing = append(missing, it.Name)
		}
		parts = append(parts, part)
	}
	return parts, missing
}

func (s *Server) factorByIDLocked(id int) *Factor {
	if i := s.findLocked(id); i >= 0 {
		return &s.factors[i]
	}
	return nil
}

// ---- AI 助手 ----

// handleAssist：因子工厂里的对话式辅助编写（人机协作，AI 产出需用户确认后才应用到编辑器）。
type assistReq struct {
	Messages []agent.AssistMsg `json:"messages"`
	Expr     string            `json:"expr"` // 当前编辑器表达式或组合上下文
	Mode     string            `json:"mode"` // factor | combo
}

func (s *Server) handleAssist(w http.ResponseWriter, r *http.Request) {
	var req assistReq
	if !readJSON(w, r, &req) {
		return
	}
	if err := validateAssistMessages(req.Messages); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// 生产纪律：模型未配置时明确报错，不做任何模板降级
	if s.currentLLM() == nil {
		writeErr(w, http.StatusServiceUnavailable, "模型未配置：请点左下角「设置」配置模型后再使用 AI 助手")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	var (
		res *agent.AssistResult
		err error
	)
	if strings.ToLower(strings.TrimSpace(req.Mode)) == agent.AssistModeCombo {
		res, err = agent.RunComboAssist(ctx, s.currentLLM(), req.Messages, req.Expr, s.assistFactorRefs())
	} else {
		res, err = agent.RunAssist(ctx, s.currentLLM(), req.Messages, req.Expr, s.panel())
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "辅助失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) assistFactorRefs() []agent.FactorRef {
	s.mu.Lock()
	fs := make([]Factor, len(s.factors))
	copy(fs, s.factors)
	s.mu.Unlock()
	s.markAvailability(fs)
	out := make([]agent.FactorRef, 0, len(fs))
	for _, f := range fs {
		ref := agent.FactorRef{ID: f.ID, Name: f.Name, Category: f.Category, Available: f.Available}
		if f.Summary != nil {
			ref.RankIC = f.Summary.RankICMean
			ref.ICIR = f.Summary.RankICIR
		}
		out = append(out, ref)
	}
	return out
}

// ---- 静态前端 ----

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeErr(w, http.StatusNotFound, "未知接口")
		return
	}
	path := filepath.Join(s.Web, filepath.Clean(strings.TrimPrefix(r.URL.Path, "/")))
	if path == s.Web {
		path = filepath.Join(s.Web, "index.html")
	}
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		path = filepath.Join(s.Web, "index.html") // SPA 回退
	}
	// 带内容哈希的构建产物可长期缓存；入口 HTML 必须每次校验，
	// 否则前端重新构建后浏览器会拿旧 HTML 去请求已删除的旧 JS，导致白屏。
	if strings.Contains(r.URL.Path, "/assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	}
	http.ServeFile(w, r, path)
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	b, err := json.Marshal(v)
	if err != nil {
		// 因子覆盖率低时 IC/净值序列可能出现 NaN/Inf，而 encoding/json 不支持非有限浮点。
		// 兜底：递归清洗为 null（前端显示 "—"），避免整个响应变成空 body。
		b, err = json.Marshal(toJSONAny(reflect.ValueOf(v)))
		if err != nil {
			b = []byte(`{"error":"结果序列化失败"}`)
		}
	}
	w.WriteHeader(code)
	_, _ = w.Write(b)
}

// marshalerType 标记实现了自定义 JSON 编码的类型（如 time.Time），必须原样交给标准库处理。
var marshalerType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()

// toJSONAny 把任意值递归转成可安全 JSON 化的 interface 树：非有限浮点 → nil。
func toJSONAny(rv reflect.Value) any {
	if !rv.IsValid() {
		return nil
	}
	// time.Time 等自带 MarshalJSON 的类型不能拆字段
	if rv.CanInterface() && rv.Type().Implements(marshalerType) {
		return rv.Interface()
	}
	switch rv.Kind() {
	case reflect.Float32, reflect.Float64:
		f := rv.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil
		}
		return f
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return rv.Interface()
	case reflect.Ptr, reflect.Interface:
		if rv.IsNil() {
			return nil
		}
		return toJSONAny(rv.Elem())
	case reflect.Slice, reflect.Array:
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = toJSONAny(rv.Index(i))
		}
		return out
	case reflect.Map:
		out := make(map[string]any, rv.Len())
		for _, k := range rv.MapKeys() {
			out[fmt.Sprint(k.Interface())] = toJSONAny(rv.MapIndex(k))
		}
		return out
	case reflect.Struct:
		t := rv.Type()
		out := make(map[string]any, t.NumField())
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			tag := strings.Split(f.Tag.Get("json"), ",")[0]
			if tag == "-" {
				continue
			}
			name := tag
			if name == "" {
				name = f.Name
			}
			out[name] = toJSONAny(rv.Field(i))
		}
		return out
	}
	return rv.Interface()
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败: "+err.Error())
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request) (int, bool) {
	var id int
	if _, err := fmt.Sscanf(r.PathValue("id"), "%d", &id); err != nil {
		writeErr(w, http.StatusBadRequest, "id 必须是整数")
		return 0, false
	}
	return id, true
}

func errsToStrings(errs []error) []string {
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.Error()
	}
	return out
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
