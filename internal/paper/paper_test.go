package paper

import (
	"math"
	"testing"
	"time"

	"factor-lab/internal/panel"
)

func mkPanel(t *testing.T, T, N int, cols map[string][]float64) *panel.Panel {
	t.Helper()
	dates := make([]time.Time, T)
	for i := range dates {
		dates[i] = time.Date(2024, 1, 1+i, 0, 0, 0, 0, time.UTC)
	}
	codes := make([]string, N)
	for j := range codes {
		codes[j] = string(rune('A' + j))
	}
	p := panel.New(dates, codes)
	for name, vals := range cols {
		if len(vals) != T*N {
			t.Fatalf("列 %s 长度 %d != %d", name, len(vals), T*N)
		}
		col := make(panel.Col, T*N)
		copy(col, vals)
		if err := p.SetCol(name, col); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func fill(T, N int, v float64) []float64 {
	out := make([]float64, T*N)
	for i := range out {
		out[i] = v
	}
	return out
}

func put(col []float64, N, i, j int, v float64) {
	col[i*N+j] = v
}

func newAcc(topN int, capital, cost float64) *Account {
	return &Account{
		Name:   "t",
		Expr:   "alpha = close",
		Config: Config{TopN: topN, Capital: capital, Cost: cost},
	}
}

func TestPicksTopNEqualWeightAtNextClose(t *testing.T) {
	// 4 日 × 3 股。因子分始终 A > B > C；Top 1 应每天选 A，T+1 收盘成交。
	const T, N = 4, 3
	close := fill(T, N, 10)
	put(close, N, 1, 0, 11) // A 在 fill 日（day1）涨到 11
	put(close, N, 2, 0, 12)
	put(close, N, 3, 0, 13)
	score := fill(T, N, 1)
	put(score, N, 0, 0, 3)
	put(score, N, 0, 1, 2)
	put(score, N, 0, 2, 1)
	put(score, N, 1, 0, 9)
	put(score, N, 1, 1, 8)
	put(score, N, 1, 2, 7)
	put(score, N, 2, 0, 5)
	put(score, N, 2, 1, 4)
	put(score, N, 2, 2, 3)
	p := mkPanel(t, T, N, map[string][]float64{
		"close": close, "raw_close": append([]float64(nil), close...), "vol": fill(T, N, 1000),
		"suspend": fill(T, N, math.NaN()), "up_limit": fill(T, N, math.NaN()), "down_limit": fill(T, N, math.NaN()),
	})
	acc := newAcc(1, 10000, 0)
	if err := Replay(p, score, acc); err != nil {
		t.Fatal(err)
	}
	if len(acc.Days) != 3 {
		t.Fatalf("成交日数 %d，期望 3（最后一日无 T+1）", len(acc.Days))
	}
	d0 := acc.Days[0]
	if d0.Date != "2024-01-01" || d0.FillDate != "2024-01-02" {
		t.Fatalf("首日信号/成交日期错误: %+v", d0)
	}
	if len(d0.Picks) != 1 || d0.Picks[0].Code != "A" {
		t.Fatalf("首日应选 A，实际 %+v", d0.Picks)
	}
	if len(d0.Holdings) != 1 || d0.Holdings[0].Code != "A" {
		t.Fatalf("首日持仓应为 A，实际 %+v", d0.Holdings)
	}
	// 10000 在 day1 以 11 买 9 手（900 股），余现金 100，净值仍是 10000
	if math.Abs(d0.NAV-10000) > 1e-6 {
		t.Errorf("首日成交净值 %v，期望 10000", d0.NAV)
	}
	d1 := acc.Days[1]
	want := 900*12 + 100.0
	if math.Abs(d1.NAV-want) > 1e-6 {
		t.Errorf("第二日净值 %v，期望 %v（900 股按 T+1 收盘计价 + 余现金）", d1.NAV, want)
	}
}

func TestSuspendedStockExcludedFromPicks(t *testing.T) {
	const T, N = 3, 2
	close := fill(T, N, 10)
	score := fill(T, N, 1)
	put(score, N, 0, 0, 10) // A 分最高但停牌
	put(score, N, 0, 1, 5)
	put(score, N, 1, 0, 10)
	put(score, N, 1, 1, 5)
	suspend := fill(T, N, math.NaN())
	put(suspend, N, 0, 0, 1)
	put(suspend, N, 1, 0, 1)
	p := mkPanel(t, T, N, map[string][]float64{
		"close": close, "raw_close": append([]float64(nil), close...), "vol": fill(T, N, 1000),
		"suspend": suspend, "up_limit": fill(T, N, math.NaN()), "down_limit": fill(T, N, math.NaN()),
	})
	acc := newAcc(1, 10000, 0)
	if err := Replay(p, score, acc); err != nil {
		t.Fatal(err)
	}
	if acc.Days[0].Picks[0].Code != "B" {
		t.Fatalf("停牌 A 应从股票池剔除，实际选了 %+v", acc.Days[0].Picks)
	}
}

func TestCannotSellSuspendedHolding(t *testing.T) {
	// day0 选 A 并在 day1 买进；day1 改选 B，但 A 在成交日 day2 停牌，卖不掉。
	const T, N = 3, 2
	close := fill(T, N, 10)
	score := fill(T, N, 1)
	put(score, N, 0, 0, 10)
	put(score, N, 0, 1, 1)
	put(score, N, 1, 0, 1)
	put(score, N, 1, 1, 10)
	suspend := fill(T, N, math.NaN())
	put(suspend, N, 2, 0, 1) // A 在 fill 日停牌
	p := mkPanel(t, T, N, map[string][]float64{
		"close": close, "raw_close": append([]float64(nil), close...), "vol": fill(T, N, 1000),
		"suspend": suspend, "up_limit": fill(T, N, math.NaN()), "down_limit": fill(T, N, math.NaN()),
	})
	acc := newAcc(1, 10000, 0)
	if err := Replay(p, score, acc); err != nil {
		t.Fatal(err)
	}
	d1 := acc.Days[1]
	heldA := false
	for _, h := range d1.Holdings {
		if h.Code == "A" && h.Shares > 0 {
			heldA = true
		}
	}
	if !heldA {
		t.Fatalf("停牌持仓应继续持有 A，实际 %+v 成交 %+v", d1.Holdings, d1.Trades)
	}
	for _, tr := range d1.Trades {
		if tr.Code == "A" && tr.Side == "sell" && tr.Status == "filled" {
			t.Fatalf("停牌日不应卖出 A: %+v", tr)
		}
	}
}

func TestLimitUpCannotBuy(t *testing.T) {
	const T, N = 3, 2
	close := fill(T, N, 10)
	raw := fill(T, N, 10)
	up := fill(T, N, math.NaN())
	put(up, N, 1, 0, 10) // A 在成交日涨停
	put(raw, N, 1, 0, 10)
	score := fill(T, N, 1)
	put(score, N, 0, 0, 10)
	put(score, N, 0, 1, 1)
	p := mkPanel(t, T, N, map[string][]float64{
		"close": close, "raw_close": raw, "vol": fill(T, N, 1000),
		"suspend": fill(T, N, math.NaN()), "up_limit": up, "down_limit": fill(T, N, math.NaN()),
	})
	acc := newAcc(1, 10000, 0)
	if err := Replay(p, score, acc); err != nil {
		t.Fatal(err)
	}
	d0 := acc.Days[0]
	if len(d0.Holdings) != 0 {
		t.Fatalf("涨停不能买入，持仓应为空，实际 %+v", d0.Holdings)
	}
	if math.Abs(d0.Cash-10000) > 1e-6 {
		t.Errorf("未成交应保留现金，实际 cash=%v", d0.Cash)
	}
	bought := false
	for _, tr := range d0.Trades {
		if tr.Code == "A" && tr.Side == "buy" && tr.Status == "filled" {
			bought = true
		}
	}
	if bought {
		t.Fatal("涨停日不应买入 A")
	}
}

func TestLimitDownCannotSell(t *testing.T) {
	const T, N = 3, 2
	close := fill(T, N, 10)
	raw := fill(T, N, 10)
	down := fill(T, N, math.NaN())
	put(down, N, 2, 0, 10) // A 在第二笔成交日跌停
	put(raw, N, 2, 0, 10)
	score := fill(T, N, 1)
	put(score, N, 0, 0, 10)
	put(score, N, 0, 1, 1)
	put(score, N, 1, 0, 1)
	put(score, N, 1, 1, 10)
	p := mkPanel(t, T, N, map[string][]float64{
		"close": close, "raw_close": raw, "vol": fill(T, N, 1000),
		"suspend": fill(T, N, math.NaN()), "up_limit": fill(T, N, math.NaN()), "down_limit": down,
	})
	acc := newAcc(1, 10000, 0)
	if err := Replay(p, score, acc); err != nil {
		t.Fatal(err)
	}
	d1 := acc.Days[1]
	heldA := false
	for _, h := range d1.Holdings {
		if h.Code == "A" && h.Shares > 0 {
			heldA = true
		}
	}
	if !heldA {
		t.Fatalf("跌停持仓应卖不掉，实际 %+v", d1.Holdings)
	}
}

func TestReplayIsAppendOnly(t *testing.T) {
	const T, N = 4, 2
	close := fill(T, N, 10)
	score := fill(T, N, 2)
	put(score, N, 0, 0, 3)
	p := mkPanel(t, T, N, map[string][]float64{
		"close": close, "raw_close": append([]float64(nil), close...), "vol": fill(T, N, 1000),
		"suspend": fill(T, N, math.NaN()), "up_limit": fill(T, N, math.NaN()), "down_limit": fill(T, N, math.NaN()),
	})
	acc := newAcc(1, 10000, 0)
	if err := Replay(p, score, acc); err != nil {
		t.Fatal(err)
	}
	n := len(acc.Days)
	firstNAV := acc.Days[0].NAV
	if err := Replay(p, score, acc); err != nil {
		t.Fatal(err)
	}
	if len(acc.Days) != n {
		t.Fatalf("重复 Replay 不应改写已发生账本：%d → %d", n, len(acc.Days))
	}
	if acc.Days[0].NAV != firstNAV {
		t.Fatalf("已发生净值被改写：%v → %v", firstNAV, acc.Days[0].NAV)
	}
}

func TestCostDragOnFirstBuy(t *testing.T) {
	const T, N = 3, 1
	p := mkPanel(t, T, N, map[string][]float64{
		"close": fill(T, N, 10), "raw_close": fill(T, N, 10), "vol": fill(T, N, 1000),
		"suspend": fill(T, N, math.NaN()), "up_limit": fill(T, N, math.NaN()), "down_limit": fill(T, N, math.NaN()),
	})
	score := fill(T, N, 1)
	acc := newAcc(1, 10000, 0.003)
	if err := Replay(p, score, acc); err != nil {
		t.Fatal(err)
	}
	h := acc.Days[0].Holdings[0]
	if int(h.Shares)%100 != 0 {
		t.Fatalf("扣费后仍须整手买入，实际 %v", h.Shares)
	}
	if acc.Days[0].NAV >= 10000 {
		t.Errorf("扣费后净值 %v 应小于本金", acc.Days[0].NAV)
	}
}

func TestPendingPicksOnLastPanelDay(t *testing.T) {
	const T, N = 3, 2
	score := fill(T, N, 1)
	put(score, N, 2, 0, 9)
	put(score, N, 2, 1, 1)
	p := mkPanel(t, T, N, map[string][]float64{
		"close": fill(T, N, 10), "raw_close": fill(T, N, 10), "vol": fill(T, N, 1000),
		"suspend": fill(T, N, math.NaN()), "up_limit": fill(T, N, math.NaN()), "down_limit": fill(T, N, math.NaN()),
	})
	acc := newAcc(1, 10000, 0)
	if err := Replay(p, score, acc); err != nil {
		t.Fatal(err)
	}
	if acc.Pending == nil || acc.Pending.Date != "2024-01-03" {
		t.Fatalf("最后一日应给出待成交选股，实际 %+v", acc.Pending)
	}
	if len(acc.Pending.Items) != 1 || acc.Pending.Items[0].Code != "A" {
		t.Fatalf("待成交应选 A，实际 %+v", acc.Pending)
	}
}

func TestBuysInLotsOf100(t *testing.T) {
	const T, N = 3, 1
	p := mkPanel(t, T, N, map[string][]float64{
		"close": fill(T, N, 11), "raw_close": fill(T, N, 11), "vol": fill(T, N, 1000),
		"suspend": fill(T, N, math.NaN()), "up_limit": fill(T, N, math.NaN()), "down_limit": fill(T, N, math.NaN()),
	})
	acc := newAcc(1, 10000, 0)
	if err := Replay(p, fill(T, N, 1), acc); err != nil {
		t.Fatal(err)
	}
	h := acc.Days[0].Holdings[0]
	if int(h.Shares)%100 != 0 {
		t.Fatalf("买入股数必须是 100 的整数倍，实际 %v", h.Shares)
	}
	if h.Shares != 900 {
		t.Fatalf("10000/11 应买 9 手 = 900 股，实际 %v", h.Shares)
	}
}

func TestPositionCostAndUnrealizedPnL(t *testing.T) {
	const T, N = 4, 1
	close := fill(T, N, 10)
	put(close, N, 1, 0, 11)
	put(close, N, 2, 0, 12)
	put(close, N, 3, 0, 12)
	p := mkPanel(t, T, N, map[string][]float64{
		"close": close, "raw_close": append([]float64(nil), close...), "vol": fill(T, N, 1000),
		"suspend": fill(T, N, math.NaN()), "up_limit": fill(T, N, math.NaN()), "down_limit": fill(T, N, math.NaN()),
	})
	acc := newAcc(1, 10000, 0)
	if err := Replay(p, fill(T, N, 1), acc); err != nil {
		t.Fatal(err)
	}
	d0 := acc.Days[0].Holdings[0]
	if math.Abs(d0.Cost-11) > 1e-9 {
		t.Errorf("买入价应为 11，实际 %v", d0.Cost)
	}
	d1 := acc.Days[1].Holdings[0]
	if math.Abs(d1.Price-12) > 1e-9 {
		t.Errorf("现价应为 12，实际 %v", d1.Price)
	}
	if math.Abs(d1.Cost-11) > 1e-9 {
		t.Errorf("买入价应保持 11，实际 %v", d1.Cost)
	}
	want := (12 - 11) * d1.Shares
	if math.Abs(d1.PnL-want) > 1e-6 {
		t.Errorf("累计盈利应为 %v，实际 %v", want, d1.PnL)
	}
}

func TestBufferKeepsHoldingUntilRankExits(t *testing.T) {
	// Top 1、阈值 1：A 从第 1 落到第 2 仍持有，落到第 3 才换成 B。
	const T, N = 4, 3
	close := fill(T, N, 10)
	score := fill(T, N, 1)
	// day0: A > B > C
	put(score, N, 0, 0, 3)
	put(score, N, 0, 1, 2)
	put(score, N, 0, 2, 1)
	// day1: B > A > C，A 仍在 Top1+1 内
	put(score, N, 1, 0, 2)
	put(score, N, 1, 1, 3)
	put(score, N, 1, 2, 1)
	// day2: B > C > A，A 跌出阈值
	put(score, N, 2, 0, 1)
	put(score, N, 2, 1, 3)
	put(score, N, 2, 2, 2)
	p := mkPanel(t, T, N, map[string][]float64{
		"close": close, "raw_close": append([]float64(nil), close...), "vol": fill(T, N, 1000),
		"suspend": fill(T, N, math.NaN()), "up_limit": fill(T, N, math.NaN()), "down_limit": fill(T, N, math.NaN()),
	})
	acc := newAcc(1, 10000, 0)
	acc.Config.Buffer = 1
	if err := Replay(p, score, acc); err != nil {
		t.Fatal(err)
	}
	if acc.Days[0].Holdings[0].Code != "A" {
		t.Fatalf("首日应买 A，实际 %+v", acc.Days[0].Holdings)
	}
	if acc.Days[1].Holdings[0].Code != "A" {
		t.Fatalf("A 仍排第 2，阈值内不应换股，实际 %+v 成交 %+v", acc.Days[1].Holdings, acc.Days[1].Trades)
	}
	for _, tr := range acc.Days[1].Trades {
		if tr.Status == "filled" {
			t.Fatalf("阈值内不应成交换股: %+v", tr)
		}
	}
	heldB := false
	for _, h := range acc.Days[2].Holdings {
		if h.Code == "B" && h.Shares > 0 {
			heldB = true
		}
	}
	if !heldB {
		t.Fatalf("A 跌出阈值后应换成 B，实际 %+v 成交 %+v", acc.Days[2].Holdings, acc.Days[2].Trades)
	}
	for _, h := range acc.Days[2].Holdings {
		if h.Code == "A" && h.Shares > 0 {
			t.Fatalf("A 已跌出阈值，不应继续持有: %+v", acc.Days[2].Holdings)
		}
	}
}

func TestBufferZeroReplacesOnceOutOfTopN(t *testing.T) {
	const T, N = 3, 2
	close := fill(T, N, 10)
	score := fill(T, N, 1)
	put(score, N, 0, 0, 10)
	put(score, N, 0, 1, 1)
	put(score, N, 1, 0, 1)
	put(score, N, 1, 1, 10)
	p := mkPanel(t, T, N, map[string][]float64{
		"close": close, "raw_close": append([]float64(nil), close...), "vol": fill(T, N, 1000),
		"suspend": fill(T, N, math.NaN()), "up_limit": fill(T, N, math.NaN()), "down_limit": fill(T, N, math.NaN()),
	})
	acc := newAcc(1, 10000, 0)
	if err := Replay(p, score, acc); err != nil {
		t.Fatal(err)
	}
	if acc.Days[0].Holdings[0].Code != "A" {
		t.Fatalf("首日应买 A，实际 %+v", acc.Days[0].Holdings)
	}
	heldB := false
	for _, h := range acc.Days[1].Holdings {
		if h.Code == "B" {
			heldB = true
		}
	}
	if !heldB {
		t.Fatalf("阈值为 0 时跌出 Top 1 应换成 B，实际 %+v", acc.Days[1].Holdings)
	}
}

func TestFillsAtNextOpenNotClose(t *testing.T) {
	const T, N = 3, 1
	open := fill(T, N, 10)
	close := fill(T, N, 10)
	put(open, N, 1, 0, 8)  // 成交日开盘 8
	put(close, N, 1, 0, 11) // 成交日收盘 11，不应作为成交价
	p := mkPanel(t, T, N, map[string][]float64{
		"open": open, "close": close, "raw_close": append([]float64(nil), close...), "vol": fill(T, N, 1000),
		"suspend": fill(T, N, math.NaN()), "up_limit": fill(T, N, math.NaN()), "down_limit": fill(T, N, math.NaN()),
	})
	acc := newAcc(1, 8000, 0)
	if err := Replay(p, fill(T, N, 1), acc); err != nil {
		t.Fatal(err)
	}
	if len(acc.Days[0].Trades) == 0 {
		t.Fatal("首日应有买入成交")
	}
	tr := acc.Days[0].Trades[0]
	if tr.Side != "buy" || math.Abs(tr.Price-8) > 1e-9 {
		t.Fatalf("应按 T+1 开盘价 8 成交，实际 %+v", tr)
	}
	h := acc.Days[0].Holdings[0]
	if math.Abs(h.Cost-8) > 1e-9 {
		t.Errorf("买入成本应为开盘价 8，实际 %v", h.Cost)
	}
	if math.Abs(h.Price-11) > 1e-9 {
		t.Errorf("收盘后现价应为 11，实际 %v", h.Price)
	}
}

func TestPrefersQfqOpen(t *testing.T) {
	const T, N = 3, 1
	p := mkPanel(t, T, N, map[string][]float64{
		"open": fill(T, N, 5), "qfq_open": fill(T, N, 9), "close": fill(T, N, 20),
		"qfq_close": fill(T, N, 10), "raw_close": fill(T, N, 10), "vol": fill(T, N, 1000),
		"suspend": fill(T, N, math.NaN()), "up_limit": fill(T, N, math.NaN()), "down_limit": fill(T, N, math.NaN()),
	})
	put(p.MustCol("qfq_open"), N, 1, 0, 9)
	acc := newAcc(1, 9000, 0)
	if err := Replay(p, fill(T, N, 1), acc); err != nil {
		t.Fatal(err)
	}
	tr := acc.Days[0].Trades[0]
	if math.Abs(tr.Price-9) > 1e-9 {
		t.Fatalf("应以前复权开盘价 9 成交，实际 %v", tr.Price)
	}
	if math.Abs(acc.Days[0].Holdings[0].Price-10) > 1e-9 {
		t.Errorf("收盘应按前复权收盘 10 计价，实际 %v", acc.Days[0].Holdings[0].Price)
	}
}

func TestNewDaysFillAtOpenWithoutRewritingOldDays(t *testing.T) {
	const T, N = 4, 1
	open := fill(T, N, 10)
	close := fill(T, N, 10)
	put(open, N, 2, 0, 7)
	put(close, N, 2, 0, 15)
	p := mkPanel(t, T, N, map[string][]float64{
		"open": open, "close": close, "raw_close": append([]float64(nil), close...), "vol": fill(T, N, 1000),
		"suspend": fill(T, N, math.NaN()), "up_limit": fill(T, N, math.NaN()), "down_limit": fill(T, N, math.NaN()),
	})
	acc := newAcc(1, 7000, 0)
	acc.Cash = 7000
	acc.Days = []Day{{
		Date: "2024-01-01", FillDate: "2024-01-02",
		NAV: 12345, Cash: 7000,
	}}
	if err := Replay(p, fill(T, N, 1), acc); err != nil {
		t.Fatal(err)
	}
	if math.Abs(acc.Days[0].NAV-12345) > 1e-9 {
		t.Fatalf("已入账日子不应被改写: NAV %v", acc.Days[0].NAV)
	}
	if len(acc.Days) < 2 {
		t.Fatalf("应追加新成交日，实际 %d 天", len(acc.Days))
	}
	filled := false
	for _, tr := range acc.Days[1].Trades {
		if tr.Status == "filled" && tr.Side == "buy" {
			filled = true
			if math.Abs(tr.Price-7) > 1e-9 {
				t.Fatalf("新增量应按开盘价 7 成交，实际 %v", tr.Price)
			}
		}
	}
	if !filled {
		t.Fatalf("增量日应有买入，实际 %+v", acc.Days[1].Trades)
	}
}

func TestLimitUpAtOpenCannotBuy(t *testing.T) {
	const T, N = 3, 1
	open := fill(T, N, 10)
	close := fill(T, N, 9)
	rawOpen := fill(T, N, 10)
	rawClose := fill(T, N, 9)
	up := fill(T, N, math.NaN())
	put(up, N, 1, 0, 10)
	p := mkPanel(t, T, N, map[string][]float64{
		"open": open, "close": close, "raw_open": rawOpen, "raw_close": rawClose, "vol": fill(T, N, 1000),
		"suspend": fill(T, N, math.NaN()), "up_limit": up, "down_limit": fill(T, N, math.NaN()),
	})
	acc := newAcc(1, 10000, 0)
	if err := Replay(p, fill(T, N, 1), acc); err != nil {
		t.Fatal(err)
	}
	for _, tr := range acc.Days[0].Trades {
		if tr.Side == "buy" && tr.Status == "filled" {
			t.Fatalf("开盘涨停不能买入: %+v", tr)
		}
	}
	if len(acc.Days[0].Holdings) != 0 {
		t.Fatalf("开盘涨停应空仓，实际 %+v", acc.Days[0].Holdings)
	}
}

func TestLabelUsesNameAndCode(t *testing.T) {
	const T, N = 3, 2
	p := mkPanel(t, T, N, map[string][]float64{
		"close": fill(T, N, 10), "raw_close": fill(T, N, 10), "vol": fill(T, N, 1000),
		"suspend": fill(T, N, math.NaN()), "up_limit": fill(T, N, math.NaN()), "down_limit": fill(T, N, math.NaN()),
	})
	p.Names = []string{"苹果", "香蕉"}
	score := fill(T, N, 1)
	put(score, N, 0, 0, 9)
	put(score, N, 0, 1, 1)
	acc := newAcc(1, 10000, 0)
	if err := Replay(p, score, acc); err != nil {
		t.Fatal(err)
	}
	pk := acc.Days[0].Picks[0]
	if pk.Label != "苹果（A）" {
		t.Fatalf("选股应显示 名称（代码），实际 %q", pk.Label)
	}
	h := acc.Days[0].Holdings[0]
	if h.Label != "苹果（A）" {
		t.Fatalf("持仓应显示 名称（代码），实际 %q", h.Label)
	}
}

func TestBriefKeepsNavCurve(t *testing.T) {
	a := Account{
		Name:      "x",
		Positions: []Position{{Code: "A", Shares: 100}},
		Days: []Day{
			{Date: "2024-01-02", FillDate: "2024-01-03", NAV: 1000, Picks: []Pick{{Code: "A"}}, Holdings: []Position{{Code: "A"}}},
			{Date: "2024-01-03", FillDate: "2024-01-04", NAV: 1100, Ret: 0.1, Trades: []Trade{{Code: "A"}}},
		},
		Pending: &Picks{Date: "2024-01-04", Items: []Pick{{Code: "A"}}},
	}
	b := a.Brief()
	if len(b.Days) != 2 {
		t.Fatalf("days %d", len(b.Days))
	}
	if len(b.Days[0].Picks) != 0 || len(b.Days[0].Holdings) != 0 || len(b.Days[1].Trades) != 0 {
		t.Fatalf("brief 仍带明细: %+v", b.Days)
	}
	if b.Days[1].NAV != 1100 || b.Days[1].Ret != 0.1 {
		t.Fatalf("净值点丢失: %+v", b.Days[1])
	}
	if b.Positions != nil {
		t.Fatal("列表不应带持仓")
	}
	if b.Pending == nil || b.Pending.Date != "2024-01-04" {
		t.Fatal("待成交应保留")
	}
	if len(a.Days[0].Picks) == 0 || a.Positions == nil {
		t.Fatal("原账户不应被改写")
	}
}
