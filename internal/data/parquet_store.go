package data

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/parquet-go/parquet-go"
)

func datasetParquetDir(dataDir, dataset string) string {
	return filepath.Join(dataDir, "parquet", dataset)
}

func endOrToday(end string) string {
	if end == "" {
		return time.Now().Format("20060102")
	}
	return compactDate(end)
}

func writeParquetFile[T any](path string, rows []T) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := parquet.WriteFile(tmp, rows); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(path)
		if err2 := os.Rename(tmp, path); err2 != nil {
			_ = os.Remove(tmp)
			return err2
		}
	}
	return nil
}

func readParquetFile[T any](path string) ([]T, error) {
	return parquet.ReadFile[T](path)
}

func readParquetIfExists[T any](path string) ([]T, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return readParquetFile[T](path)
}

const coveredFileName = "covered.txt"

func loadCoveredDates(dir string) (map[string]bool, error) {
	out := map[string]bool{}
	f, err := os.Open(filepath.Join(dir, coveredFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		d := compactDate(strings.TrimSpace(sc.Text()))
		if len(d) == 8 {
			out[d] = true
		}
	}
	return out, sc.Err()
}

func markCovered(dir string, dates []string) error {
	if len(dates) == 0 {
		return nil
	}
	have, err := loadCoveredDates(dir)
	if err != nil {
		return err
	}
	for _, d := range dates {
		d = compactDate(d)
		if len(d) == 8 {
			have[d] = true
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	keys := make([]string, 0, len(have))
	for d := range have {
		keys = append(keys, d)
	}
	sort.Strings(keys)
	tmp := filepath.Join(dir, coveredFileName+".tmp")
	if err := os.WriteFile(tmp, []byte(strings.Join(keys, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	dst := filepath.Join(dir, coveredFileName)
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(dst)
		if err2 := os.Rename(tmp, dst); err2 != nil {
			_ = os.Remove(tmp)
			return err2
		}
	}
	return nil
}

func parquetDateSet[T any](dir, from, to string, dateOf func(T) string) (map[string]bool, error) {
	out := map[string]bool{}
	y0, y1 := parquetEpochYear, parquetEpochYear
	if len(from) >= 4 {
		fmt.Sscanf(from[:4], "%d", &y0)
	}
	if len(to) >= 4 {
		fmt.Sscanf(to[:4], "%d", &y1)
	} else {
		y1 = time.Now().Year()
	}
	if y0 < parquetEpochYear {
		y0 = parquetEpochYear
	}
	seen := map[string]bool{}
	for y := y0; y <= y1; y++ {
		path := shardPath(dir, y)
		if seen[path] {
			continue
		}
		seen[path] = true
		t0 := time.Now()
		rows, err := readParquetIfExists[T](path)
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			slog.Info("扫描 parquet 分片日期", "file", filepath.Base(path), "rows", len(rows),
				"elapsed", time.Since(t0).Round(time.Millisecond).String())
		}
		for _, r := range rows {
			d := compactDate(dateOf(r))
			if (from == "" || d >= from) && (to == "" || d <= to) {
				out[d] = true
			}
		}
	}
	return out, nil
}

func presentDates[T any](dir, from, to string, dateOf func(T) string) (map[string]bool, error) {
	have, err := loadCoveredDates(dir)
	if err != nil {
		return nil, err
	}
	from, to = compactDate(from), compactDate(to)
	if len(have) == 0 {
		return parquetDateSet(dir, from, to, dateOf)
	}
	if from == "" && to == "" {
		return have, nil
	}
	out := map[string]bool{}
	for d := range have {
		if (from == "" || d >= from) && (to == "" || d <= to) {
			out[d] = true
		}
	}
	return out, nil
}

func missingFromCal(cal []string, have map[string]bool) []string {
	var missing []string
	for _, d := range cal {
		if !have[d] {
			missing = append(missing, d)
		}
	}
	return missing
}

func clipCal(cal []string, notBefore string) []string {
	notBefore = compactDate(notBefore)
	if notBefore == "" {
		return cal
	}
	out := make([]string, 0, len(cal))
	for _, d := range cal {
		if d >= notBefore {
			out = append(out, d)
		}
	}
	return out
}

func mergeByKey[T any](old, neu []T, key func(T) string) []T {
	m := make(map[string]T, len(old)+len(neu))
	order := make([]string, 0, len(old)+len(neu))
	add := func(r T) {
		k := key(r)
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
	out := make([]T, len(order))
	for i, k := range order {
		out[i] = m[k]
	}
	return out
}

func readDataset[T any](dir, start, end string, dateOf func(T) string, keep func(T) bool) ([]T, error) {
	start, end = compactDate(start), compactDate(end)
	y0, y1 := parquetEpochYear, time.Now().Year()
	if len(start) >= 4 {
		fmt.Sscanf(start[:4], "%d", &y0)
	}
	if len(end) >= 4 {
		fmt.Sscanf(end[:4], "%d", &y1)
	}
	if y0 < parquetEpochYear {
		y0 = parquetEpochYear
	}
	var out []T
	seen := map[string]bool{}
	for y := y0; y <= y1; y++ {
		path := shardPath(dir, y)
		if seen[path] {
			continue
		}
		seen[path] = true
		rows, err := readParquetIfExists[T](path)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			d := compactDate(dateOf(r))
			if start != "" && d < start {
				continue
			}
			if end != "" && d > end {
				continue
			}
			if keep != nil && !keep(r) {
				continue
			}
			out = append(out, r)
		}
	}
	return out, nil
}

func shardChanged(prevYear, year int) bool {
	if prevYear == 0 {
		return false
	}
	ps, pe := shardRange(prevYear)
	ys, ye := shardRange(year)
	return ps != ys || pe != ye
}

func codeDateKey(code, date string) string {
	return code + "|" + compactDate(date)
}
