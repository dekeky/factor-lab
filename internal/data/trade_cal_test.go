package data

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCalendarHorizonExtendsPastToday(t *testing.T) {
	today := time.Now().Format("20060102")
	got := calendarHorizon("")
	if got <= today {
		t.Fatalf("horizon %s want after %s", got, today)
	}
	past := "20200101"
	if h := calendarHorizon(past); h <= today {
		t.Fatalf("past End still needs future calendar, got %s", h)
	}
	future := time.Now().AddDate(2, 0, 0).Format("20060102")
	if h := calendarHorizon(future); h != future {
		t.Fatalf("user End %s got %s", future, h)
	}
}

func TestClipOpenDatesStopsAtThrough(t *testing.T) {
	cal := []string{"20261126", "20261127", "20261130"}
	got := clipOpenDates(cal, "20261127")
	if len(got) != 2 || got[0] != "20261126" || got[1] != "20261127" {
		t.Fatalf("%v", got)
	}
}

func TestWriteReadOpenDates(t *testing.T) {
	dir := t.TempDir()
	if err := writeOpenDates(dir, []string{"20261127", "20261130"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "parquet", "trade_cal", "open.txt")); err != nil {
		t.Fatal(err)
	}
	got, err := ReadOpenDates(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "2026-11-27" || got[1] != "2026-11-30" {
		t.Fatalf("%v", got)
	}
	missing, err := ReadOpenDates(filepath.Join(dir, "nope"))
	if err != nil || len(missing) != 0 {
		t.Fatalf("missing file: %v %v", missing, err)
	}
}
