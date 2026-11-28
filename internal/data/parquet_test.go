package data

import (
	"math"
	"path/filepath"
	"testing"
)

func TestShardRangeAlignsFrom2000(t *testing.T) {
	cases := []struct {
		year, start, end int
	}{
		{2000, 2000, 2004},
		{2004, 2000, 2004},
		{2005, 2005, 2009},
		{2026, 2025, 2029},
	}
	for _, c := range cases {
		start, end := shardRange(c.year)
		if start != c.start || end != c.end {
			t.Errorf("year %d: got %d-%d, want %d-%d", c.year, start, end, c.start, c.end)
		}
	}
	dir := t.TempDir()
	got := shardPath(dir, 2026)
	want := filepath.Join(dir, "2025-2029.parquet")
	if got != want {
		t.Errorf("shardPath: got %s, want %s", got, want)
	}
}

func TestWriteReadDailyShardRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "2020-2024.parquet")
	in := []DailyParquetRow{
		{
			TsCode: "000001.SZ", TradeDate: "20200102",
			Open: 10, High: 11, Low: 9, Close: 10.5, PreClose: 10, Vol: 100, Amount: 1000,
			TurnoverRate: 1.5, LimitStatus: 1,
		},
		{
			TsCode: "600000.SH", TradeDate: "20200102",
			Open: 20, High: 21, Low: 19, Close: 20.5, PreClose: 20, Vol: 200, Amount: 2000,
			TurnoverRate: math.NaN(), LimitStatus: math.NaN(),
		},
	}
	if err := WriteDailyShard(path, in); err != nil {
		t.Fatalf("WriteDailyShard: %v", err)
	}
	out, err := ReadDailyShard(path)
	if err != nil {
		t.Fatalf("ReadDailyShard: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("rows %d, want 2", len(out))
	}
	if out[0].TsCode != "000001.SZ" || out[0].Close != 10.5 || out[0].TurnoverRate != 1.5 {
		t.Errorf("row0 mismatch: %+v", out[0])
	}
	if out[1].TsCode != "600000.SH" || out[1].Close != 20.5 {
		t.Errorf("row1 mismatch: %+v", out[1])
	}
	if !math.IsNaN(out[1].TurnoverRate) {
		t.Errorf("missing daily_basic should roundtrip as NaN, got %v", out[1].TurnoverRate)
	}
}
