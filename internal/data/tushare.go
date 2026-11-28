package data

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/dekeky/tushare-go-sdk"

	"factor-lab/internal/panel"
)

// 必需字段：缺失即视为数据源不可用（生产环境不允许带病启动）
var requiredFields = []string{"open", "high", "low", "close", "vol", "amount"}

// DailyBasicCols 写入面板的每日指标。不含 ts_code/trade_date（键）和 close（与后复权 close 冲突）。
var DailyBasicCols = []string{
	"turnover_rate", "turnover_rate_f", "volume_ratio",
	"pe", "pe_ttm", "pb", "ps", "ps_ttm",
	"dv_ratio", "dv_ttm",
	"total_share", "float_share", "free_share",
	"total_mv", "circ_mv",
	"limit_status",
}

const (
	defaultIndex       = "000300.SH"
	csi300SZ           = "399300.SZ"
	defaultQPS         = 4.0
	panelLookbackYears = 5
)

// panelRange 内存面板日期窗：默认最近 5 年，右端为 End（空则今天）。Start 更晚则从其起。
func panelRange(start, end string) (from, to string) {
	to = endOrToday(end)
	lookback := to
	if t, err := time.Parse("20060102", to); err == nil {
		lookback = t.AddDate(-panelLookbackYears, 0, 0).Format("20060102")
	}
	from = compactDate(start)
	if from == "" || from < lookback {
		from = lookback
	}
	return from, to
}

func allMarketIndex(code string) bool {
	c := CanonicalIndex(code)
	return c == "" || c == AllMarket
}

// TushareSource 用 tushare-go-sdk 从 Tushare Pro 加载 A 股面板。
//
// 数据流（Tushare 按接口限流，无依赖的请求并行）：
//  1. index_weight（仅指数切片）∥ trade_cal
//  2. daily+daily_basic ∥ adj_factor ∥ fina_indicator_vip ∥ stock_st ∥ suspend_d ∥ stk_limit ∥ stock_basic
//     各接口全市场写入自己的 parquet；内存面板默认最近 5 年全市场，评价时再按指数切。
type TushareSource struct {
	Token     string
	IndexCode string // 空 / ALL = 全市场；否则按该指数最新成分切片
	TopN      int    // 成分取前 N 只（0 = 全部成分）
	Start     string // YYYYMMDD
	End       string // YYYYMMDD

	Concurrency int           // 分股票并发数，默认 4
	QPS         float64       // 客户端全局限流（已弃用，保留兼容）
	RPM         float64       // 每个 Tushare 接口每分钟请求上限，默认 500
	Timeout     time.Duration // 单次请求超时，默认 60s
	DataDir     string        // parquet 根目录（daily 分片在 DataDir/parquet/daily）

	Client *tushare.Client // 可注入（测试用）
	// LastReport 是最近一次 Load 的数据质量摘要
	LastReport *LoadReport
}

func (s *TushareSource) Name() string {
	idx := CanonicalIndex(s.IndexCode)
	if idx == "" || idx == AllMarket {
		return "tushare:all"
	}
	return "tushare:" + idx
}

// ForIndex 复制一份数据源配置，只换指数代码。共享 Client，以便限流对所有股票池生效。
func (s *TushareSource) ForIndex(index string) *TushareSource {
	if s == nil {
		return &TushareSource{IndexCode: CanonicalIndex(index)}
	}
	c := *s
	c.IndexCode = CanonicalIndex(index)
	c.LastReport = nil
	if c.IndexCode == "" {
		c.IndexCode = defaultIndex
	}
	return &c
}

// Report 实现 Reporter：返回最近一次加载的数据质量摘要。
func (s *TushareSource) Report() *LoadReport { return s.LastReport }

func parallel(fns ...func()) {
	var wg sync.WaitGroup
	wg.Add(len(fns))
	for _, fn := range fns {
		go func(fn func()) {
			defer wg.Done()
			fn()
		}(fn)
	}
	wg.Wait()
}

func (s *TushareSource) client() (*tushare.Client, error) {
	if s.Concurrency <= 0 {
		s.Concurrency = 4
	}
	if s.QPS <= 0 {
		s.QPS = defaultQPS
	}
	if s.RPM <= 0 {
		s.RPM = defaultAPIRPM
	}
	if s.Timeout <= 0 {
		s.Timeout = 60 * time.Second
	}
	if s.Client != nil {
		return s.Client, nil
	}
	cli, err := tushare.NewClient(s.Token,
		tushare.WithTimeout(s.Timeout),
		tushare.WithAPIRateLimit(s.RPM),
		tushare.WithRetry(3, 500*time.Millisecond, 8*time.Second),
		tushare.WithLogger(slog.Default()),
	)
	if err != nil {
		return nil, err
	}
	s.Client = cli
	return cli, nil
}

func (s *TushareSource) Load(ctx context.Context) (*panel.Panel, error) {
	cli, err := s.client()
	if err != nil {
		return nil, fmt.Errorf("创建 Tushare 客户端失败（请设置 TUSHARE_TOKEN 或 -tushare-token）: %w", err)
	}
	if s.DataDir == "" {
		return nil, fmt.Errorf("未配置 DataDir，无法写入 parquet")
	}
	index := CanonicalIndex(s.IndexCode)
	if allMarketIndex(index) {
		index = AllMarket
	}

	from, to := panelRange(s.Start, s.End)
	var stocks []stock
	var cal []string
	var weightErr, calErr error
	parallel(
		func() {
			if allMarketIndex(s.IndexCode) {
				return
			}
			stocks, weightErr = s.loadWeights(ctx, cli, index)
		},
		func() { cal, calErr = loadOpenDates(ctx, cli, dailySyncStart, calendarHorizon(s.End)) },
	)
	if weightErr != nil {
		return nil, weightErr
	}
	if calErr != nil {
		return nil, fmt.Errorf("取交易日历失败: %w", calErr)
	}
	if err := writeOpenDates(s.DataDir, cal); err != nil {
		return nil, fmt.Errorf("写入交易日历失败: %w", err)
	}
	cal = clipOpenDates(cal, endOrToday(s.End))
	syncCal := clipCal(cal, from)
	var codes []string
	if !allMarketIndex(s.IndexCode) {
		if s.TopN > 0 && len(stocks) > s.TopN {
			stocks = stocks[:s.TopN]
		}
		codes = make([]string, len(stocks))
		for i, st := range stocks {
			codes[i] = st.code
		}
	}

	var dailyErr, adjErr, factorErr, roeErr, stErr, suspendErr, limitErr, nameErr error
	var nameMap map[string]string
	parallel(
		func() { dailyErr = s.syncDailyMissing(ctx, cli, syncCal) },
		func() { adjErr = s.syncAdjFactorParquet(ctx, cli, syncCal) },
		func() { roeErr = s.syncFinaParquet(ctx, cli) },
		func() { stErr = s.syncStockSTParquet(ctx, cli, syncCal) },
		func() { suspendErr = s.syncSuspendParquet(ctx, cli, syncCal) },
		func() { limitErr = s.syncLimitParquet(ctx, cli, syncCal) },
		func() { nameMap, nameErr = s.queryNames(ctx, cli, codes) },
	)
	if dailyErr != nil {
		return nil, fmt.Errorf("同步日K parquet 失败: %w", dailyErr)
	}
	if adjErr != nil {
		return nil, fmt.Errorf("同步复权因子失败: %w", adjErr)
	}

	slog.Info("开始读日K parquet 切片", "from", from, "to", to, "codes", len(codes))
	readT0 := time.Now()
	pqRows, err := readDailyParquet(dailyParquetDir(s.DataDir), from, to, codes)
	if err != nil {
		return nil, fmt.Errorf("读日K parquet 失败: %w", err)
	}
	slog.Info("日K parquet 读取完成", "rows", len(pqRows), "elapsed", time.Since(readT0).Round(time.Millisecond).String())
	barRows, factorRows := parquetToPanelRows(pqRows)

	adjRows, err := s.readAdjMaps(from, to, codes)
	if err != nil {
		return nil, fmt.Errorf("读复权因子 parquet 失败: %w", err)
	}
	roeRows, readErr := s.readFinaMaps(finaStart(from), to, codes)
	if readErr != nil && roeErr == nil {
		roeErr = readErr
	}
	stRows, readErr := s.readSTMaps(from, to, codes)
	if readErr != nil && stErr == nil {
		stErr = readErr
	}
	suspendRows, readErr := s.readSuspendMaps(from, to, codes)
	if readErr != nil && suspendErr == nil {
		suspendErr = readErr
	}
	limitRows, readErr := s.readLimitMaps(from, to, codes)
	if readErr != nil && limitErr == nil {
		limitErr = readErr
	}

	p, report, err := assemblePanel(rawBundle{
		Index:       index,
		BarRows:     barRows,
		AdjRows:     adjRows,
		FactorRows:  factorRows,
		ROERows:     roeRows,
		STRows:      stRows,
		SuspendRows: suspendRows,
		LimitRows:   limitRows,
		FactorErr:   factorErr,
		ROEErr:      roeErr,
		STErr:       stErr,
		SuspendErr:  suspendErr,
		LimitErr:    limitErr,
		NameErr:     nameErr,
	})
	if err != nil {
		return nil, err
	}
	p.SetNames(alignNames(p.Codes, nameMap))
	s.LastReport = report
	return p, nil
}

type stock struct {
	code   string
	weight float64
}

func (s *TushareSource) loadWeights(ctx context.Context, cli *tushare.Client, index string) ([]stock, error) {
	start, end := s.weightWindow()
	tbl, err := s.queryIndexWeight(ctx, cli, index, start, end)
	if err != nil {
		return nil, fmt.Errorf("取指数成分失败: %w", err)
	}
	if tbl.IsEmpty() && index == defaultIndex {
		tbl, err = s.queryIndexWeight(ctx, cli, csi300SZ, start, end)
		if err != nil {
			return nil, fmt.Errorf("取指数成分失败: %w", err)
		}
		if !tbl.IsEmpty() {
			index = csi300SZ
			s.IndexCode = csi300SZ
		}
	}
	if tbl.IsEmpty() {
		// 再往前扩一年，兼容月末权重尚未更新的情况
		t, perr := time.Parse("20060102", end)
		if perr == nil {
			wider := t.AddDate(-1, 0, 0).Format("20060102")
			tbl, err = s.queryIndexWeight(ctx, cli, index, wider, end)
			if err != nil {
				return nil, fmt.Errorf("取指数成分失败: %w", err)
			}
		}
	}
	stocks := latestWeights(tbl)
	if len(stocks) == 0 {
		return nil, fmt.Errorf("指数 %s 没有成分数据（请确认积分权限以及指数代码）", index)
	}
	return stocks, nil
}

func (s *TushareSource) queryIndexWeight(ctx context.Context, cli *tushare.Client, index, start, end string) (*tushare.Table, error) {
	return cli.IndexWeight(ctx, &tushare.IndexWeightParams{
		IndexCode: index,
		StartDate: start,
		EndDate:   end,
	})
}

func (s *TushareSource) weightWindow() (start, end string) {
	end = s.End
	if end == "" {
		end = time.Now().Format("20060102")
	}
	t, err := time.Parse("20060102", end)
	if err != nil {
		return s.Start, end
	}
	return t.AddDate(0, -3, 0).Format("20060102"), end
}

func latestWeights(tbl *tushare.Table) []stock {
	if tbl == nil || tbl.IsEmpty() {
		return nil
	}
	latest := ""
	for i := 0; i < tbl.Len(); i++ {
		d := tbl.Row(i).Str("trade_date")
		if d > latest {
			latest = d
		}
	}
	var stocks []stock
	seen := map[string]bool{}
	for i := 0; i < tbl.Len(); i++ {
		row := tbl.Row(i)
		if row.Str("trade_date") != latest {
			continue
		}
		code := row.Str("con_code")
		if code == "" {
			code = row.Str("ts_code")
		}
		if code == "" || seen[code] {
			continue
		}
		seen[code] = true
		stocks = append(stocks, stock{code: code, weight: row.Float("weight")})
	}
	sort.Slice(stocks, func(a, b int) bool { return stocks[a].weight > stocks[b].weight })
	return stocks
}

func (s *TushareSource) queryNames(ctx context.Context, cli *tushare.Client, codes []string) (map[string]string, error) {
	want := map[string]bool{}
	for _, c := range codes {
		want[c] = true
	}
	rows, err := queryAPIPaged(ctx, cli, "stock_basic", tushare.Params{"list_status": "L"},
		[]string{"ts_code", "name"}, 5000)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, r := range rows {
		code := rowCode(r)
		if len(want) > 0 && !want[code] {
			continue
		}
		if n := str(r["name"]); n != "" {
			out[code] = n
		}
	}
	return out, nil
}

func alignNames(codes []string, m map[string]string) []string {
	out := make([]string, len(codes))
	for i, c := range codes {
		out[i] = m[c]
	}
	return out
}

func queryAPI(ctx context.Context, cli *tushare.Client, api string, params tushare.Params, fields []string) ([]map[string]any, error) {
	return queryAPIPaged(ctx, cli, api, params, fields, 5000)
}

func queryAPIPaged(ctx context.Context, cli *tushare.Client, api string, params tushare.Params, fields []string, pageSize int) ([]map[string]any, error) {
	if pageSize <= 0 {
		pageSize = 5000
	}
	var out []map[string]any
	opts := []tushare.CallOption{tushare.WithLimit(pageSize)}
	if len(fields) > 0 {
		opts = append(opts, tushare.WithFields(fields...))
	}
	err := cli.Paginate(ctx, api, params, func(page *tushare.Table) error {
		out = append(out, page.Records()...)
		return nil
	}, opts...)
	return out, err
}

// splitDateRange 把 [start, end] 切成最多 months 个月一段（闭区间，YYYYMMDD）。
func splitDateRange(start, end string, months int) [][2]string {
	t0, err0 := time.Parse("20060102", start)
	t1, err1 := time.Parse("20060102", end)
	if err0 != nil || err1 != nil {
		return [][2]string{{start, end}}
	}
	if t1.Before(t0) {
		t0, t1 = t1, t0
	}
	if months <= 0 {
		months = 6
	}
	var out [][2]string
	cur := t0
	for !cur.After(t1) {
		next := cur.AddDate(0, months, 0).AddDate(0, 0, -1)
		if next.After(t1) {
			next = t1
		}
		out = append(out, [2]string{cur.Format("20060102"), next.Format("20060102")})
		cur = next.AddDate(0, 0, 1)
	}
	if len(out) == 0 {
		return [][2]string{{start, end}}
	}
	return out
}

func finaStart(start string) string {
	if start == "" {
		return ""
	}
	t, err := time.Parse("20060102", start)
	if err != nil {
		return start
	}
	return t.AddDate(-1, 0, 0).Format("20060102")
}

type rawBundle struct {
	Index       string
	BarRows     []map[string]any
	AdjRows     []map[string]any
	FactorRows  []map[string]any
	ROERows     []map[string]any
	STRows      []map[string]any
	SuspendRows []map[string]any
	LimitRows   []map[string]any
	FactorErr   error
	ROEErr      error
	STErr       error
	SuspendErr  error
	LimitErr    error
	NameErr     error
}

func assemblePanel(b rawBundle) (*panel.Panel, *LoadReport, error) {
	type key struct {
		d    string
		code string
	}
	dateSet := map[string]bool{}
	bars := map[key]map[string]float64{}
	for _, r := range b.BarRows {
		code := rowCode(r)
		d := normDate(str(r["trade_date"]))
		if code == "" || d == "" {
			continue
		}
		rec := map[string]float64{}
		for _, f := range []string{"open", "high", "low", "close", "vol", "amount", "pre_close"} {
			if v, ok := toFloat(r[f]); ok {
				rec[f] = v
			}
		}
		bars[key{d, code}] = rec
		dateSet[d] = true
	}
	if len(bars) == 0 {
		return nil, nil, fmt.Errorf("日K数据为空（指数 %s）", b.Index)
	}

	adjs := map[key]float64{}
	for _, r := range b.AdjRows {
		code := rowCode(r)
		d := normDate(str(r["trade_date"]))
		if v, ok := toFloat(r["adj_factor"]); ok && code != "" && d != "" {
			adjs[key{d, code}] = v
		}
	}

	extra := map[key]map[string]float64{}
	for _, r := range b.FactorRows {
		code := rowCode(r)
		d := normDate(str(r["trade_date"]))
		if code == "" || d == "" {
			continue
		}
		rec := map[string]float64{}
		for _, f := range DailyBasicCols {
			if v, ok := toFloat(r[f]); ok {
				rec[f] = v
			}
		}
		extra[key{d, code}] = rec
	}

	stSet := map[key]bool{}
	for _, r := range b.STRows {
		code := rowCode(r)
		d := normDate(str(r["trade_date"]))
		if code != "" && d != "" {
			stSet[key{d, code}] = true
		}
	}
	suspendSet := map[key]bool{}
	for _, r := range b.SuspendRows {
		code := rowCode(r)
		d := normDate(str(r["trade_date"]))
		if code != "" && d != "" {
			suspendSet[key{d, code}] = true
		}
	}
	type limit struct{ up, down float64 }
	limits := map[key]limit{}
	for _, r := range b.LimitRows {
		code := rowCode(r)
		d := normDate(str(r["trade_date"]))
		up, okU := toFloat(r["up_limit"])
		dn, okD := toFloat(r["down_limit"])
		if code == "" || d == "" || !okU || !okD {
			continue
		}
		limits[key{d, code}] = limit{up, dn}
	}

	type pit struct {
		ann string
		val float64
	}
	pitByCode := map[string][]pit{}
	for _, r := range b.ROERows {
		code := rowCode(r)
		ann := normDate(str(r["ann_date"]))
		v, ok := toFloat(r["roe"])
		if code == "" || ann == "" || !ok {
			continue
		}
		pitByCode[code] = append(pitByCode[code], pit{ann, v})
	}
	for c := range pitByCode {
		list := pitByCode[c]
		sort.Slice(list, func(i, j int) bool { return list[i].ann < list[j].ann })
		dedup := list[:0]
		for _, p := range list {
			if n := len(dedup); n > 0 && dedup[n-1].ann == p.ann {
				dedup[n-1].val = p.val
				continue
			}
			dedup = append(dedup, p)
		}
		pitByCode[c] = dedup
	}

	dates := make([]string, 0, len(dateSet))
	for d := range dateSet {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	times := make([]time.Time, len(dates))
	for i, d := range dates {
		t, err := time.Parse("2006-01-02", d)
		if err != nil {
			return nil, nil, fmt.Errorf("日期解析失败 %q: %w", d, err)
		}
		times[i] = t
	}

	codeSet := map[string]bool{}
	for k := range bars {
		codeSet[k.code] = true
	}
	sortedCodes := make([]string, 0, len(codeSet))
	for c := range codeSet {
		sortedCodes = append(sortedCodes, c)
	}
	sort.Strings(sortedCodes)

	T, N := len(dates), len(sortedCodes)
	p := panel.New(times, sortedCodes)
	dateIdx := make(map[string]int, T)
	for i, d := range dates {
		dateIdx[d] = i
	}
	codeIdx := make(map[string]int, N)
	for j, c := range sortedCodes {
		codeIdx[c] = j
	}

	names := []string{"open", "high", "low", "close", "vol", "amount", "roe", "raw_close", "raw_open", "qfq_open", "qfq_close", "suspend", "up_limit", "down_limit"}
	names = append(names, DailyBasicCols...)
	cols := make(map[string][]float64, len(names))
	for _, n := range names {
		col := make([]float64, T*N)
		for i := range col {
			col[i] = math.NaN()
		}
		cols[n] = col
	}

	adjBase := map[string]float64{}
	adjLast := map[string]float64{}
	for _, c := range sortedCodes {
		for _, d := range dates {
			if a, ok := adjs[key{d, c}]; ok && a != 0 {
				adjBase[c] = a
				break
			}
		}
		for i := len(dates) - 1; i >= 0; i-- {
			if a, ok := adjs[key{dates[i], c}]; ok && a != 0 {
				adjLast[c] = a
				break
			}
		}
	}

	stDropped := 0
	for k, rec := range bars {
		i, ok1 := dateIdx[k.d]
		j, ok2 := codeIdx[k.code]
		if !ok1 || !ok2 {
			continue
		}
		if stSet[k] {
			stDropped++
			continue
		}
		idx := i*N + j
		ab, hasBase := adjBase[k.code]
		a, hasAdj := adjs[k]
		last, hasLast := adjLast[k.code]
		if !hasBase || !hasAdj {
			ab, a = 1, 1
		}
		if !hasLast || last == 0 {
			last = a
			if last == 0 {
				last = 1
			}
		}
		scale := a / ab
		qfq := a / last
		for _, f := range []string{"open", "high", "low", "close"} {
			if v, ok := rec[f]; ok {
				cols[f][idx] = v * scale
			}
		}
		for _, f := range []string{"vol", "amount"} {
			if v, ok := rec[f]; ok {
				cols[f][idx] = v
			}
		}
		if v, ok := rec["open"]; ok {
			cols["raw_open"][idx] = v
			cols["qfq_open"][idx] = v * qfq
		}
		if v, ok := rec["close"]; ok {
			cols["raw_close"][idx] = v
			cols["qfq_close"][idx] = v * qfq
		}
	}

	for k := range suspendSet {
		i, ok1 := dateIdx[k.d]
		j, ok2 := codeIdx[k.code]
		if !ok1 || !ok2 {
			continue
		}
		cols["suspend"][i*N+j] = 1
	}
	for k, lim := range limits {
		i, ok1 := dateIdx[k.d]
		j, ok2 := codeIdx[k.code]
		if !ok1 || !ok2 {
			continue
		}
		idx := i*N + j
		cols["up_limit"][idx] = lim.up
		cols["down_limit"][idx] = lim.down
	}

	for k, rec := range extra {
		i, ok1 := dateIdx[k.d]
		j, ok2 := codeIdx[k.code]
		if !ok1 || !ok2 || stSet[k] {
			continue
		}
		idx := i*N + j
		for f, v := range rec {
			cols[f][idx] = v
		}
	}

	for j, c := range sortedCodes {
		list := pitByCode[c]
		ptr := -1
		for i, d := range dates {
			for ptr+1 < len(list) && list[ptr+1].ann <= d {
				ptr++
			}
			if ptr >= 0 {
				idx := i*N + j
				if !stSet[key{d, c}] {
					cols["roe"][idx] = list[ptr].val
				}
			}
		}
	}

	pct := make([]float64, T*N)
	for i := range pct {
		pct[i] = math.NaN()
	}
	for j := 0; j < N; j++ {
		for i := 1; i < T; i++ {
			prev := cols["close"][(i-1)*N+j]
			cur := cols["close"][i*N+j]
			if !math.IsNaN(prev) && prev != 0 && !math.IsNaN(cur) {
				pct[i*N+j] = cur/prev - 1
			}
		}
	}
	cols["pct_chg"] = pct

	for _, f := range requiredFields {
		col := cols[f]
		valid := 0
		for _, v := range col {
			if !math.IsNaN(v) {
				valid++
			}
		}
		if valid == 0 {
			return nil, nil, fmt.Errorf("必需字段 %s 在区间内没有任何有效值，数据源不可用", f)
		}
	}

	for name, col := range cols {
		if err := p.SetCol(name, col); err != nil {
			return nil, nil, err
		}
	}

	report := &LoadReport{
		Index: b.Index, Codes: N, Dates: T,
		From: dates[0], To: dates[T-1],
		Bars: len(bars), STRows: len(stSet), STDropped: stDropped,
		ROERows: len(b.ROERows), HasROE: len(pitByCode) > 0,
		OptionalErrs: filterErrs(b.FactorErr, b.ROEErr, b.STErr, b.SuspendErr, b.LimitErr, b.NameErr),
	}
	return p, report, nil
}

func filterErrs(errs ...error) []string {
	out := make([]string, 0, len(errs))
	for _, e := range errs {
		if e != nil {
			out = append(out, e.Error())
		}
	}
	return out
}

func rowCode(r map[string]any) string {
	if c := str(r["ts_code"]); c != "" {
		return c
	}
	if c := str(r["con_code"]); c != "" {
		return c
	}
	return str(r["code"])
}

func normDate(s string) string {
	s = compactDate(s)
	if len(s) == 8 { // 20060102 → 2006-01-02
		return s[:4] + "-" + s[4:6] + "-" + s[6:8]
	}
	return s
}

func compactDate(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' && s[i] != 'T' && s[i] != '/' {
			out = append(out, s[i])
		}
	}
	return string(out)
}

func str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	default:
		return fmt.Sprint(x)
	}
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return 0, false
		}
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case string:
		if x == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(x, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		return f, true
	}
	return 0, false
}
