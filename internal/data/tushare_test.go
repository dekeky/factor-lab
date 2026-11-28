package data

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dekeky/tushare-go-sdk"
)

type tsReq struct {
	APIName string         `json:"api_name"`
	Token   string         `json:"token"`
	Params  map[string]any `json:"params"`
	Fields  string         `json:"fields"`
}

func mockTushare(t *testing.T, handle func(api string, params map[string]any) (fields []string, items [][]any)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body tsReq
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("解析请求失败: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fields, items := handle(body.APIName, body.Params)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0, "msg": "",
			"data": map[string]any{"fields": fields, "items": items, "has_more": false},
		})
	}))
}

func testClient(t *testing.T, srv *httptest.Server) *tushare.Client {
	t.Helper()
	cli, err := tushare.NewClient("test-token",
		tushare.WithBaseURL(srv.URL),
		tushare.WithHTTPClient(srv.Client()),
		tushare.WithRetry(0, 0, 0),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}
	return cli
}

func defaultHandle(_ *testing.T, bars [][]any) func(string, map[string]any) ([]string, [][]any) {
	return func(api string, params map[string]any) ([]string, [][]any) {
		switch api {
		case "index_weight":
			return []string{"index_code", "con_code", "trade_date", "weight"}, [][]any{
				{"000300.SH", "000001.SZ", "20240131", 5.0},
				{"000300.SH", "600000.SH", "20240131", 3.0},
			}
		case "trade_cal":
			return []string{"exchange", "cal_date", "is_open"}, [][]any{
				{"SSE", "20240110", "1"},
				{"SSE", "20240115", "1"},
				{"SSE", "20240215", "1"},
			}
		case "daily":
			return []string{"ts_code", "trade_date", "open", "high", "low", "close", "vol", "amount"}, filterByDate(filterByCode(bars, params), params, 1)
		case "adj_factor":
			return []string{"ts_code", "trade_date", "adj_factor"}, filterByDate(filterByCode([][]any{
				{"000001.SZ", "20240110", 1.0},
				{"000001.SZ", "20240115", 2.0},
				{"000001.SZ", "20240215", 2.0},
				{"600000.SH", "20240110", 1.0},
				{"600000.SH", "20240115", 1.0},
				{"600000.SH", "20240215", 1.0},
			}, params), params, 1)
		case "daily_basic":
			return []string{
					"ts_code", "trade_date",
					"turnover_rate", "turnover_rate_f", "volume_ratio",
					"pe", "pe_ttm", "pb", "ps", "ps_ttm",
					"dv_ratio", "dv_ttm",
					"total_share", "float_share", "free_share",
					"total_mv", "circ_mv", "limit_status",
				}, filterByDate(filterByCode([][]any{
					{"000001.SZ", "20240110", 1.5, 1.2, 1.1, 12.0, 10.0, 1.2, 2.0, 1.8, 3.0, 2.5, 1000.0, 800.0, 600.0, 1e10, 8e9, 1.0},
					{"600000.SH", "20240110", 2.5, 2.0, 0.9, 9.0, 8.0, 0.9, 1.5, 1.4, 4.0, 3.5, 2000.0, 1500.0, 1200.0, 2e10, 1e10, 0.0},
				}, params), params, 1)
		case "fina_indicator_vip":
			period, _ := params["period"].(string)
			rows := [][]any{
				{"000001.SZ", "20240210", "20231231", 0.20},
				{"000001.SZ", "20240105", "20230930", 0.10},
			}
			if period != "" {
				var filtered [][]any
				for _, row := range rows {
					if compactDate(str(row[2])) == compactDate(period) {
						filtered = append(filtered, row)
					}
				}
				rows = filtered
			}
			return []string{"ts_code", "ann_date", "end_date", "roe"}, filterByCode(rows, params)
		case "stock_st":
			return []string{"ts_code", "name", "trade_date", "type"}, filterByDate(filterByCode([][]any{
				{"000001.SZ", "*ST某某", "20240115", "ST"},
			}, params), params, 2)
		case "stock_basic":
			return []string{"ts_code", "name"}, [][]any{
				{"000001.SZ", "平安银行"},
				{"600000.SH", "浦发银行"},
			}
		default:
			return nil, nil
		}
	}
}

func filterByDate(rows [][]any, params map[string]any, dateIdx int) [][]any {
	td := compactDate(str(params["trade_date"]))
	start := compactDate(str(params["start_date"]))
	end := compactDate(str(params["end_date"]))
	if td == "" && start == "" && end == "" {
		return rows
	}
	out := make([][]any, 0, len(rows))
	for _, row := range rows {
		if dateIdx >= len(row) {
			continue
		}
		d := compactDate(str(row[dateIdx]))
		if td != "" && d != td {
			continue
		}
		if start != "" && d < start {
			continue
		}
		if end != "" && d > end {
			continue
		}
		out = append(out, row)
	}
	return out
}

func filterByCode(rows [][]any, params map[string]any) [][]any {
	code, _ := params["ts_code"].(string)
	if code == "" {
		return rows
	}
	want := map[string]bool{}
	for _, c := range strings.Split(code, ",") {
		c = strings.TrimSpace(c)
		if c != "" {
			want[c] = true
		}
	}
	out := make([][]any, 0, len(rows))
	for _, row := range rows {
		if len(row) > 0 && want[str(row[0])] {
			out = append(out, row)
		}
	}
	return out
}

func sampleBars() [][]any {
	return [][]any{
		{"000001.SZ", "20240110", 10, 10.5, 9.8, 10, 1000, 10000},
		{"000001.SZ", "20240115", 11, 11.5, 10.8, 11, 1100, 12100},
		{"000001.SZ", "20240215", 12, 12.5, 11.8, 12, 1200, 14400},
		{"600000.SH", "20240110", 20, 20.5, 19.8, 20, 2000, 40000},
		{"600000.SH", "20240115", 21, 21.5, 20.8, 21, 2100, 44100},
		{"600000.SH", "20240215", 22, 22.5, 21.8, 22, 2200, 48400},
	}
}

func TestTushareLoadPITAndST(t *testing.T) {
	srv := mockTushare(t, defaultHandle(t, sampleBars()))
	defer srv.Close()

	src := &TushareSource{
		Client:    testClient(t, srv),
		IndexCode: "000300.SH",
		TopN:      10,
		Start:     "20240101",
		End:       "20240228",
		DataDir:   t.TempDir(),
	}
	p, err := src.Load(context.Background())
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if p.T() != 3 || p.N() != 2 {
		t.Fatalf("面板维度错误: %d×%d", p.T(), p.N())
	}

	dateIdx := map[string]int{}
	for i, d := range p.DateLabels() {
		dateIdx[d] = i
	}
	codeIdx := map[string]int{}
	for j, c := range p.Codes {
		codeIdx[c] = j
	}
	at := func(col string, date, code string) float64 {
		c, ok := p.Col(col)
		if !ok {
			t.Fatalf("缺少列 %s", col)
		}
		return c[dateIdx[date]*p.N()+codeIdx[code]]
	}

	// 后复权：000001.SZ 首日 adj=1，0215 adj=2 → 12×2=24（0115 为 ST，不写价格）
	if got := at("close", "2024-02-15", "000001.SZ"); math.Abs(got-24) > 1e-9 {
		t.Errorf("后复权价错误: 期望 24（12×2），实际 %v", got)
	}
	// 前复权：以最新 adj=2 为基准，0215 收盘 = 12，0110 收盘 = 10×1/2 = 5
	if got := at("qfq_close", "2024-02-15", "000001.SZ"); math.Abs(got-12) > 1e-9 {
		t.Errorf("前复权收盘错误: 期望 12，实际 %v", got)
	}
	if got := at("qfq_open", "2024-02-15", "000001.SZ"); math.Abs(got-12) > 1e-9 {
		t.Errorf("前复权开盘错误: 期望 12，实际 %v", got)
	}
	if got := at("qfq_close", "2024-01-10", "000001.SZ"); math.Abs(got-5) > 1e-9 {
		t.Errorf("前复权收盘错误: 0110 期望 5（10×1/2），实际 %v", got)
	}

	// PIT：0110 只能用 0105 公告的 0.10；0215 用 0210 公告的 0.20
	if got := at("roe", "2024-01-10", "000001.SZ"); math.Abs(got-0.10) > 1e-9 {
		t.Errorf("PIT 展开错误: 0110 期望 0.10，实际 %v", got)
	}
	if got := at("roe", "2024-02-15", "000001.SZ"); math.Abs(got-0.20) > 1e-9 {
		t.Errorf("PIT 展开错误: 0215 期望 0.20，实际 %v", got)
	}

	// ST 剔除：0115 的 000001.SZ 应整只剔除
	if got := at("close", "2024-01-15", "000001.SZ"); !math.IsNaN(got) {
		t.Errorf("ST 未剔除: 0115 close 期望 NaN，实际 %v", got)
	}
	if got := at("close", "2024-01-15", "600000.SH"); math.IsNaN(got) {
		t.Errorf("非 ST 股票被误删")
	}

	if got := at("turnover_rate", "2024-01-10", "000001.SZ"); math.Abs(got-1.5) > 1e-9 {
		t.Errorf("turnover_rate 错误: %v", got)
	}

	// pct_chg 由后复权价计算：600000.SH 0115 相对 0110 为 21/20-1=0.05
	if got := at("pct_chg", "2024-01-15", "600000.SH"); math.Abs(got-0.05) > 1e-9 {
		t.Errorf("pct_chg 错误: 期望 0.05，实际 %v", got)
	}

	if src.Report() == nil || !src.Report().HasROE {
		t.Errorf("数据质量摘要缺失或未识别 ROE")
	}
	if src.Report().STDropped != 1 {
		t.Errorf("ST 剔除计数错误: %d", src.Report().STDropped)
	}
}

func TestLoadReadsDailyFromParquetNotByCode(t *testing.T) {
	var mu sync.Mutex
	var dailyCalls []map[string]any
	srv := mockTushare(t, func(api string, params map[string]any) ([]string, [][]any) {
		if api == "daily" {
			mu.Lock()
			cp := make(map[string]any, len(params))
			for k, v := range params {
				cp[k] = v
			}
			dailyCalls = append(dailyCalls, cp)
			mu.Unlock()
		}
		return defaultHandle(t, sampleBars())(api, params)
	})
	defer srv.Close()

	src := &TushareSource{
		Client:    testClient(t, srv),
		IndexCode: "000300.SH",
		Start:     "20240101",
		End:       "20240228",
		DataDir:   t.TempDir(),
	}
	p, err := src.Load(context.Background())
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	mu.Lock()
	calls := append([]map[string]any(nil), dailyCalls...)
	mu.Unlock()
	if len(calls) == 0 {
		t.Fatal("应至少请求一次 daily 以写入 parquet")
	}
	for i, c := range calls {
		if code, _ := c["ts_code"].(string); code != "" {
			t.Errorf("daily[%d] 不应按股票拉取，ts_code=%q params=%v", i, code, c)
		}
		_, hasRange := c["start_date"]
		_, hasDay := c["trade_date"]
		if !hasRange && !hasDay {
			t.Errorf("daily[%d] 应按交易日拉取: %v", i, c)
		}
	}
	closeC, ok := p.Col("close")
	if !ok {
		t.Fatal("缺少 close")
	}
	// 000001.SZ 2024-01-10 adj=1 → 10
	dateIdx, codeIdx := map[string]int{}, map[string]int{}
	for i, d := range p.DateLabels() {
		dateIdx[d] = i
	}
	for j, c := range p.Codes {
		codeIdx[c] = j
	}
	got := closeC[dateIdx["2024-01-10"]*p.N()+codeIdx["000001.SZ"]]
	if math.Abs(got-10) > 1e-9 {
		t.Errorf("parquet 读入后 close 应为 10，实际 %v", got)
	}
	tr, ok := p.Col("turnover_rate")
	if !ok {
		t.Fatal("缺少 turnover_rate")
	}
	if got := tr[dateIdx["2024-01-10"]*p.N()+codeIdx["000001.SZ"]]; math.Abs(got-1.5) > 1e-9 {
		t.Errorf("turnover_rate 应为 1.5，实际 %v", got)
	}
}

func TestTushareWritesDailyBasicColumns(t *testing.T) {
	srv := mockTushare(t, defaultHandle(t, sampleBars()))
	defer srv.Close()

	src := &TushareSource{
		Client:    testClient(t, srv),
		IndexCode: "000300.SH",
		Start:     "20240101",
		End:       "20240228",
		DataDir:   t.TempDir(),
	}
	p, err := src.Load(context.Background())
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}

	dateIdx := map[string]int{}
	for i, d := range p.DateLabels() {
		dateIdx[d] = i
	}
	codeIdx := map[string]int{}
	for j, c := range p.Codes {
		codeIdx[c] = j
	}
	at := func(col string) float64 {
		c, ok := p.Col(col)
		if !ok {
			t.Fatalf("缺少列 %s", col)
		}
		return c[dateIdx["2024-01-10"]*p.N()+codeIdx["000001.SZ"]]
	}

	want := map[string]float64{
		"turnover_rate":   1.5,
		"turnover_rate_f": 1.2,
		"volume_ratio":    1.1,
		"pe":              12.0,
		"pe_ttm":          10.0,
		"pb":              1.2,
		"ps":              2.0,
		"ps_ttm":          1.8,
		"dv_ratio":        3.0,
		"dv_ttm":          2.5,
		"total_share":     1000.0,
		"float_share":     800.0,
		"free_share":      600.0,
		"total_mv":        1e10,
		"circ_mv":         8e9,
		"limit_status":    1.0,
	}
	for name, v := range want {
		if got := at(name); math.Abs(got-v) > 1e-9 {
			t.Errorf("%s: 期望 %v，实际 %v", name, v, got)
		}
	}
	if _, ok := p.Col("close"); !ok {
		t.Fatal("close 列不应被 daily_basic 挤掉")
	}
	if got := at("close"); math.Abs(got-10) > 1e-9 {
		t.Errorf("close 应保持后复权日 K（0110 adj=1 → 10），实际 %v", got)
	}
}

func TestTushareLoadSuspendAndLimit(t *testing.T) {
	bars := sampleBars()
	srv := mockTushare(t, func(api string, params map[string]any) ([]string, [][]any) {
		switch api {
		case "suspend_d":
			return []string{"ts_code", "trade_date", "suspend_timing", "suspend_type"}, filterByDate([][]any{
				{"600000.SH", "20240215", "", "S"},
			}, params, 1)
		case "stk_limit":
			return []string{"ts_code", "trade_date", "pre_close", "up_limit", "down_limit"}, filterByDate([][]any{
				{"000001.SZ", "20240215", 11, 12, 10}, // 收盘 12 触及涨停
				{"600000.SH", "20240215", 21, 24.2, 19.8},
			}, params, 1)
		default:
			return defaultHandle(t, bars)(api, params)
		}
	})
	defer srv.Close()

	src := &TushareSource{
		Client: testClient(t, srv), IndexCode: "000300.SH",
		Start: "20240101", End: "20240228", DataDir: t.TempDir(),
	}
	p, err := src.Load(context.Background())
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	dateIdx := map[string]int{}
	for i, d := range p.DateLabels() {
		dateIdx[d] = i
	}
	codeIdx := map[string]int{}
	for j, c := range p.Codes {
		codeIdx[c] = j
	}
	at := func(col, date, code string) float64 {
		c, ok := p.Col(col)
		if !ok {
			t.Fatalf("缺少列 %s", col)
		}
		return c[dateIdx[date]*p.N()+codeIdx[code]]
	}

	if got := at("raw_close", "2024-02-15", "000001.SZ"); math.Abs(got-12) > 1e-9 {
		t.Errorf("未复权收盘应为 12，实际 %v", got)
	}
	if got := at("suspend", "2024-02-15", "600000.SH"); got != 1 {
		t.Errorf("停牌标记应为 1，实际 %v", got)
	}
	if got := at("up_limit", "2024-02-15", "000001.SZ"); math.Abs(got-12) > 1e-9 {
		t.Errorf("涨停价应为 12，实际 %v", got)
	}
	j := codeIdx["000001.SZ"]
	if j >= len(p.Names) || p.Names[j] != "平安银行" {
		t.Errorf("股票名称应为 平安银行，实际 %v", p.Names)
	}
	if p.Label(j) != "平安银行（000001.SZ）" {
		t.Errorf("展示名错误: %q", p.Label(j))
	}
}

func TestTushareRejectsMissingRequiredFields(t *testing.T) {
	rows := [][]any{
		{"000001.SZ", "20240110", 10, 10.5, 9.8, nil, 1000, 10000},
	}
	srv := mockTushare(t, defaultHandle(t, rows))
	defer srv.Close()

	src := &TushareSource{Client: testClient(t, srv), IndexCode: "000300.SH", DataDir: t.TempDir()}
	_, err := src.Load(context.Background())
	if err == nil {
		t.Fatal("缺少 close 时应当报错")
	}
	if !contains(err.Error(), "close") {
		t.Errorf("错误信息应指出缺失字段 close，实际: %v", err)
	}
}

func TestTushareReportsEmptyUniverse(t *testing.T) {
	srv := mockTushare(t, func(api string, _ map[string]any) ([]string, [][]any) {
		if api == "index_weight" {
			return []string{"index_code", "con_code", "trade_date", "weight"}, nil
		}
		return nil, nil
	})
	defer srv.Close()

	src := &TushareSource{Client: testClient(t, srv)}
	if _, err := src.Load(context.Background()); err == nil {
		t.Fatal("空成分应报错")
	}
}

func TestTushareFallsBackToCSI300SZ(t *testing.T) {
	var seen []string
	srv := mockTushare(t, func(api string, params map[string]any) ([]string, [][]any) {
		if api == "index_weight" {
			code, _ := params["index_code"].(string)
			seen = append(seen, code)
			if code == "399300.SZ" {
				return []string{"index_code", "con_code", "trade_date", "weight"}, [][]any{
					{"399300.SZ", "000001.SZ", "20240131", 5.0},
					{"399300.SZ", "600000.SH", "20240131", 3.0},
				}
			}
			return []string{"index_code", "con_code", "trade_date", "weight"}, nil
		}
		return defaultHandle(t, sampleBars())(api, params)
	})
	defer srv.Close()

	src := &TushareSource{Client: testClient(t, srv), IndexCode: "000300.SH", Start: "20240101", End: "20240228", DataDir: t.TempDir()}
	p, err := src.Load(context.Background())
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if p.N() != 2 {
		t.Fatalf("回退后成分数错误: %d", p.N())
	}
	if len(seen) < 2 || seen[0] != "000300.SH" || seen[1] != "399300.SZ" {
		t.Errorf("应先查 000300.SH 再回退 399300.SZ，实际 %v", seen)
	}
}

func TestSplitDateRange(t *testing.T) {
	got := splitDateRange("20200101", "20211231", 6)
	want := [][2]string{
		{"20200101", "20200630"},
		{"20200701", "20201231"},
		{"20210101", "20210630"},
		{"20210701", "20211231"},
	}
	if len(got) != len(want) {
		t.Fatalf("窗口数 %d，期望 %d：%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("窗口 %d: 得到 %v，期望 %v", i, got[i], want[i])
		}
	}
}

func TestLoadCachesAuxByMarketNotByCode(t *testing.T) {
	type call struct {
		api                                        string
		code, start, end, period, day, suspendType string
	}
	var mu sync.Mutex
	var calls []call
	srv := mockTushare(t, func(api string, params map[string]any) ([]string, [][]any) {
		switch api {
		case "adj_factor", "fina_indicator_vip", "fina_indicator", "stock_st", "suspend_d", "stk_limit":
			c := call{
				api: api, code: str(params["ts_code"]),
				start: str(params["start_date"]), end: str(params["end_date"]),
				period: str(params["period"]), day: str(params["trade_date"]),
				suspendType: str(params["suspend_type"]),
			}
			mu.Lock()
			calls = append(calls, c)
			mu.Unlock()
		}
		return defaultHandle(t, sampleBars())(api, params)
	})
	defer srv.Close()

	src := &TushareSource{
		Client: testClient(t, srv), IndexCode: "000300.SH",
		Start: "20240101", End: "20240228", DataDir: t.TempDir(),
	}
	if _, err := src.Load(context.Background()); err != nil {
		t.Fatalf("Load 失败: %v", err)
	}

	mu.Lock()
	first := append([]call(nil), calls...)
	calls = nil
	mu.Unlock()

	var adj, fina, st, suspend, limit, oldFina int
	for _, c := range first {
		if c.code != "" {
			t.Errorf("%s 不应按股票拉取，ts_code=%q", c.api, c.code)
		}
		switch c.api {
		case "adj_factor":
			adj++
			if c.day == "" {
				t.Errorf("adj_factor 应按日拉取，得到 %+v", c)
			}
		case "fina_indicator_vip":
			fina++
			if c.period == "" {
				t.Errorf("fina_indicator_vip 应按 period 拉取，得到 %+v", c)
			}
		case "fina_indicator":
			oldFina++
		case "stock_st":
			st++
			if c.day == "" {
				t.Errorf("stock_st 应按日拉取，得到 %+v", c)
			}
		case "suspend_d":
			suspend++
			if c.day == "" || c.suspendType != "S" {
				t.Errorf("suspend_d 应按日且 suspend_type=S，得到 %+v", c)
			}
		case "stk_limit":
			limit++
			if c.day == "" {
				t.Errorf("stk_limit 应按日拉取，得到 %+v", c)
			}
		}
	}
	if adj == 0 || fina == 0 || st == 0 || suspend == 0 || limit == 0 {
		t.Fatalf("首次应回填 adj=%d fina_vip=%d st=%d suspend=%d limit=%d", adj, fina, st, suspend, limit)
	}
	if oldFina != 0 {
		t.Errorf("不应再打 fina_indicator，打了 %d 次", oldFina)
	}

	if _, err := src.Load(context.Background()); err != nil {
		t.Fatalf("第二次 Load 失败: %v", err)
	}
	mu.Lock()
	second := append([]call(nil), calls...)
	mu.Unlock()
	for _, c := range second {
		switch c.api {
		case "adj_factor", "stock_st", "suspend_d", "stk_limit":
			t.Errorf("增量同步不应再拉 %s: %+v", c.api, c)
		case "fina_indicator_vip":
			// 最新一期季报每次刷新
		default:
			t.Errorf("增量同步意外请求 %s: %+v", c.api, c)
		}
	}
}

func TestLoadFetchesIndependentAPIsInParallel(t *testing.T) {
	const auxSleep = 40 * time.Millisecond
	aux := map[string]bool{
		"adj_factor": true, "fina_indicator_vip": true, "stock_st": true,
		"suspend_d": true, "stk_limit": true,
	}
	var mu sync.Mutex
	inflight, max := 0, 0
	srv := mockTushare(t, func(api string, params map[string]any) ([]string, [][]any) {
		if aux[api] {
			mu.Lock()
			inflight++
			if inflight > max {
				max = inflight
			}
			mu.Unlock()
			time.Sleep(auxSleep)
			mu.Lock()
			inflight--
			mu.Unlock()
		}
		return defaultHandle(t, sampleBars())(api, params)
	})
	defer srv.Close()

	src := &TushareSource{
		Client: testClient(t, srv), IndexCode: "000300.SH",
		Start: "20240101", End: "20240228", DataDir: t.TempDir(),
	}
	if _, err := src.Load(context.Background()); err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if max < 2 {
		t.Fatalf("独立接口应并行拉取，同时在飞最多 %d", max)
	}
}

func TestPanelRangeFiveYears(t *testing.T) {
	from, to := panelRange("", "20261127")
	if from != "20211127" || to != "20261127" {
		t.Fatalf("got %s %s", from, to)
	}
	from, to = panelRange("20240101", "20240228")
	if from != "20240101" || to != "20240228" {
		t.Fatalf("explicit Start should win when inside window: %s %s", from, to)
	}
	from, to = panelRange("20100101", "20240228")
	if from != "20190228" || to != "20240228" {
		t.Fatalf("old Start should clip to 5y: %s %s", from, to)
	}
}

func TestLoadAllMarketIncludesNonIndexStocks(t *testing.T) {
	bars := append(sampleBars(), []any{"000002.SZ", "20240110", 8, 8.5, 7.8, 8, 800, 6400})
	srv := mockTushare(t, func(api string, params map[string]any) ([]string, [][]any) {
		if api == "stock_basic" {
			return []string{"ts_code", "name"}, [][]any{
				{"000001.SZ", "平安银行"},
				{"600000.SH", "浦发银行"},
				{"000002.SZ", "万科A"},
			}
		}
		return defaultHandle(t, bars)(api, params)
	})
	defer srv.Close()

	src := &TushareSource{
		Client: testClient(t, srv), IndexCode: AllMarket,
		Start: "20240101", End: "20240228", DataDir: t.TempDir(),
	}
	p, err := src.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, c := range p.Codes {
		got[c] = true
	}
	if !got["000002.SZ"] || p.N() != 3 {
		t.Fatalf("全市场应含指数外股票，得到 %v", p.Codes)
	}
}

func TestLoadClipsOlderThanFiveYears(t *testing.T) {
	bars := append(sampleBars(), []any{"000001.SZ", "20180110", 5, 5.5, 4.8, 5, 500, 2500})
	srv := mockTushare(t, func(api string, params map[string]any) ([]string, [][]any) {
		if api == "trade_cal" {
			return []string{"exchange", "cal_date", "is_open"}, [][]any{
				{"SSE", "20180110", "1"},
				{"SSE", "20240110", "1"},
				{"SSE", "20240115", "1"},
				{"SSE", "20240215", "1"},
			}
		}
		return defaultHandle(t, bars)(api, params)
	})
	defer srv.Close()

	src := &TushareSource{
		Client: testClient(t, srv), IndexCode: "000300.SH",
		End: "20240228", DataDir: t.TempDir(),
	}
	p, err := src.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range p.DateLabels() {
		if d == "2018-01-10" {
			t.Fatal("内存面板不应含 5 年前的交易日")
		}
	}
	if p.T() != 3 {
		t.Fatalf("应只保留窗口内 3 天，得到 %d %v", p.T(), p.DateLabels())
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
