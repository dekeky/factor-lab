package data

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"factor-lab/internal/panel"
)

// CSVSource 加载长表 CSV：首列为 date（2006-01-02 或 20060102），第二列为 code，
// 其余列必须是数值字段（如 open,high,low,close,vol）。行顺序任意。
type CSVSource struct {
	Path string
}

func (s *CSVSource) Name() string { return "csv:" + s.Path }

func (s *CSVSource) Load(ctx context.Context) (*panel.Panel, error) {
	f, err := os.Open(s.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("读取表头失败: %w", err)
	}
	if len(header) < 3 {
		return nil, fmt.Errorf("CSV 至少需要 3 列：date,code,数值字段...")
	}
	fields := header[2:]

	type row struct {
		d    time.Time
		code string
		vals []float64
	}
	var rows []row
	bad := 0
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(rec) < len(header) {
			bad++
			continue
		}
		d, err := parseDate(rec[0])
		if err != nil {
			bad++
			continue
		}
		vals := make([]float64, len(fields))
		ok := true
		for k, name := range fields {
			v, err := strconv.ParseFloat(rec[2+k], 64)
			if err != nil {
				v = math.NaN()
			}
			if name == "close" && math.IsNaN(v) {
				ok = false
			}
			vals[k] = v
		}
		if !ok {
			bad++
			continue
		}
		rows = append(rows, row{d: d, code: rec[1], vals: vals})
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("CSV 没有有效数据行")
	}
	if bad > 0 {
		fmt.Printf("csv: 跳过 %d 行无效数据\n", bad)
	}

	// 去重排序：date, code
	sort.Slice(rows, func(a, b int) bool {
		if rows[a].d.Equal(rows[b].d) {
			return rows[a].code < rows[b].code
		}
		return rows[a].d.Before(rows[b].d)
	})
	dates := []time.Time{rows[0].d}
	codesMap := map[string]bool{}
	for _, rw := range rows {
		if !rw.d.Equal(dates[len(dates)-1]) {
			dates = append(dates, rw.d)
		}
		codesMap[rw.code] = true
	}
	codes := make([]string, 0, len(codesMap))
	for c := range codesMap {
		codes = append(codes, c)
	}
	sort.Strings(codes)

	p := panel.New(dates, codes)
	dateIdx := make(map[string]int, len(dates))
	for i, d := range dates {
		dateIdx[d.Format("2006-01-02")] = i
	}
	codeIdx := make(map[string]int, len(codes))
	for j, c := range codes {
		codeIdx[c] = j
	}

	cols := make([][]float64, len(fields))
	for k := range cols {
		cols[k] = make([]float64, p.Size())
		for i := range cols[k] {
			cols[k][i] = math.NaN()
		}
	}
	for _, rw := range rows {
		i := dateIdx[rw.d.Format("2006-01-02")]
		j := codeIdx[rw.code]
		for k := range fields {
			cols[k][i*len(codes)+j] = rw.vals[k]
		}
	}
	// 完整日期×股票网格中缺失的行补 NaN（已初始化）
	for k, name := range fields {
		if err := p.SetCol(name, cols[k]); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func parseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{"2006-01-02", "20060102", "2006/01/02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("无法解析日期 %q", s)
}
