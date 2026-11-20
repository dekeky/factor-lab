// factor-lab 服务入口。
//
// 生产（默认）：用 Tushare Pro 取真实 A 股数据
//
//	go run ./cmd/server -addr 127.0.0.1:8080 -data tushare \
//	  -index 000300.SH -start 20200101
//
// Token 优先级：-tushare-token > 环境变量 TUSHARE_TOKEN > 界面「设置」写入的 data/settings.json
//
// 其他数据源：
//
//	-data csv -csv panel.csv          用户提供的 CSV 面板
//
// LLM（可选，未配置时 AI 助手在界面明确提示去「设置」里配置，不做降级）：
//
//	环境变量 LLM_BASE_URL / LLM_API_KEY / LLM_MODEL（OpenAI 协议兼容端点）
//	或在界面左下角「设置」里配置（写入 data/settings.json，保存即热生效）
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"factor-lab/internal/agent"
	"factor-lab/internal/api"
	"factor-lab/internal/data"
	"factor-lab/internal/panel"
)

func main() {
	var (
		addr      = flag.String("addr", "127.0.0.1:8080", "监听地址")
		dataMode  = flag.String("data", "tushare", "数据源: tushare（默认）| csv")
		csvPath   = flag.String("csv", "", "CSV 路径（data=csv 时必填）")
		tsToken   = flag.String("tushare-token", os.Getenv("TUSHARE_TOKEN"), "Tushare Pro token（也可设环境变量 TUSHARE_TOKEN，或在界面「设置」里填写）")
		tsIndex   = flag.String("index", "000300.SH", "指数代码（成分股票池）")
		tsTopN    = flag.Int("topn", 0, "成分取前 N 只（0 = 全部成分）")
		tsStart   = flag.String("start", "", "起始日期 YYYYMMDD（空 = 接口默认区间）")
		tsEnd     = flag.String("end", "", "结束日期 YYYYMMDD")
		parallel  = flag.Int("parallel", 4, "取数并发数")
		tsQPS     = flag.Float64("tushare-qps", 4, "Tushare 每秒请求上限")
		storePath = flag.String("store", "data/factors.json", "因子注册表持久化路径（空 = 不持久化）")
		webDir    = flag.String("web", "web/dist", "前端静态目录（不存在则只提供 API）")
		apiToken  = flag.String("api-token", os.Getenv("FACTOR_LAB_TOKEN"), "访问令牌（对外监听必填；空 = 仅限本机，不鉴权）")
		corsOrig  = flag.String("cors-origin", os.Getenv("FACTOR_LAB_CORS"), "允许跨域的来源，逗号分隔（空 = 同源专用）")
		logFormat = flag.String("log-format", "json", "日志格式: json（生产默认）| text")
	)
	flag.Parse()

	var handler slog.Handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	if *logFormat == "text" {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)

	// 生产红线：对外监听（非回环地址）必须配置访问令牌，避免因子库与模型配置裸奔。
	if !isLoopback(*addr) && *apiToken == "" {
		fatal("拒绝启动：对外监听必须配置访问令牌（FACTOR_LAB_TOKEN 或 -api-token），仅本机调试可用 127.0.0.1", "addr", *addr)
	}

	loadCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// 配置：优先用界面「设置」保存过的 settings.json，其次用环境变量。
	settingsPath := filepath.Join(filepath.Dir(*storePath), "settings.json")
	llmCfg, fromFile, err := agent.LoadSettings(settingsPath)
	if err != nil {
		slog.Warn("配置读取失败，回退环境变量", "err", err.Error())
	}
	if !fromFile {
		llmCfg = agent.DefaultSettings()
		if v := os.Getenv("LLM_BASE_URL"); v != "" {
			llmCfg.BaseURL = v
		}
		if v := os.Getenv("LLM_API_KEY"); v != "" {
			llmCfg.APIKey = v
		}
		if v := os.Getenv("LLM_MODEL"); v != "" {
			llmCfg.Model = v
		}
		llmCfg = llmCfg.ApplyFlat().Normalize()
	}

	tsTokenVal := strings.TrimSpace(*tsToken)
	if tsTokenVal == "" {
		tsTokenVal = llmCfg.TushareToken
	}

	var src data.Source
	switch *dataMode {
	case "tushare":
		src = &data.TushareSource{
			Token: tsTokenVal, IndexCode: *tsIndex, TopN: *tsTopN,
			Start: *tsStart, End: *tsEnd,
			Concurrency: *parallel, QPS: *tsQPS,
		}
	case "csv":
		if *csvPath == "" {
			fatal("data=csv 需要用 -csv 指定文件路径")
		}
		src = &data.CSVSource{Path: *csvPath}
	default:
		fatal("未知数据源（tushare | csv）", "data", *dataMode)
	}

	p := panel.New(nil, nil)
	if *dataMode == "tushare" && tsTokenVal == "" {
		slog.Warn("未配置 Tushare token，先启动界面。请在左下角「设置」填写，或设置 TUSHARE_TOKEN / -tushare-token。见 https://tushare.pro/user/token")
	} else {
		slog.Info("正在加载数据面板…", "source", src.Name())
		t0 := time.Now()
		loaded, err := src.Load(loadCtx)
		if err != nil {
			fatal("加载数据失败", "err", err)
		}
		p = loaded
		from, to := p.Range()
		slog.Info("面板就绪",
			"source", src.Name(), "dates", p.T(), "stocks", p.N(),
			"fields", len(p.Fields()), "from", from, "to", to,
			"elapsed", time.Since(t0).Round(time.Millisecond).String())
	}

	if rep, ok := src.(data.Reporter); ok {
		if r := rep.Report(); r != nil {
			slog.Info("数据质量",
				"bars", r.Bars, "st_rows", r.STRows, "st_dropped", r.STDropped,
				"roe_rows", r.ROERows, "has_roe", r.HasROE)
			for _, e := range r.OptionalErrs {
				slog.Warn("可选数据集缺失（相关因子将不可用）", "err", e)
			}
		}
	}
	var llm agent.LLM // nil = 未配置
	if el, err := llmCfg.NewLLM(loadCtx); err == nil {
		llm = el
		cfgSrc := "env"
		if fromFile {
			cfgSrc = "settings.json"
		}
		slog.Info("LLM 已启用", "engine", el.Name(), "base_url", llmCfg.BaseURL, "配置来源", cfgSrc)
	} else {
		slog.Warn("模型未配置，AI 助手不可用；请在界面左侧「设置」里配置模型",
			"reason", err.Error(), "settings", settingsPath)
	}

	if _, err := os.Stat(*webDir); err != nil {
		slog.Warn("前端目录不存在，仅提供 API", "dir", *webDir)
		*webDir = ""
	}
	if *storePath != "" {
		if err := os.MkdirAll(filepath.Dir(*storePath), 0o755); err != nil {
			fatal("创建存储目录失败", "err", err)
		}
	}

	stockPool := *tsIndex
	if ts, ok := src.(*data.TushareSource); ok && ts.IndexCode != "" {
		stockPool = ts.IndexCode
	}
	srv, err := api.New(p, llm, *storePath, *webDir, api.Options{
		DataSource:   src.Name(),
		StockPool:    stockPool,
		Report:       reportOf(src),
		APIToken:     *apiToken,
		CORSOrigins:  splitList(*corsOrig),
		LLMSettings:  llmCfg,
		SettingsPath: settingsPath,
		// 热加载：重新走一遍数据源加载，成功后原子替换面板（不中断在读的请求）
		Reload: func(ctx context.Context) (*panel.Panel, error) {
			np, err := src.Load(ctx)
			if err != nil {
				return nil, err
			}
			return np, nil
		},
		ApplyTushareToken: func(token string) {
			if ts, ok := src.(*data.TushareSource); ok {
				ts.Token = token
				ts.Client = nil
			}
		},
		LoadUniverse: func() func(context.Context, string) (*panel.Panel, error) {
			ts, ok := src.(*data.TushareSource)
			if !ok {
				base := p
				return func(context.Context, string) (*panel.Panel, error) { return base, nil }
			}
			return func(ctx context.Context, index string) (*panel.Panel, error) {
				return ts.ForIndex(index).Load(ctx)
			}
		}(),
	})
	if err != nil {
		fatal("初始化服务失败", "err", err)
	}

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      15 * time.Minute, // 首次加载新股票池要拉成分与行情
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	// 优雅关闭
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		slog.Info("factor-lab 启动", "addr", "http://"+*addr, "data_source", src.Name(), "auth", *apiToken != "")
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatal("服务异常退出", "err", err)
		}
	}()
	<-ctx.Done()
	slog.Info("收到退出信号，正在关闭服务…")
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer shutCancel()
	if err := httpSrv.Shutdown(shutCtx); err != nil {
		slog.Error("关闭超时，强制退出", "err", err)
	}
	slog.Info("已退出")
}

func reportOf(src data.Source) *data.LoadReport {
	if r, ok := src.(data.Reporter); ok {
		return r.Report()
	}
	return nil
}

// fatal 记一条 error 日志后退出（统一走结构化日志，避免 FATAL 被打成 INFO）。
func fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}

// isLoopback 判断监听地址是否只对本机开放（"127.0.0.1:8080"、":8080" 视为对外）。
func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// splitList 把逗号分隔的配置项切成切片（忽略空项）。
func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
