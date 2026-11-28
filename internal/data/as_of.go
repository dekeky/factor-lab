package data

import (
	"bufio"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

// as_of 存在 data/meta.db：dataset → 已同步到的 YYYYMMDD。
// 增量只拉 cursor 之后的交易日/报告期；首次没有行时从 covered.txt、旧 as_of.tsv 或分片日期列迁移。

const (
	asOfDBFile  = "meta.db"
	asOfTSVFile = "as_of.tsv"
)

const (
	dsDaily   = "daily"
	dsAdj     = "adj_factor"
	dsFina    = "fina_indicator"
	dsST      = "stock_st"
	dsSuspend = "suspend_d"
	dsLimit   = "stk_limit"
)

const asOfSchema = `
CREATE TABLE IF NOT EXISTS as_of (
	dataset TEXT PRIMARY KEY,
	as_of TEXT NOT NULL,
	updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
`

func asOfDBPath(dataDir string) string {
	return filepath.Join(dataDir, asOfDBFile)
}

var asOfMu sync.Mutex

func datesAfter(cal []string, cursor string) []string {
	cursor = compactDate(cursor)
	if cursor == "" {
		return cal
	}
	out := make([]string, 0, len(cal))
	for _, d := range cal {
		d = compactDate(d)
		if d > cursor {
			out = append(out, d)
		}
	}
	return out
}

func maxYYYYMMDD(dates []string) string {
	max := ""
	for _, d := range dates {
		d = compactDate(d)
		if len(d) == 8 && d > max {
			max = d
		}
	}
	return max
}

func maxFromSet(have map[string]bool) string {
	max := ""
	for d, ok := range have {
		if !ok {
			continue
		}
		d = compactDate(d)
		if len(d) == 8 && d > max {
			max = d
		}
	}
	return max
}

func withAsOfDB(dataDir string, fn func(*sql.DB) error) error {
	asOfMu.Lock()
	defer asOfMu.Unlock()
	if dataDir == "" {
		return fmt.Errorf("未配置 DataDir，无法打开 as_of 库")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	path := asOfDBPath(dataDir)
	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
		_ = db.Close()
	}()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(asOfSchema); err != nil {
		return fmt.Errorf("创建 as_of 表失败: %w", err)
	}
	if err := migrateAsOfTSV(dataDir, db); err != nil {
		return err
	}
	return fn(db)
}

func loadAsOf(dataDir string) (map[string]string, error) {
	out := map[string]string{}
	if dataDir == "" {
		return out, nil
	}
	err := withAsOfDB(dataDir, func(db *sql.DB) error {
		rows, err := db.Query(`SELECT dataset, as_of FROM as_of`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var ds, asOf string
			if err := rows.Scan(&ds, &asOf); err != nil {
				return err
			}
			asOf = compactDate(asOf)
			if ds != "" && len(asOf) == 8 {
				out[ds] = asOf
			}
		}
		return rows.Err()
	})
	return out, err
}

func advanceAsOf(dataDir, dataset, asOf string) error {
	asOf = compactDate(asOf)
	if dataDir == "" || dataset == "" || len(asOf) != 8 {
		return nil
	}
	return withAsOfDB(dataDir, func(db *sql.DB) error {
		_, err := db.Exec(`
			INSERT INTO as_of(dataset, as_of, updated_at) VALUES(?, ?, datetime('now'))
			ON CONFLICT(dataset) DO UPDATE SET
				as_of = excluded.as_of,
				updated_at = excluded.updated_at
			WHERE excluded.as_of > as_of.as_of
		`, dataset, asOf)
		return err
	})
}

func resolveAsOf(dataDir, dataset, coveredDir string, scan func() (map[string]bool, error)) (string, error) {
	tab, err := loadAsOf(dataDir)
	if err != nil {
		return "", err
	}
	if c := compactDate(tab[dataset]); c != "" {
		return c, nil
	}
	if coveredDir != "" {
		have, err := loadCoveredDates(coveredDir)
		if err != nil {
			return "", err
		}
		if c := maxFromSet(have); c != "" {
			if err := advanceAsOf(dataDir, dataset, c); err != nil {
				return "", err
			}
			slog.Info("as_of 从 covered.txt 迁移", "dataset", dataset, "as_of", c)
			return c, nil
		}
	}
	if scan != nil {
		have, err := scan()
		if err != nil {
			return "", err
		}
		if c := maxFromSet(have); c != "" {
			if err := advanceAsOf(dataDir, dataset, c); err != nil {
				return "", err
			}
			slog.Info("as_of 从 parquet 日期列迁移", "dataset", dataset, "as_of", c)
			return c, nil
		}
	}
	return "", nil
}

func migrateAsOfTSV(dataDir string, db *sql.DB) error {
	path := filepath.Join(dataDir, "parquet", asOfTSVFile)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ds, asOf, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		ds = strings.TrimSpace(ds)
		asOf = compactDate(asOf)
		if ds == "" || ds == "dataset" || len(asOf) != 8 {
			continue
		}
		if _, err := db.Exec(`
			INSERT INTO as_of(dataset, as_of, updated_at) VALUES(?, ?, datetime('now'))
			ON CONFLICT(dataset) DO UPDATE SET
				as_of = excluded.as_of,
				updated_at = excluded.updated_at
			WHERE excluded.as_of > as_of.as_of
		`, ds, asOf); err != nil {
			return err
		}
		n++
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if n > 0 {
		slog.Info("as_of 从 tsv 迁入 sqlite", "rows", n, "db", asOfDBPath(dataDir))
		_ = os.Remove(path)
	}
	return nil
}
