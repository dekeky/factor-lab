package data

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestDatesAfterCursor(t *testing.T) {
	cal := []string{"20250102", "20250103", "20250106"}
	got := datesAfter(cal, "20250102")
	if len(got) != 2 || got[0] != "20250103" || got[1] != "20250106" {
		t.Fatalf("got %v, want (20250103, 20250106)", got)
	}
	if got := datesAfter(cal, "20250106"); len(got) != 0 {
		t.Fatalf("at cursor should be empty, got %v", got)
	}
	if got := datesAfter(cal, ""); len(got) != 3 {
		t.Fatalf("empty cursor should keep all, got %v", got)
	}
}

func TestAsOfTableRoundtripAndAdvance(t *testing.T) {
	dir := t.TempDir()
	if err := advanceAsOf(dir, dsDaily, "20250102"); err != nil {
		t.Fatal(err)
	}
	if err := advanceAsOf(dir, dsAdj, "20250103"); err != nil {
		t.Fatal(err)
	}
	if err := advanceAsOf(dir, dsDaily, "20250102"); err != nil {
		t.Fatal(err)
	}
	tab, err := loadAsOf(dir)
	if err != nil {
		t.Fatal(err)
	}
	if tab[dsDaily] != "20250102" {
		t.Errorf("daily as_of %q, want 20250102", tab[dsDaily])
	}
	if tab[dsAdj] != "20250103" {
		t.Errorf("adj as_of %q, want 20250103", tab[dsAdj])
	}
	if err := advanceAsOf(dir, dsDaily, "20250106"); err != nil {
		t.Fatal(err)
	}
	tab, err = loadAsOf(dir)
	if err != nil {
		t.Fatal(err)
	}
	if tab[dsDaily] != "20250106" {
		t.Errorf("advanced daily as_of %q, want 20250106", tab[dsDaily])
	}
	db, err := sql.Open("sqlite", asOfDBPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM as_of`).Scan(&n); err != nil {
		t.Fatalf("as_of 应是 sqlite 表: %v", err)
	}
	if n != 2 {
		t.Errorf("as_of rows %d, want 2", n)
	}
}

func TestAsOfMigratesTSVIntoSQLite(t *testing.T) {
	dir := t.TempDir()
	pq := filepath.Join(dir, "parquet")
	if err := os.MkdirAll(pq, 0o755); err != nil {
		t.Fatal(err)
	}
	tsv := "dataset\tas_of\n" + dsDaily + "\t20250106\n"
	if err := os.WriteFile(filepath.Join(pq, asOfTSVFile), []byte(tsv), 0o644); err != nil {
		t.Fatal(err)
	}
	tab, err := loadAsOf(dir)
	if err != nil {
		t.Fatal(err)
	}
	if tab[dsDaily] != "20250106" {
		t.Errorf("tsv migrate daily=%q, want 20250106", tab[dsDaily])
	}
}

func TestResolveAsOfPrefersTableOverCovered(t *testing.T) {
	dir := t.TempDir()
	dailyDir := filepath.Join(dir, "parquet", "daily")
	if err := os.MkdirAll(dailyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dailyDir, coveredFileName), []byte("20250106\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := advanceAsOf(dir, dsDaily, "20250102"); err != nil {
		t.Fatal(err)
	}
	got, err := resolveAsOf(dir, dsDaily, dailyDir, func() (map[string]bool, error) {
		t.Fatal("as_of 表已有水位，不应扫 parquet")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "20250102" {
		t.Errorf("got %q, want table cursor 20250102", got)
	}
}

func TestResolveAsOfMigratesCoveredMax(t *testing.T) {
	dir := t.TempDir()
	dailyDir := filepath.Join(dir, "parquet", "daily")
	if err := os.MkdirAll(dailyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dailyDir, coveredFileName), []byte("20250102\n20250106\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := resolveAsOf(dir, dsDaily, dailyDir, func() (map[string]bool, error) {
		t.Fatal("covered.txt 可迁移时不应扫 parquet")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "20250106" {
		t.Errorf("got %q, want covered max 20250106", got)
	}
	tab, err := loadAsOf(dir)
	if err != nil {
		t.Fatal(err)
	}
	if tab[dsDaily] != "20250106" {
		t.Errorf("migration did not persist, as_of=%q", tab[dsDaily])
	}
}
