package data

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestJoinDailyBasicKeepsDailyClose(t *testing.T) {
	bars := []map[string]any{
		{"ts_code": "000001.SZ", "trade_date": "20250102", "open": 10.0, "high": 11.0, "low": 9.0, "close": 10.0, "pre_close": 9.5, "vol": 100.0, "amount": 1000.0},
	}
	basic := []map[string]any{
		{"ts_code": "000001.SZ", "trade_date": "20250102", "close": 99.0, "turnover_rate": 1.5},
	}
	rows := joinDailyBasic(bars, basic)
	if len(rows) != 1 {
		t.Fatalf("rows %d, want 1", len(rows))
	}
	if rows[0].Close != 10 {
		t.Errorf("close should stay daily 10, got %v", rows[0].Close)
	}
	if rows[0].TurnoverRate != 1.5 {
		t.Errorf("turnover_rate %v, want 1.5", rows[0].TurnoverRate)
	}
	if !math.IsNaN(rows[0].PE) {
		t.Errorf("missing daily_basic field should be NaN, got %v", rows[0].PE)
	}
}

func TestSyncDailyParquetTwoDayThenSplit(t *testing.T) {
	var mu sync.Mutex
	var dailyCalls []map[string]any
	big := make([][]any, 6001)
	for i := range big {
		big[i] = []any{"000001.SZ", "20250102", 1, 1, 1, 1, 1, 1, 1}
	}
	srv := mockTushare(t, func(api string, params map[string]any) ([]string, [][]any) {
		switch api {
		case "trade_cal":
			return []string{"exchange", "cal_date", "is_open"}, [][]any{
				{"SSE", "20250102", "1"},
				{"SSE", "20250103", "1"},
				{"SSE", "20250106", "1"},
			}
		case "daily":
			mu.Lock()
			cp := make(map[string]any, len(params))
			for k, v := range params {
				cp[k] = v
			}
			dailyCalls = append(dailyCalls, cp)
			mu.Unlock()
			start, _ := params["start_date"].(string)
			end, _ := params["end_date"].(string)
			if start != "" && end != "" && start != end {
				return []string{"ts_code", "trade_date", "open", "high", "low", "close", "pre_close", "vol", "amount"}, big
			}
			td, _ := params["trade_date"].(string)
			return []string{"ts_code", "trade_date", "open", "high", "low", "close", "pre_close", "vol", "amount"}, [][]any{
				{"000001.SZ", td, 10, 11, 9, 10.5, 10, 100, 1000},
			}
		case "daily_basic":
			td, _ := params["trade_date"].(string)
			if td == "" {
				td, _ = params["start_date"].(string)
			}
			return []string{"ts_code", "trade_date", "turnover_rate"}, [][]any{
				{"000001.SZ", td, 1.5},
			}
		default:
			return nil, nil
		}
	})
	defer srv.Close()

	dir := t.TempDir()
	src := &TushareSource{
		Client:  testClient(t, srv),
		DataDir: dir,
		End:     "20250106",
	}
	if err := src.syncDailyParquet(context.Background(), src.Client); err != nil {
		t.Fatalf("sync: %v", err)
	}

	mu.Lock()
	first := append([]map[string]any(nil), dailyCalls...)
	mu.Unlock()
	if len(first) < 3 {
		t.Fatalf("daily calls %d, want at least 2-day then two 1-day: %+v", len(first), first)
	}
	if first[0]["start_date"] != "20250102" || first[0]["end_date"] != "20250103" {
		t.Errorf("first call should be two-day window, got %+v", first[0])
	}
	if first[0]["ts_code"] != nil && first[0]["ts_code"] != "" {
		t.Errorf("full-market daily must not pass ts_code, got %+v", first[0])
	}
	sawDay1, sawDay2 := false, false
	for _, c := range first[1:] {
		if c["trade_date"] == "20250102" {
			sawDay1 = true
		}
		if c["trade_date"] == "20250103" {
			sawDay2 = true
		}
	}
	if !sawDay1 || !sawDay2 {
		t.Errorf("after 6001-row two-day window, should retry each day, got %+v", first)
	}

	path := filepath.Join(dir, "parquet", "daily", "2025-2029.parquet")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("missing shard %s: %v", path, err)
	}
	rows, err := ReadDailyShard(path)
	if err != nil {
		t.Fatalf("read shard: %v", err)
	}
	if len(rows) > 10 {
		t.Fatalf("shard kept truncated two-day dump? %d rows", len(rows))
	}
	if len(rows) < 2 {
		t.Fatalf("shard rows %d, want at least the split days", len(rows))
	}

	mu.Lock()
	dailyCalls = nil
	mu.Unlock()
	if err := src.syncDailyParquet(context.Background(), src.Client); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	mu.Lock()
	n := len(dailyCalls)
	mu.Unlock()
	if n != 0 {
		t.Errorf("incremental sync should skip existing dates, daily calls=%d", n)
	}
	tab, err := loadAsOf(dir)
	if err != nil {
		t.Fatal(err)
	}
	if tab[dsDaily] != "20250106" {
		t.Errorf("as_of daily=%q, want last synced 20250106", tab[dsDaily])
	}
}

func TestParquetDatesEmptyDir(t *testing.T) {
	got, err := parquetDates(t.TempDir(), "20000101", "20250101")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("empty dir should have no dates, got %v", got)
	}
}

func TestParquetDatesSkipsShardsBeforeWindow(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "2000-2004.parquet"), []byte("not parquet"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "2020-2024.parquet")
	if err := WriteDailyShard(path, []DailyParquetRow{
		{TsCode: "000001.SZ", TradeDate: "20200102", Close: 9},
		{TsCode: "000001.SZ", TradeDate: "20211129", Close: 10},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := parquetDates(dir, "20211128", "20261128")
	if err != nil {
		t.Fatalf("不应打开窗口外的旧分片: %v", err)
	}
	if !got["20211129"] {
		t.Errorf("missing in-window date, got %v", got)
	}
	if got["20200102"] {
		t.Errorf("date before window should be ignored, got %v", got)
	}
}

func TestSyncDailyMissingScansOnlyCalWindow(t *testing.T) {
	dir := t.TempDir()
	dailyDir := filepath.Join(dir, "parquet", "daily")
	if err := os.MkdirAll(dailyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dailyDir, "2000-2004.parquet"), []byte("not parquet"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteDailyShard(filepath.Join(dailyDir, "2020-2024.parquet"), []DailyParquetRow{
		{TsCode: "000001.SZ", TradeDate: "20211129", Close: 10},
	}); err != nil {
		t.Fatal(err)
	}

	var dailyCalls int
	srv := mockTushare(t, func(api string, params map[string]any) ([]string, [][]any) {
		if api == "daily" || api == "daily_basic" {
			dailyCalls++
		}
		return nil, nil
	})
	defer srv.Close()

	src := &TushareSource{Client: testClient(t, srv), DataDir: dir, End: "20211129"}
	if err := src.syncDailyMissing(context.Background(), src.Client, []string{"20211129"}); err != nil {
		t.Fatalf("面板窗口外的旧分片不应参与覆盖扫描: %v", err)
	}
	if dailyCalls != 0 {
		t.Errorf("窗口内日期已在分片中，不应拉 daily，calls=%d", dailyCalls)
	}
}

func TestSyncDailyMissingUsesAsOfTable(t *testing.T) {
	dir := t.TempDir()
	dailyDir := filepath.Join(dir, "parquet", "daily")
	if err := os.MkdirAll(dailyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dailyDir, "2025-2029.parquet"), []byte("not parquet"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := advanceAsOf(dir, dsDaily, "20250103"); err != nil {
		t.Fatal(err)
	}

	var dailyCalls int
	srv := mockTushare(t, func(api string, params map[string]any) ([]string, [][]any) {
		if api == "daily" || api == "daily_basic" {
			dailyCalls++
		}
		return nil, nil
	})
	defer srv.Close()

	src := &TushareSource{Client: testClient(t, srv), DataDir: dir, End: "20250103"}
	if err := src.syncDailyMissing(context.Background(), src.Client, []string{"20250102", "20250103"}); err != nil {
		t.Fatalf("as_of 水位命中时不应读分片或拉接口: %v", err)
	}
	if dailyCalls != 0 {
		t.Errorf("as_of 已到最新不应再打 daily，calls=%d", dailyCalls)
	}
}

func TestSyncDailyMissingUsesCoveredIndex(t *testing.T) {
	dir := t.TempDir()
	dailyDir := filepath.Join(dir, "parquet", "daily")
	if err := os.MkdirAll(dailyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dailyDir, coveredFileName), []byte("20250102\n20250103\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 坏文件：如果还走整片 ReadDailyShard 扫日期，这里会直接失败。
	if err := os.WriteFile(filepath.Join(dailyDir, "2025-2029.parquet"), []byte("not parquet"), 0o644); err != nil {
		t.Fatal(err)
	}

	var dailyCalls int
	srv := mockTushare(t, func(api string, params map[string]any) ([]string, [][]any) {
		if api == "daily" || api == "daily_basic" {
			dailyCalls++
		}
		return nil, nil
	})
	defer srv.Close()

	src := &TushareSource{Client: testClient(t, srv), DataDir: dir, End: "20250103"}
	if err := src.syncDailyMissing(context.Background(), src.Client, []string{"20250102", "20250103"}); err != nil {
		t.Fatalf("covered 索引命中时不应读分片或拉接口: %v", err)
	}
	if dailyCalls != 0 {
		t.Errorf("covered 已齐不应再打 daily，calls=%d", dailyCalls)
	}
}
