package data

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/dekeky/tushare-go-sdk"
)

const (
	stkLimitStart = "20070104"
	finaVIPAPI    = "fina_indicator_vip"
)

type AdjParquetRow struct {
	TsCode    string  `parquet:"ts_code"`
	TradeDate string  `parquet:"trade_date"`
	AdjFactor float64 `parquet:"adj_factor"`
}

type FinaParquetRow struct {
	TsCode  string  `parquet:"ts_code"`
	AnnDate string  `parquet:"ann_date"`
	EndDate string  `parquet:"end_date"`
	ROE     float64 `parquet:"roe"`
}

type STParquetRow struct {
	TsCode    string `parquet:"ts_code"`
	TradeDate string `parquet:"trade_date"`
	Name      string `parquet:"name"`
	Type      string `parquet:"type"`
}

type SuspendParquetRow struct {
	TsCode        string `parquet:"ts_code"`
	TradeDate     string `parquet:"trade_date"`
	SuspendTiming string `parquet:"suspend_timing"`
	SuspendType   string `parquet:"suspend_type"`
}

type LimitParquetRow struct {
	TsCode    string  `parquet:"ts_code"`
	TradeDate string  `parquet:"trade_date"`
	UpLimit   float64 `parquet:"up_limit"`
	DownLimit float64 `parquet:"down_limit"`
}

func (s *TushareSource) syncAdjFactorParquet(ctx context.Context, cli *tushare.Client, cal []string) error {
	dir := datasetParquetDir(s.DataDir, dsAdj)
	return syncByDay(ctx, s.DataDir, dsAdj, dir, cal, "", func(day string) ([]AdjParquetRow, error) {
		rows, err := queryAPIPaged(ctx, cli, "adj_factor", tushare.Params{"trade_date": day},
			[]string{"ts_code", "trade_date", "adj_factor"}, dailyRowLimit)
		if err != nil {
			return nil, err
		}
		out := make([]AdjParquetRow, 0, len(rows))
		for _, r := range rows {
			out = append(out, AdjParquetRow{
				TsCode: rowCode(r), TradeDate: compactDate(str(r["trade_date"])),
				AdjFactor: floatOrNaN(r, "adj_factor"),
			})
		}
		return out, nil
	}, func(r AdjParquetRow) string { return r.TradeDate }, func(r AdjParquetRow) string {
		return codeDateKey(r.TsCode, r.TradeDate)
	}, "复权因子")
}

func (s *TushareSource) syncFinaParquet(ctx context.Context, cli *tushare.Client) error {
	dir := datasetParquetDir(s.DataDir, dsFina)
	from, to := panelRange(s.Start, s.End)
	periods := quarterPeriods(finaStart(from), to)
	cursor, err := resolveAsOf(s.DataDir, dsFina, dir, func() (map[string]bool, error) {
		return parquetDateSet(dir, finaStart(from), to, func(r FinaParquetRow) string { return r.EndDate })
	})
	if err != nil {
		return err
	}
	missing := datesAfter(periods, cursor)
	if n := len(periods); n > 0 {
		last := periods[n-1]
		if last <= cursor {
			missing = append([]string{last}, missing...)
		}
	}
	if len(missing) == 0 {
		slog.Info("财务指标已到最新", "as_of", cursor)
		return nil
	}
	t0 := time.Now()
	slog.Info("增量同步财务指标到 parquet", "as_of", cursor, "missing_periods", len(missing), "api", finaVIPAPI,
		"from", missing[0], "to", missing[len(missing)-1])

	var buf []FinaParquetRow
	bufYear := 0
	var pending []string
	flush := func() error {
		done := maxYYYYMMDD(pending)
		if err := flushDated(dir, bufYear, buf, pending, func(r FinaParquetRow) string {
			return codeDateKey(r.TsCode, r.AnnDate)
		}, func() { buf = buf[:0]; pending = nil }); err != nil {
			return err
		}
		return advanceAsOf(s.DataDir, dsFina, done)
	}

	for _, period := range missing {
		year := yearOfYYYYMMDD(period)
		if shardChanged(bufYear, year) {
			if err := flush(); err != nil {
				return err
			}
		}
		rows, err := queryAPIPaged(ctx, cli, finaVIPAPI, tushare.Params{"period": period},
			[]string{"ts_code", "ann_date", "end_date", "roe"}, dailyRowLimit)
		if err != nil {
			return err
		}
		for _, r := range rows {
			buf = append(buf, FinaParquetRow{
				TsCode: rowCode(r), AnnDate: compactDate(str(r["ann_date"])),
				EndDate: compactDate(str(r["end_date"])), ROE: floatOrNaN(r, "roe"),
			})
		}
		pending = append(pending, period)
		bufYear = year
	}
	if err := flush(); err != nil {
		return err
	}
	slog.Info("财务指标 parquet 同步完成", "periods", len(missing), "elapsed", time.Since(t0).Round(time.Millisecond).String())
	return nil
}

func (s *TushareSource) syncStockSTParquet(ctx context.Context, cli *tushare.Client, cal []string) error {
	dir := datasetParquetDir(s.DataDir, dsST)
	return syncByDay(ctx, s.DataDir, dsST, dir, cal, "", func(day string) ([]STParquetRow, error) {
		rows, err := queryAPIPaged(ctx, cli, "stock_st", tushare.Params{"trade_date": day},
			[]string{"ts_code", "trade_date", "name", "type"}, 5000)
		if err != nil {
			return nil, err
		}
		out := make([]STParquetRow, 0, len(rows))
		for _, r := range rows {
			out = append(out, STParquetRow{
				TsCode: rowCode(r), TradeDate: compactDate(str(r["trade_date"])),
				Name: str(r["name"]), Type: str(r["type"]),
			})
		}
		return out, nil
	}, func(r STParquetRow) string { return r.TradeDate }, func(r STParquetRow) string {
		return codeDateKey(r.TsCode, r.TradeDate)
	}, "ST名单")
}

func (s *TushareSource) syncSuspendParquet(ctx context.Context, cli *tushare.Client, cal []string) error {
	dir := datasetParquetDir(s.DataDir, dsSuspend)
	return syncByDay(ctx, s.DataDir, dsSuspend, dir, cal, "", func(day string) ([]SuspendParquetRow, error) {
		rows, err := queryAPIPaged(ctx, cli, "suspend_d", tushare.Params{
			"trade_date": day, "suspend_type": "S",
		}, []string{"ts_code", "trade_date", "suspend_timing", "suspend_type"}, 5000)
		if err != nil {
			return nil, err
		}
		out := make([]SuspendParquetRow, 0, len(rows))
		for _, r := range rows {
			out = append(out, SuspendParquetRow{
				TsCode: rowCode(r), TradeDate: compactDate(str(r["trade_date"])),
				SuspendTiming: str(r["suspend_timing"]), SuspendType: str(r["suspend_type"]),
			})
		}
		return out, nil
	}, func(r SuspendParquetRow) string { return r.TradeDate }, func(r SuspendParquetRow) string {
		return codeDateKey(r.TsCode, r.TradeDate)
	}, "停牌")
}

func (s *TushareSource) syncLimitParquet(ctx context.Context, cli *tushare.Client, cal []string) error {
	dir := datasetParquetDir(s.DataDir, dsLimit)
	return syncByDay(ctx, s.DataDir, dsLimit, dir, cal, stkLimitStart, func(day string) ([]LimitParquetRow, error) {
		rows, err := queryAPIPaged(ctx, cli, "stk_limit", tushare.Params{"trade_date": day},
			[]string{"ts_code", "trade_date", "up_limit", "down_limit"}, dailyRowLimit)
		if err != nil {
			return nil, err
		}
		out := make([]LimitParquetRow, 0, len(rows))
		for _, r := range rows {
			out = append(out, LimitParquetRow{
				TsCode: rowCode(r), TradeDate: compactDate(str(r["trade_date"])),
				UpLimit: floatOrNaN(r, "up_limit"), DownLimit: floatOrNaN(r, "down_limit"),
			})
		}
		return out, nil
	}, func(r LimitParquetRow) string { return r.TradeDate }, func(r LimitParquetRow) string {
		return codeDateKey(r.TsCode, r.TradeDate)
	}, "涨跌停价")
}

func syncByDay[T any](
	ctx context.Context,
	dataDir, dataset, dir string,
	cal []string,
	notBefore string,
	fetch func(day string) ([]T, error),
	dateOf func(T) string,
	key func(T) string,
	label string,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cal = clipCal(cal, notBefore)
	if len(cal) == 0 {
		return nil
	}
	cursor, err := resolveAsOf(dataDir, dataset, dir, func() (map[string]bool, error) {
		return parquetDateSet(dir, cal[0], cal[len(cal)-1], dateOf)
	})
	if err != nil {
		return err
	}
	missing := datesAfter(cal, cursor)
	if len(missing) == 0 {
		slog.Info(label+"已到最新", "as_of", cursor, "cal_to", cal[len(cal)-1])
		return nil
	}
	slog.Info("增量同步"+label+"到 parquet", "as_of", cursor, "missing_days", len(missing), "from", missing[0], "to", missing[len(missing)-1])

	var buf []T
	bufYear := 0
	var pending []string
	flush := func() error {
		done := maxYYYYMMDD(pending)
		if err := flushDated(dir, bufYear, buf, pending, key, func() { buf = buf[:0]; pending = nil }); err != nil {
			return err
		}
		return advanceAsOf(dataDir, dataset, done)
	}
	for _, d := range missing {
		if err := ctx.Err(); err != nil {
			return err
		}
		year := yearOfYYYYMMDD(d)
		if shardChanged(bufYear, year) {
			if err := flush(); err != nil {
				return err
			}
		}
		rows, err := fetch(d)
		if err != nil {
			return err
		}
		buf = append(buf, rows...)
		pending = append(pending, d)
		bufYear = year
	}
	return flush()
}

func flushDated[T any](dir string, year int, buf []T, pending []string, key func(T) string, reset func()) error {
	if year != 0 && len(buf) > 0 {
		path := shardPath(dir, year)
		old, err := readParquetIfExists[T](path)
		if err != nil {
			return err
		}
		if err := writeParquetFile(path, mergeByKey(old, buf, key)); err != nil {
			return err
		}
	}
	reset()
	return nil
}

func quarterPeriods(from, to string) []string {
	from, to = compactDate(from), compactDate(to)
	if len(from) < 8 {
		from = dailySyncStart
	}
	if len(to) < 8 {
		to = time.Now().Format("20060102")
	}
	y0, _ := strconv.Atoi(from[:4])
	y1, _ := strconv.Atoi(to[:4])
	var out []string
	for y := y0; y <= y1; y++ {
		for _, md := range []string{"0331", "0630", "0930", "1231"} {
			p := fmt.Sprintf("%04d%s", y, md)
			if p >= from && p <= to {
				out = append(out, p)
			}
		}
	}
	return out
}

func wantCodes(codes []string) map[string]bool {
	want := map[string]bool{}
	for _, c := range codes {
		want[c] = true
	}
	return want
}

func (s *TushareSource) readAdjMaps(start, end string, codes []string) ([]map[string]any, error) {
	want := wantCodes(codes)
	rows, err := readDataset(datasetParquetDir(s.DataDir, "adj_factor"), start, end,
		func(r AdjParquetRow) string { return r.TradeDate },
		func(r AdjParquetRow) bool { return len(want) == 0 || want[r.TsCode] })
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, len(rows))
	for i, r := range rows {
		out[i] = map[string]any{"ts_code": r.TsCode, "trade_date": r.TradeDate, "adj_factor": r.AdjFactor}
	}
	return out, nil
}

func (s *TushareSource) readFinaMaps(annFrom, annTo string, codes []string) ([]map[string]any, error) {
	want := wantCodes(codes)
	annFrom, annTo = compactDate(annFrom), compactDate(annTo)
	rows, err := readDataset(datasetParquetDir(s.DataDir, "fina_indicator"), "", "",
		func(r FinaParquetRow) string { return r.EndDate },
		func(r FinaParquetRow) bool {
			if len(want) > 0 && !want[r.TsCode] {
				return false
			}
			a := compactDate(r.AnnDate)
			if annFrom != "" && a < annFrom {
				return false
			}
			if annTo != "" && a > annTo {
				return false
			}
			return true
		})
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, len(rows))
	for i, r := range rows {
		out[i] = map[string]any{"ts_code": r.TsCode, "ann_date": r.AnnDate, "end_date": r.EndDate, "roe": r.ROE}
	}
	return out, nil
}

func (s *TushareSource) readSTMaps(start, end string, codes []string) ([]map[string]any, error) {
	want := wantCodes(codes)
	rows, err := readDataset(datasetParquetDir(s.DataDir, "stock_st"), start, end,
		func(r STParquetRow) string { return r.TradeDate },
		func(r STParquetRow) bool { return len(want) == 0 || want[r.TsCode] })
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, len(rows))
	for i, r := range rows {
		out[i] = map[string]any{"ts_code": r.TsCode, "trade_date": r.TradeDate, "name": r.Name, "type": r.Type}
	}
	return out, nil
}

func (s *TushareSource) readSuspendMaps(start, end string, codes []string) ([]map[string]any, error) {
	want := wantCodes(codes)
	rows, err := readDataset(datasetParquetDir(s.DataDir, "suspend_d"), start, end,
		func(r SuspendParquetRow) string { return r.TradeDate },
		func(r SuspendParquetRow) bool { return len(want) == 0 || want[r.TsCode] })
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, len(rows))
	for i, r := range rows {
		out[i] = map[string]any{
			"ts_code": r.TsCode, "trade_date": r.TradeDate,
			"suspend_timing": r.SuspendTiming, "suspend_type": r.SuspendType,
		}
	}
	return out, nil
}

func (s *TushareSource) readLimitMaps(start, end string, codes []string) ([]map[string]any, error) {
	want := wantCodes(codes)
	rows, err := readDataset(datasetParquetDir(s.DataDir, "stk_limit"), start, end,
		func(r LimitParquetRow) string { return r.TradeDate },
		func(r LimitParquetRow) bool { return len(want) == 0 || want[r.TsCode] })
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, len(rows))
	for i, r := range rows {
		out[i] = map[string]any{
			"ts_code": r.TsCode, "trade_date": r.TradeDate,
			"up_limit": r.UpLimit, "down_limit": r.DownLimit,
		}
	}
	return out, nil
}
