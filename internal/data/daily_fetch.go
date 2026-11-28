package data

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"sync"

	"github.com/dekeky/tushare-go-sdk"
)

const dailyRowLimit = 6000
const dailySyncStart = "20000101"
const defaultAPIRPM = 200

var dailyBarFields = []string{"ts_code", "trade_date", "open", "high", "low", "close", "pre_close", "vol", "amount"}

func dailyBasicFetchFields() []string {
	return append([]string{"ts_code", "trade_date"}, DailyBasicCols...)
}

func batchTradeDates(dates []string) [][]string {
	if len(dates) == 0 {
		return nil
	}
	out := make([][]string, 0, (len(dates)+1)/2)
	for i := 0; i < len(dates); i += 2 {
		j := i + 2
		if j > len(dates) {
			j = len(dates)
		}
		out = append(out, dates[i:j])
	}
	return out
}

// shouldSplitBatch reports whether a two-day window must be retried as one-day
// calls. A single-day window is paginated instead of split.
func shouldSplitBatch(n int, hasMore bool, days int) bool {
	if days <= 1 {
		return false
	}
	return n >= dailyRowLimit || hasMore
}

func joinDailyBasic(bars, basic []map[string]any) []DailyParquetRow {
	type key struct{ code, date string }
	extra := make(map[key]map[string]any, len(basic))
	for _, r := range basic {
		code := rowCode(r)
		d := compactDate(str(r["trade_date"]))
		if code == "" || d == "" {
			continue
		}
		extra[key{code, d}] = r
	}
	out := make([]DailyParquetRow, 0, len(bars))
	for _, r := range bars {
		code := rowCode(r)
		d := compactDate(str(r["trade_date"]))
		if code == "" || len(d) != 8 {
			continue
		}
		row := DailyParquetRow{
			TsCode:    code,
			TradeDate: d,
			Open:      floatOrNaN(r, "open"),
			High:      floatOrNaN(r, "high"),
			Low:       floatOrNaN(r, "low"),
			Close:     floatOrNaN(r, "close"),
			PreClose:  floatOrNaN(r, "pre_close"),
			Vol:       floatOrNaN(r, "vol"),
			Amount:    floatOrNaN(r, "amount"),
		}
		fillBasicNaN(&row)
		if b, ok := extra[key{code, d}]; ok {
			applyBasic(&row, b)
		}
		out = append(out, row)
	}
	return out
}

func fillBasicNaN(row *DailyParquetRow) {
	row.TurnoverRate = math.NaN()
	row.TurnoverRateF = math.NaN()
	row.VolumeRatio = math.NaN()
	row.PE = math.NaN()
	row.PETTM = math.NaN()
	row.PB = math.NaN()
	row.PS = math.NaN()
	row.PSTTM = math.NaN()
	row.DVRatio = math.NaN()
	row.DVTTM = math.NaN()
	row.TotalShare = math.NaN()
	row.FloatShare = math.NaN()
	row.FreeShare = math.NaN()
	row.TotalMV = math.NaN()
	row.CircMV = math.NaN()
	row.LimitStatus = math.NaN()
}

func applyBasic(row *DailyParquetRow, r map[string]any) {
	row.TurnoverRate = floatOrNaN(r, "turnover_rate")
	row.TurnoverRateF = floatOrNaN(r, "turnover_rate_f")
	row.VolumeRatio = floatOrNaN(r, "volume_ratio")
	row.PE = floatOrNaN(r, "pe")
	row.PETTM = floatOrNaN(r, "pe_ttm")
	row.PB = floatOrNaN(r, "pb")
	row.PS = floatOrNaN(r, "ps")
	row.PSTTM = floatOrNaN(r, "ps_ttm")
	row.DVRatio = floatOrNaN(r, "dv_ratio")
	row.DVTTM = floatOrNaN(r, "dv_ttm")
	row.TotalShare = floatOrNaN(r, "total_share")
	row.FloatShare = floatOrNaN(r, "float_share")
	row.FreeShare = floatOrNaN(r, "free_share")
	row.TotalMV = floatOrNaN(r, "total_mv")
	row.CircMV = floatOrNaN(r, "circ_mv")
	row.LimitStatus = floatOrNaN(r, "limit_status")
}

func floatOrNaN(r map[string]any, key string) float64 {
	if v, ok := toFloat(r[key]); ok {
		return v
	}
	return math.NaN()
}

func (s *TushareSource) syncDailyParquet(ctx context.Context, cli *tushare.Client) error {
	if s.DataDir == "" {
		return fmt.Errorf("未配置 DataDir，无法写入 parquet")
	}
	from := dailySyncStart
	to := endOrToday(s.End)
	cal, err := loadOpenDates(ctx, cli, from, to)
	if err != nil {
		return fmt.Errorf("取交易日历失败: %w", err)
	}
	return s.syncDailyMissing(ctx, cli, cal)
}

func (s *TushareSource) syncDailyMissing(ctx context.Context, cli *tushare.Client, cal []string) error {
	if s.DataDir == "" {
		return fmt.Errorf("未配置 DataDir，无法写入 parquet")
	}
	if len(cal) == 0 {
		return nil
	}
	dir := dailyParquetDir(s.DataDir)
	cursor, err := resolveAsOf(s.DataDir, dsDaily, dir, func() (map[string]bool, error) {
		return parquetDates(dir, cal[0], cal[len(cal)-1])
	})
	if err != nil {
		return err
	}
	missing := datesAfter(cal, cursor)
	if len(missing) == 0 {
		slog.Info("日K 已到最新", "as_of", cursor, "cal_to", cal[len(cal)-1])
		return nil
	}
	slog.Info("增量同步日K到 parquet", "as_of", cursor, "missing_days", len(missing), "from", missing[0], "to", missing[len(missing)-1])

	var buf []DailyParquetRow
	bufYear := 0
	var pending []string
	flush := func() error {
		if len(buf) == 0 || bufYear == 0 {
			return nil
		}
		path := shardPath(dir, bufYear)
		old, err := readShardIfExists(path)
		if err != nil {
			return err
		}
		if err := WriteDailyShard(path, mergeDailyRows(old, buf)); err != nil {
			return err
		}
		if err := advanceAsOf(s.DataDir, dsDaily, maxYYYYMMDD(pending)); err != nil {
			return err
		}
		buf = buf[:0]
		pending = nil
		return nil
	}

	for _, batch := range batchTradeDates(missing) {
		year := yearOfYYYYMMDD(batch[0])
		if bufYear != 0 {
			bs, be := shardRange(bufYear)
			ys, ye := shardRange(year)
			if bs != ys || be != ye {
				if err := flush(); err != nil {
					return err
				}
			}
		}
		bars, basic, err := fetchDailyAndBasic(ctx, cli, batch)
		if err != nil {
			return err
		}
		joined := joinDailyBasic(bars, basic)
		buf = append(buf, joined...)
		seen := make(map[string]bool, len(joined))
		for _, r := range joined {
			seen[r.TradeDate] = true
		}
		for _, d := range batch {
			if seen[d] {
				pending = append(pending, d)
			}
		}
		bufYear = year
	}
	return flush()
}

func fetchDailyAndBasic(ctx context.Context, cli *tushare.Client, dates []string) (bars, basic []map[string]any, err error) {
	var err1, err2 error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		bars, err1 = fetchMarketAPI(ctx, cli, "daily", dates, dailyBarFields)
	}()
	go func() {
		defer wg.Done()
		basic, err2 = fetchMarketAPI(ctx, cli, "daily_basic", dates, dailyBasicFetchFields())
	}()
	wg.Wait()
	if err1 != nil {
		return nil, nil, err1
	}
	if err2 != nil {
		slog.Warn("daily_basic 拉取失败，日K仍写入 parquet", "err", err2)
		basic = nil
	}
	return bars, basic, nil
}

func fetchMarketAPI(ctx context.Context, cli *tushare.Client, api string, dates []string, fields []string) ([]map[string]any, error) {
	if len(dates) == 0 {
		return nil, nil
	}
	if len(dates) == 1 {
		return queryAPIPaged(ctx, cli, api, tushare.Params{"trade_date": dates[0]}, fields, dailyRowLimit)
	}
	params := tushare.Params{"start_date": dates[0], "end_date": dates[len(dates)-1]}
	rows, hasMore, err := queryAPIOnce(ctx, cli, api, params, fields, dailyRowLimit)
	if err != nil {
		return nil, err
	}
	if shouldSplitBatch(len(rows), hasMore, len(dates)) {
		var all []map[string]any
		for _, d := range dates {
			part, err := queryAPIPaged(ctx, cli, api, tushare.Params{"trade_date": d}, fields, dailyRowLimit)
			if err != nil {
				return nil, err
			}
			all = append(all, part...)
		}
		return all, nil
	}
	return rows, nil
}

func queryAPIOnce(ctx context.Context, cli *tushare.Client, api string, params tushare.Params, fields []string, pageSize int) ([]map[string]any, bool, error) {
	if pageSize <= 0 {
		pageSize = dailyRowLimit
	}
	opts := []tushare.CallOption{tushare.WithLimit(pageSize)}
	if len(fields) > 0 {
		opts = append(opts, tushare.WithFields(fields...))
	}
	tbl, err := cli.Query(ctx, api, params, opts...)
	if err != nil {
		return nil, false, err
	}
	return tbl.Records(), tbl.HasMore, nil
}

func loadOpenDates(ctx context.Context, cli *tushare.Client, from, to string) ([]string, error) {
	rows, err := queryAPIPaged(ctx, cli, "trade_cal", tushare.Params{
		"exchange":   "SSE",
		"start_date": from,
		"end_date":   to,
		"is_open":    "1",
	}, []string{"cal_date", "is_open"}, 5000)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	seen := map[string]bool{}
	for _, r := range rows {
		if !isOpenFlag(r["is_open"]) {
			continue
		}
		d := compactDate(str(r["cal_date"]))
		if len(d) == 8 && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	sort.Strings(out)
	return out, nil
}

func isOpenFlag(v any) bool {
	switch x := v.(type) {
	case string:
		return x == "1" || x == "true"
	case int:
		return x == 1
	case int64:
		return x == 1
	case float64:
		return x == 1
	default:
		return false
	}
}

func yearOfYYYYMMDD(d string) int {
	if len(d) < 4 {
		return parquetEpochYear
	}
	y, err := strconv.Atoi(d[:4])
	if err != nil || y == 0 {
		return parquetEpochYear
	}
	return y
}
