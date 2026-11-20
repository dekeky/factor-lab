package tushare

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// 资产类别，对应 tushare 1.4.29 pro_bar 的 asset 参数。
const (
	AssetStock           = "E"  // 股票与交易所基金
	AssetIndex           = "I"  // 沪深指数
	AssetFutures         = "FT" // 期货
	AssetFund            = "FD" // 基金
	AssetOption          = "O"  // 期权
	AssetConvertibleBond = "CB" // 可转债
	AssetCrypto          = "C"  // 数字货币
)

// 复权类型。
const (
	AdjNone = ""    // 不复权
	AdjQFQ  = "qfq" // 前复权
	AdjHFQ  = "hfq" // 后复权
)

// 因子类型，对应 factors 参数（仅股票日线有效）。
const (
	FactorTurnover = "tor" // 换手率
	FactorVolume   = "vr"  // 量比
)

// errNoAdjFactor 对应 Python pro_bar 在 adj_factor 为空时 return None。
var errNoAdjFactor = errors.New("tushare: adj_factor 为空")

// BarQuery 是 GetBar 的请求参数。
//
// 该结构对应 Python SDK 1.4.29 的 pro_bar(...)。
type BarQuery struct {
	// TsCode 证券代码，如 000001.SZ。复权时必填；不复权时允许为空（与 Python 一致）。
	TsCode string
	// Asset 资产类别，默认 AssetStock。
	Asset string
	// Freq 频度：D/W/M 或 1min/5min/15min/30min/60min，默认 D。
	Freq string
	// StartDate 开始日期，日线用 YYYYMMDD；分钟用 2019-09-01 09:00:00。
	StartDate string
	// EndDate 结束日期，留空表示今天。
	EndDate string
	// Adj 复权类型，仅 asset=E 时生效：AdjNone/AdjQFQ/AdjHFQ。
	Adj string
	// MA 需要计算的均线周期，如 []int{5, 10, 20}。
	MA []int
	// Factors 需要附加的因子（仅股票日线）：FactorTurnover、FactorVolume。
	Factors []string
	// AdjFactor 为 true 时在复权结果中保留 adj_factor 列。
	AdjFactor bool
	// Offset 起始行号，用于分页。
	Offset int
	// Limit 本次返回的最大行数。
	Limit int
	// Fields 只返回指定字段。
	Fields []string
	// Exchange 交易所代码，期货/期权/数字货币需要。
	Exchange string
	// ContractType 合约类型，数字货币等品种需要。
	ContractType string
}

// GetBar 获取 K 线数据，等价于 Python SDK 1.4.29 的 ts.pro_bar。
//
// 编排逻辑按 tushare/pro/data_pro.py 的 pro_bar 原样翻译，不额外补路由或校验。
func (c *Client) GetBar(ctx context.Context, q *BarQuery) (*Table, error) {
	if q == nil {
		q = &BarQuery{}
	}
	if strings.TrimSpace(q.TsCode) == "" && q.Adj != AdjNone {
		return nil, &APIError{Kind: KindParam, Msg: "提取复权行情必须输入ts_code参数"}
	}

	asset := strings.TrimSpace(q.Asset)
	freq := strings.TrimSpace(q.Freq)
	if freq == "" {
		freq = "D"
	}
	// 与 Python 一致：len(freq) >= 3 视为分钟级并转小写，否则按 D/W/M 处理。
	if len(freq) >= 3 {
		freq = strings.ToLower(freq)
	} else if strings.EqualFold(asset, AssetCrypto) {
		freq = strings.ToLower(freq)
	} else {
		freq = strings.ToUpper(freq)
	}
	minute := strings.Contains(freq, "min")

	start, end := q.StartDate, q.EndDate
	if !minute {
		if end == "" {
			end = time.Now().Format("20060102")
		}
		start = strings.ReplaceAll(start, "-", "")
		end = strings.ReplaceAll(end, "-", "")
	}

	code := strings.TrimSpace(q.TsCode)
	if !strings.EqualFold(asset, AssetCrypto) {
		code = strings.ToUpper(code)
	} else {
		code = strings.ToLower(code)
	}
	asset = strings.ToUpper(asset)
	if asset == "" {
		asset = AssetStock
	}

	apiName, err := resolveBarAPI(asset, freq)
	if err != nil {
		return nil, err
	}

	params := Params{}
	opts := []CallOption{}
	if asset == AssetCrypto {
		if freq == "d" {
			freq = "daily"
		} else if freq == "w" {
			freq = "week"
		}
		params["exchange"] = q.Exchange
		params["symbol"] = code
		params["freq"] = freq
		params["start_dae"] = start // 与 Python 参数名一致
		params["end_date"] = end
		params["contract_type"] = q.ContractType
	} else {
		params["ts_code"] = code
		if start != "" {
			params["start_date"] = start
		}
		if end != "" {
			params["end_date"] = end
		}
		if minute {
			params["freq"] = freq
		}
		if q.Exchange != "" {
			params["exchange"] = q.Exchange
		}
		if q.ContractType != "" {
			params["contract_type"] = q.ContractType
		}
		if q.Offset > 0 {
			opts = append(opts, WithOffset(q.Offset))
		}
		if q.Limit > 0 {
			opts = append(opts, WithLimit(q.Limit))
		}
	}

	tbl, err := c.Query(ctx, apiName, params, opts...)
	if err != nil {
		return nil, err
	}

	if asset == AssetStock {
		if freq == "D" && len(q.Factors) > 0 {
			basic, err := c.Query(ctx, "daily_basic", Params{
				"ts_code":    code,
				"start_date": start,
				"end_date":   end,
			})
			if err != nil {
				return nil, err
			}
			if err := mergeColumns(tbl, basic, "trade_date", "turnover_rate", "volume_ratio"); err != nil {
				return nil, err
			}
			keepRowsMatching(tbl, basic, "trade_date")
			if contains(q.Factors, FactorTurnover) && !contains(q.Factors, FactorVolume) {
				tbl.DropColumns("volume_ratio")
			}
			if contains(q.Factors, FactorVolume) && !contains(q.Factors, FactorTurnover) {
				tbl.DropColumns("turnover_rate")
			}
		}
		if minute {
			addTradeDateFromTime(tbl)
			if err := shiftPreClose(tbl); err != nil {
				return nil, err
			}
		}
		if q.Adj != AdjNone {
			if minute {
				if err := tbl.SortBy("trade_time", true); err != nil {
					return nil, err
				}
			}
			if err := c.adjustBar(ctx, tbl, code, start, end, q.Adj, q.AdjFactor); err != nil {
				if errors.Is(err, errNoAdjFactor) {
					return &Table{API: tbl.API}, nil
				}
				return nil, err
			}
			if minute {
				tbl.DropColumns("trade_date", "pre_close")
			} else {
				recalcChangePct(tbl)
			}
		} else {
			if err := shiftPreClose(tbl); err != nil {
				return nil, err
			}
			recalcChangePct(tbl)
		}
	}

	if len(q.MA) > 0 {
		closes, err := numericColumn(tbl, "close")
		if err != nil {
			return nil, err
		}
		vols, err := numericColumn(tbl, "vol")
		if err != nil {
			return nil, err
		}
		for _, n := range q.MA {
			if n <= 0 {
				continue
			}
			tbl.AddColumn(fmt.Sprintf("ma%d", n), rollingMean(closes, n, 2))
			tbl.AddColumn(fmt.Sprintf("ma_v_%d", n), rollingMean(vols, n, 2))
		}
	}

	if len(q.Fields) > 0 {
		return tbl.Select(q.Fields...)
	}
	return tbl, nil
}

// resolveBarAPI 把 asset + freq 映射到实际调用的 api_name（与 Python 1.4.29 一致）。
func resolveBarAPI(asset, freq string) (string, error) {
	minute := strings.Contains(freq, "min")
	switch asset {
	case AssetStock:
		switch {
		case minute:
			return "stk_mins", nil
		case freq == "D":
			return "daily", nil
		case freq == "W":
			return "weekly", nil
		case freq == "M":
			return "monthly", nil
		}
	case AssetIndex:
		switch {
		case minute:
			return "stk_mins", nil
		case freq == "D":
			return "index_daily", nil
		case freq == "W":
			return "index_weekly", nil
		case freq == "M":
			return "index_monthly", nil
		}
	case AssetFutures:
		switch {
		case minute:
			return "ft_mins", nil
		case freq == "D":
			return "fut_daily", nil
		}
	case AssetOption:
		switch {
		case minute:
			return "opt_mins", nil
		case freq == "D":
			return "opt_daily", nil
		}
	case AssetConvertibleBond:
		if freq == "D" {
			return "cb_daily", nil
		}
	case AssetFund:
		switch {
		case minute:
			return "stk_mins", nil
		case freq == "D":
			return "fund_daily", nil
		}
	case AssetCrypto:
		return "coinbar", nil
	}
	return "", &APIError{Kind: KindParam, Msg: fmt.Sprintf("asset=%s 与 freq=%s 的组合暂不支持", asset, freq)}
}

// priceColumns 是需要复权处理的价格列（Python PRICE_COLS）。
var priceColumns = []string{"open", "close", "high", "low", "pre_close"}

// adjustBar 按复权因子调整价格列。涨跌重算由调用方按 Python 分支决定。
func (c *Client) adjustBar(ctx context.Context, tbl *Table, code, start, end, adj string, keepFactor bool) error {
	factors, err := c.Query(ctx, "adj_factor", Params{
		"ts_code":    code,
		"start_date": start,
		"end_date":   end,
	})
	if err != nil {
		return err
	}
	if factors.Len() == 0 {
		return errNoAdjFactor
	}

	fDateIdx, ok := factors.FieldIndex("trade_date")
	if !ok {
		return &APIError{Kind: KindDecode, API: "adj_factor", Msg: "返回结果缺少 trade_date 列"}
	}
	fIdx, ok := factors.FieldIndex("adj_factor")
	if !ok {
		return &APIError{Kind: KindDecode, API: "adj_factor", Msg: "返回结果缺少 adj_factor 列"}
	}
	tblDateIdx, ok := tbl.FieldIndex("trade_date")
	if !ok {
		return &APIError{Kind: KindDecode, API: tbl.API, Msg: "结果缺少 trade_date 列"}
	}

	// Python: float(fcts['adj_factor'][0])，取因子表第一行（接口按日期降序时即最新日）。
	ref, err := toFloat(cellAt(factors.Items[0], fIdx))
	if err != nil || ref == 0 {
		return &APIError{Kind: KindDecode, API: "adj_factor", Msg: "复权因子无效，无法复权"}
	}

	byDate := make(map[string]float64, factors.Len())
	for _, row := range factors.Items {
		d := toString(cellAt(row, fDateIdx))
		f, err := toFloat(cellAt(row, fIdx))
		if err != nil || d == "" {
			continue
		}
		byDate[d] = f
	}

	values := make([]float64, tbl.Len())
	known := make([]bool, tbl.Len())
	for i, row := range tbl.Items {
		d := toString(cellAt(row, tblDateIdx))
		if f, ok := byDate[d]; ok {
			values[i], known[i] = f, true
		}
	}
	// pandas Series.bfill()：沿当前行序向后取下一个有效值。
	for i := tbl.Len() - 2; i >= 0; i-- {
		if !known[i] && known[i+1] {
			values[i], known[i] = values[i+1], true
		}
	}

	for _, col := range priceColumns {
		idx, ok := tbl.FieldIndex(col)
		if !ok {
			continue
		}
		for i, row := range tbl.Items {
			if idx >= len(row) {
				continue
			}
			if !known[i] {
				row[idx] = nil
				continue
			}
			p, err := toFloat(row[idx])
			if err != nil {
				row[idx] = nil
				continue
			}
			if adj == AdjHFQ {
				p *= values[i]
			}
			if adj == AdjQFQ {
				p = p * values[i] / ref
			}
			row[idx] = round(p, 2)
		}
	}

	tbl.AddColumn("adj_factor", floatsToAny(values))
	if !keepFactor {
		tbl.DropColumns("adj_factor")
	}
	return nil
}

func addTradeDateFromTime(tbl *Table) {
	idx, ok := tbl.FieldIndex("trade_time")
	if !ok {
		return
	}
	dates := make([]any, tbl.Len())
	for i, row := range tbl.Items {
		s := strings.ReplaceAll(toString(cellAt(row, idx)), "-", "")
		if len(s) >= 8 {
			dates[i] = s[:8]
		}
	}
	tbl.AddColumn("trade_date", dates)
}

func shiftPreClose(tbl *Table) error {
	closeIdx, ok := tbl.FieldIndex("close")
	if !ok {
		return &APIError{Kind: KindDecode, API: tbl.API, Msg: "结果缺少 close 列"}
	}
	n := tbl.Len()
	pre := make([]any, n)
	for i := 0; i < n-1; i++ {
		pre[i] = cellAt(tbl.Items[i+1], closeIdx)
	}
	tbl.AddColumn("pre_close", pre)
	return nil
}

func recalcChangePct(tbl *Table) {
	closeIdx, ok1 := tbl.FieldIndex("close")
	preIdx, ok2 := tbl.FieldIndex("pre_close")
	if !ok1 || !ok2 {
		return
	}
	change := make([]any, tbl.Len())
	pct := make([]any, tbl.Len())
	for i, row := range tbl.Items {
		cv := cellAt(row, closeIdx)
		pv := cellAt(row, preIdx)
		if cv == nil || pv == nil {
			continue
		}
		c0, err1 := toFloat(cv)
		p0, err2 := toFloat(pv)
		if err1 != nil || err2 != nil || p0 == 0 {
			continue
		}
		ch := c0 - p0
		change[i] = ch
		pct[i] = round(ch/p0*100, 2)
	}
	tbl.AddColumn("change", change)
	tbl.AddColumn("pct_chg", pct)
}

// mergeColumns 按 key 列把 other 的若干列合并进 tbl（左连接，缺失为 nil）。
func mergeColumns(tbl, other *Table, key string, cols ...string) error {
	kIdx, ok := tbl.FieldIndex(key)
	if !ok {
		return &APIError{Kind: KindDecode, API: tbl.API, Msg: fmt.Sprintf("主结果缺少 %s 列", key)}
	}
	okIdx, ok := other.FieldIndex(key)
	if !ok {
		return nil
	}
	lookup := make(map[string][]any, other.Len())
	for _, row := range other.Items {
		k := toString(cellAt(row, okIdx))
		if k == "" {
			continue
		}
		vals := make([]any, len(cols))
		for i, c := range cols {
			if ci, ok := other.FieldIndex(c); ok {
				vals[i] = cellAt(row, ci)
			}
		}
		lookup[k] = vals
	}
	for i, c := range cols {
		values := make([]any, tbl.Len())
		for r, row := range tbl.Items {
			if v, hit := lookup[toString(cellAt(row, kIdx))]; hit {
				values[r] = v[i]
			}
		}
		tbl.AddColumn(c, values)
	}
	return nil
}

// keepRowsMatching 对应 pandas merge(how='inner')：只保留 key 在 other 中出现过的行。
func keepRowsMatching(tbl, other *Table, key string) {
	okIdx, ok := other.FieldIndex(key)
	if !ok {
		tbl.Items = nil
		return
	}
	keys := make(map[string]struct{}, other.Len())
	for _, row := range other.Items {
		k := toString(cellAt(row, okIdx))
		if k != "" {
			keys[k] = struct{}{}
		}
	}
	kIdx, ok := tbl.FieldIndex(key)
	if !ok {
		tbl.Items = nil
		return
	}
	kept := make([][]any, 0, tbl.Len())
	for _, row := range tbl.Items {
		if _, hit := keys[toString(cellAt(row, kIdx))]; hit {
			kept = append(kept, row)
		}
	}
	tbl.Items = kept
}

// numericColumn 提取数值列，缺失值以 NaN 占位以便均线计算跳过。
func numericColumn(tbl *Table, name string) ([]float64, error) {
	idx, ok := tbl.FieldIndex(name)
	if !ok {
		return nil, &APIError{Kind: KindDecode, API: tbl.API, Msg: fmt.Sprintf("结果缺少 %s 列，无法计算", name)}
	}
	out := make([]float64, tbl.Len())
	for i, row := range tbl.Items {
		v := cellAt(row, idx)
		if v == nil {
			out[i] = math.NaN()
			continue
		}
		f, err := toFloat(v)
		if err != nil {
			out[i] = math.NaN()
			continue
		}
		out[i] = f
	}
	return out, nil
}

// rollingMean 计算前 N 日均值。
//
// 结果集按交易日期降序排列，因此第 i 行的 N 日窗口为 [i, i+N-1]，
// 与 Python 的 rolling(N).mean().shift(-(N-1)) 语义一致。
func rollingMean(vals []float64, n, digits int) []any {
	out := make([]any, len(vals))
	if n <= 0 || len(vals) == 0 {
		return out
	}
	for i := 0; i < len(vals); i++ {
		if i+n > len(vals) {
			break
		}
		sum, ok := 0.0, true
		for j := i; j < i+n; j++ {
			if math.IsNaN(vals[j]) {
				ok = false
				break
			}
			sum += vals[j]
		}
		if !ok {
			continue
		}
		out[i] = round(sum/float64(n), digits)
	}
	return out
}

func round(v float64, digits int) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return v
	}
	p := math.Pow10(digits)
	return math.Round(v*p) / p
}

func floatsToAny(vals []float64) []any {
	out := make([]any, len(vals))
	for i, v := range vals {
		out[i] = round(v, 6)
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
