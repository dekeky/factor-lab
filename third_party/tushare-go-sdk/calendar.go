package tushare

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// TradingCalendar 是某个交易所一段区间内的交易日历，支持按日期查询与前后交易日推算。
type TradingCalendar struct {
	// Exchange 交易所代码，空字符串表示上交所（tushare 默认）。
	Exchange string
	// Days 是区间内的交易日，升序排列。
	Days []string

	open map[string]bool
}

// TradingCalendar 拉取交易日历（trade_cal）。
//
// 仅保留交易日（is_open=1），日期格式为 YYYYMMDD；开始/结束日期留空表示不限。
//
//	cal, err := cli.TradingCalendar(ctx, "SSE", "20240101", "20241231")
//	cal.IsOpen("20240102") // true
func (c *Client) TradingCalendar(ctx context.Context, exchange, startDate, endDate string) (*TradingCalendar, error) {
	params := Params{"is_open": "1"}
	if strings.TrimSpace(exchange) != "" {
		params["exchange"] = strings.TrimSpace(exchange)
	}
	if s := strings.TrimSpace(startDate); s != "" {
		params["start_date"] = strings.ReplaceAll(s, "-", "")
	}
	if e := strings.TrimSpace(endDate); e != "" {
		params["end_date"] = strings.ReplaceAll(e, "-", "")
	}

	tbl, err := c.Query(ctx, "trade_cal", params)
	if err != nil {
		return nil, err
	}
	dateIdx, ok := tbl.FieldIndex("cal_date")
	if !ok {
		return nil, &APIError{Kind: KindDecode, API: "trade_cal", Msg: "返回结果缺少 cal_date 列"}
	}
	openIdx, hasOpen := tbl.FieldIndex("is_open")

	cal := &TradingCalendar{
		Exchange: exchange,
		Days:     make([]string, 0, tbl.Len()),
		open:     make(map[string]bool, tbl.Len()),
	}
	for _, row := range tbl.Items {
		d := toString(cellAt(row, dateIdx))
		if d == "" {
			continue
		}
		if hasOpen {
			if isOpen, err := toInt(cellAt(row, openIdx)); err == nil && isOpen != 1 {
				continue
			}
		}
		cal.Days = append(cal.Days, d)
		cal.open[d] = true
	}
	sort.Strings(cal.Days)
	return cal, nil
}

// Len 返回交易日数量。
func (tc *TradingCalendar) Len() int {
	if tc == nil {
		return 0
	}
	return len(tc.Days)
}

// IsOpen 判断某日是否为交易日（区间外的日期返回 false）。
func (tc *TradingCalendar) IsOpen(date string) bool {
	if tc == nil {
		return false
	}
	return tc.open[strings.ReplaceAll(strings.TrimSpace(date), "-", "")]
}

// NextOpen 返回严格大于 date 的下一个交易日。
func (tc *TradingCalendar) NextOpen(date string) (string, bool) {
	if tc == nil {
		return "", false
	}
	d := strings.ReplaceAll(strings.TrimSpace(date), "-", "")
	i := sort.SearchStrings(tc.Days, d)
	if i < len(tc.Days) && tc.Days[i] == d {
		i++
	}
	if i < len(tc.Days) {
		return tc.Days[i], true
	}
	return "", false
}

// PrevOpen 返回严格小于 date 的上一个交易日。
func (tc *TradingCalendar) PrevOpen(date string) (string, bool) {
	if tc == nil {
		return "", false
	}
	d := strings.ReplaceAll(strings.TrimSpace(date), "-", "")
	i := sort.SearchStrings(tc.Days, d) - 1
	if i >= 0 && i < len(tc.Days) {
		return tc.Days[i], true
	}
	return "", false
}

// OffsetOpen 返回以 date 为基准偏移 n 个交易日的日期（n 可为负；date 非交易日时按插入位置计算）。
func (tc *TradingCalendar) OffsetOpen(date string, n int) (string, bool) {
	if tc == nil {
		return "", false
	}
	d := strings.ReplaceAll(strings.TrimSpace(date), "-", "")
	i := sort.SearchStrings(tc.Days, d)
	if i < len(tc.Days) && tc.Days[i] == d {
		i += n
	} else if n > 0 {
		i += n - 1
	} else {
		i += n
	}
	if i >= 0 && i < len(tc.Days) {
		return tc.Days[i], true
	}
	return "", false
}

// LatestTradeDate 返回 endDate（含）之前最近的交易日。
func (c *Client) LatestTradeDate(ctx context.Context, exchange, endDate string) (string, error) {
	if strings.TrimSpace(endDate) == "" {
		return "", fmt.Errorf("tushare: endDate 不能为空")
	}
	cal, err := c.TradingCalendar(ctx, exchange, "", endDate)
	if err != nil {
		return "", err
	}
	d := strings.ReplaceAll(endDate, "-", "")
	if cal.IsOpen(d) {
		return d, nil
	}
	if prev, ok := cal.PrevOpen(d); ok {
		return prev, nil
	}
	return "", ErrNoRows
}
