package data

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const tradeCalFile = "open.txt"

func tradeCalDir(dataDir string) string {
	return datasetParquetDir(dataDir, "trade_cal")
}

// calendarHorizon 交易日历右端：至少覆盖用户 End，并向前看一年（含尚未开盘的交易日）。
func calendarHorizon(end string) string {
	to := endOrToday(end)
	h := time.Now().AddDate(1, 0, 0).Format("20060102")
	if h > to {
		return h
	}
	return to
}

func clipOpenDates(dates []string, through string) []string {
	through = compactDate(through)
	out := make([]string, 0, len(dates))
	for _, d := range dates {
		d = compactDate(d)
		if len(d) == 8 && (through == "" || d <= through) {
			out = append(out, d)
		}
	}
	return out
}

func writeOpenDates(dataDir string, dates []string) error {
	if dataDir == "" {
		return nil
	}
	dir := tradeCalDir(dataDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var b strings.Builder
	for _, d := range dates {
		d = compactDate(d)
		if len(d) != 8 {
			continue
		}
		b.WriteString(d)
		b.WriteByte('\n')
	}
	path := filepath.Join(dir, tradeCalFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
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

// ReadOpenDates 返回已落盘的开市日，YYYY-MM-DD。文件不存在则空切片。
func ReadOpenDates(dataDir string) ([]string, error) {
	if dataDir == "" {
		return nil, nil
	}
	f, err := os.Open(filepath.Join(tradeCalDir(dataDir), tradeCalFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		d := compactDate(strings.TrimSpace(sc.Text()))
		if len(d) == 8 {
			out = append(out, normDate(d))
		}
	}
	return out, sc.Err()
}
