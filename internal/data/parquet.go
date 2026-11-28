package data

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// DailyParquetRow is one joined daily + daily_basic record.
// daily_basic.close is not stored; Close is the daily unadjusted close.
type DailyParquetRow struct {
	TsCode    string  `parquet:"ts_code"`
	TradeDate string  `parquet:"trade_date"`
	Open      float64 `parquet:"open"`
	High      float64 `parquet:"high"`
	Low       float64 `parquet:"low"`
	Close     float64 `parquet:"close"`
	PreClose  float64 `parquet:"pre_close"`
	Vol       float64 `parquet:"vol"`
	Amount    float64 `parquet:"amount"`

	TurnoverRate  float64 `parquet:"turnover_rate"`
	TurnoverRateF float64 `parquet:"turnover_rate_f"`
	VolumeRatio   float64 `parquet:"volume_ratio"`
	PE            float64 `parquet:"pe"`
	PETTM         float64 `parquet:"pe_ttm"`
	PB            float64 `parquet:"pb"`
	PS            float64 `parquet:"ps"`
	PSTTM         float64 `parquet:"ps_ttm"`
	DVRatio       float64 `parquet:"dv_ratio"`
	DVTTM         float64 `parquet:"dv_ttm"`
	TotalShare    float64 `parquet:"total_share"`
	FloatShare    float64 `parquet:"float_share"`
	FreeShare     float64 `parquet:"free_share"`
	TotalMV       float64 `parquet:"total_mv"`
	CircMV        float64 `parquet:"circ_mv"`
	LimitStatus   float64 `parquet:"limit_status"`
}

const parquetEpochYear = 2000
const parquetShardYears = 5

func shardRange(year int) (start, end int) {
	if year < parquetEpochYear {
		year = parquetEpochYear
	}
	start = parquetEpochYear + ((year-parquetEpochYear)/parquetShardYears)*parquetShardYears
	end = start + parquetShardYears - 1
	return
}

func shardPath(dir string, year int) string {
	start, end := shardRange(year)
	return filepath.Join(dir, fmt.Sprintf("%d-%d.parquet", start, end))
}

func dailyParquetDir(dataDir string) string {
	return filepath.Join(dataDir, "parquet", "daily")
}

func WriteDailyShard(path string, rows []DailyParquetRow) error {
	return writeParquetFile(path, rows)
}

func ReadDailyShard(path string) ([]DailyParquetRow, error) {
	return readParquetFile[DailyParquetRow](path)
}

// dailyDateCol 只投影 trade_date，用来建覆盖索引，避免把整片日K读进内存。
type dailyDateCol struct {
	TradeDate string `parquet:"trade_date"`
}

func parquetDates(dir, from, to string) (map[string]bool, error) {
	return parquetDateSet(dir, from, to, func(r dailyDateCol) string { return r.TradeDate })
}

func mergeDailyRows(old, neu []DailyParquetRow) []DailyParquetRow {
	type key struct{ code, date string }
	m := make(map[key]DailyParquetRow, len(old)+len(neu))
	order := make([]key, 0, len(old)+len(neu))
	add := func(r DailyParquetRow) {
		k := key{r.TsCode, r.TradeDate}
		if _, ok := m[k]; !ok {
			order = append(order, k)
		}
		m[k] = r
	}
	for _, r := range old {
		add(r)
	}
	for _, r := range neu {
		add(r)
	}
	out := make([]DailyParquetRow, len(order))
	for i, k := range order {
		out[i] = m[k]
	}
	return out
}

func readDailyParquet(dir, start, end string, codes []string) ([]DailyParquetRow, error) {
	start = compactDate(start)
	end = compactDate(end)
	want := map[string]bool{}
	for _, c := range codes {
		want[c] = true
	}
	y0, y1 := parquetEpochYear, parquetEpochYear
	if len(start) >= 4 {
		fmt.Sscanf(start[:4], "%d", &y0)
	}
	if len(end) >= 4 {
		fmt.Sscanf(end[:4], "%d", &y1)
	} else {
		y1 = time.Now().Year()
	}
	var out []DailyParquetRow
	seen := map[string]bool{}
	for y := y0; y <= y1; y++ {
		path := shardPath(dir, y)
		if seen[path] {
			continue
		}
		seen[path] = true
		if _, err := os.Stat(path); err != nil {
			continue
		}
		t0 := time.Now()
		rows, err := ReadDailyShard(path)
		if err != nil {
			return nil, err
		}
		before := len(out)
		for _, r := range rows {
			if len(want) > 0 && !want[r.TsCode] {
				continue
			}
			if start != "" && r.TradeDate < start {
				continue
			}
			if end != "" && r.TradeDate > end {
				continue
			}
			out = append(out, r)
		}
		slog.Info("读日K分片", "file", filepath.Base(path), "rows", len(rows), "kept", len(out)-before,
			"elapsed", time.Since(t0).Round(time.Millisecond).String())
	}
	return out, nil
}

func parquetToPanelRows(rows []DailyParquetRow) (bars, extra []map[string]any) {
	bars = make([]map[string]any, 0, len(rows))
	extra = make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		bars = append(bars, map[string]any{
			"ts_code": r.TsCode, "trade_date": r.TradeDate,
			"open": r.Open, "high": r.High, "low": r.Low, "close": r.Close,
			"pre_close": r.PreClose, "vol": r.Vol, "amount": r.Amount,
		})
		extra = append(extra, map[string]any{
			"ts_code": r.TsCode, "trade_date": r.TradeDate,
			"turnover_rate": r.TurnoverRate, "turnover_rate_f": r.TurnoverRateF,
			"volume_ratio": r.VolumeRatio,
			"pe":           r.PE, "pe_ttm": r.PETTM, "pb": r.PB, "ps": r.PS, "ps_ttm": r.PSTTM,
			"dv_ratio": r.DVRatio, "dv_ttm": r.DVTTM,
			"total_share": r.TotalShare, "float_share": r.FloatShare, "free_share": r.FreeShare,
			"total_mv": r.TotalMV, "circ_mv": r.CircMV, "limit_status": r.LimitStatus,
		})
	}
	return bars, extra
}

func readShardIfExists(path string) ([]DailyParquetRow, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return ReadDailyShard(path)
}
