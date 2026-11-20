// Package paper 实现因子纸上账户：逐日打分、Top N 等权调仓、T+1 开盘成交。
//
// 口径：
//   - 信号日 t 用 t 及以前的因子值排序；t+1 前复权开盘成交（无前复权列时退回 open）
//   - 成交后持仓按当日收盘（前复权优先）计价
//   - 停牌 / 涨跌停从当日股票池剔除，不进入新开仓
//   - 已持仓：停牌或开盘跌停卖不掉，继续持有；开盘涨停可以卖、不能买
//   - 调仓阈值 Buffer：持仓排名仍在 TopN+Buffer 内则不换股；空位只从当日 Top N 补
//   - 账本只追加，不回写已发生的交易日
package paper

import (
	"fmt"
	"math"
	"sort"
	"time"

	"factor-lab/internal/panel"
)

type Config struct {
	TopN    int     `json:"top_n"`
	Buffer  int     `json:"buffer"` // 调仓阈值：持仓排名 > TopN+Buffer 才换股；0 = 刚跌出 Top N 就换
	Capital float64 `json:"capital"`
	Cost    float64 `json:"cost"`
	Start   string  `json:"start"`
}

func DefaultConfig() Config {
	return Config{TopN: 10, Capital: 1_000_000, Cost: 0.003}
}

// LotSize 是 A 股最小交易单位（1 手 = 100 股）。
const LotSize = 100.0

type Position struct {
	Code     string  `json:"code"`
	Name     string  `json:"name,omitempty"`
	Label    string  `json:"label"`
	Shares   float64 `json:"shares"`
	Cost     float64 `json:"cost"`  // 买入均价
	Price    float64 `json:"price"` // 现价
	Value    float64 `json:"value"`
	Weight   float64 `json:"weight"`
	PnL      float64 `json:"pnl"` // 累计盈利（浮动+该股已实现）
	Realized float64 `json:"realized,omitempty"`
}

type Pick struct {
	Code   string  `json:"code"`
	Name   string  `json:"name,omitempty"`
	Label  string  `json:"label"`
	Score  float64 `json:"score"`
	Rank   int     `json:"rank"`
	Status string  `json:"status"`
}

type Picks struct {
	Date     string `json:"date"`
	FillDate string `json:"fill_date,omitempty"`
	Items    []Pick `json:"items"`
	Skipped  []Pick `json:"skipped,omitempty"`
}

type Trade struct {
	Date   string  `json:"date"`
	Code   string  `json:"code"`
	Name   string  `json:"name,omitempty"`
	Label  string  `json:"label"`
	Side   string  `json:"side"`
	Shares float64 `json:"shares"`
	Price  float64 `json:"price"`
	Amount float64 `json:"amount"`
	Cost   float64 `json:"cost"`
	Status string  `json:"status"`
}

type Day struct {
	Date     string     `json:"date"`
	FillDate string     `json:"fill_date"`
	NAV      float64    `json:"nav"`
	Cash     float64    `json:"cash"`
	Ret      float64    `json:"ret"`
	Picks    []Pick     `json:"picks"`
	Skipped  []Pick     `json:"skipped,omitempty"`
	Holdings []Position `json:"holdings"`
	Trades   []Trade    `json:"trades"`
}

type Summary struct {
	Days      int       `json:"days"`
	NAV       float64   `json:"nav"`
	Return    float64   `json:"return"`
	MaxDD     float64   `json:"max_dd"`
	LastDate  string    `json:"last_date"`
	LastFill  string    `json:"last_fill"`
	Holdings  int       `json:"holdings"`
	PnL       float64   `json:"pnl"`
	PickCodes []string  `json:"pick_codes,omitempty"`
	SyncedAt  time.Time `json:"synced_at"`
}

type Account struct {
	ID         int       `json:"id"`
	Name       string    `json:"name"`
	FactorID   int       `json:"factor_id,omitempty"`
	FactorName string    `json:"factor_name,omitempty"`
	ComboID    int       `json:"combo_id,omitempty"`
	ComboName  string    `json:"combo_name,omitempty"`
	Expr       string    `json:"expr"`
	Config     Config    `json:"config"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`

	Cash      float64    `json:"cash"`
	Positions []Position `json:"positions"`
	Days      []Day      `json:"days,omitempty"`
	Pending   *Picks     `json:"pending,omitempty"`
	Summary   *Summary   `json:"summary,omitempty"`
}

// Brief 列表用：去掉持仓与逐日明细，只留净值点给卡片曲线。
func (a Account) Brief() Account {
	if n := len(a.Days); n > 0 {
		slim := make([]Day, n)
		for i, d := range a.Days {
			slim[i] = Day{Date: d.Date, FillDate: d.FillDate, NAV: d.NAV, Ret: d.Ret}
		}
		a.Days = slim
	}
	a.Positions = nil
	return a
}

// Replay 从账户已处理过的最后信号日之后，按面板继续逐日记账。已发生的 Days 不回写。
func Replay(p *panel.Panel, factor panel.Col, acc *Account) error {
	if acc == nil {
		return fmt.Errorf("账户为空")
	}
	if p == nil || p.T() < 2 {
		return fmt.Errorf("面板日期不足，无法做 T+1 成交")
	}
	T, N := p.T(), p.N()
	if len(factor) != T*N {
		return fmt.Errorf("因子列长度与面板不匹配")
	}
	cfg := acc.Config
	if cfg.TopN < 1 {
		return fmt.Errorf("Top N 必须 >= 1")
	}
	if cfg.Capital <= 0 {
		cfg.Capital = DefaultConfig().Capital
		acc.Config.Capital = cfg.Capital
	}
	if cfg.Cost < 0 {
		cfg.Cost = 0
		acc.Config.Cost = 0
	}
	if cfg.Buffer < 0 {
		cfg.Buffer = 0
		acc.Config.Buffer = 0
	}

	dates := p.DateLabels()
	start := 0
	if cfg.Start != "" {
		found := false
		for i, d := range dates {
			if d >= cfg.Start {
				start = i
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("起始日 %s 晚于面板最后一天 %s", cfg.Start, dates[T-1])
		}
	}
	if n := len(acc.Days); n > 0 {
		last := acc.Days[n-1].Date
		start = T
		for i, d := range dates {
			if d > last {
				start = i
				break
			}
		}
	} else {
		acc.Cash = cfg.Capital
		acc.Positions = nil
	}

	shares := map[int]float64{}
	lastPx := map[int]float64{}
	avgCost := map[int]float64{}
	realized := map[int]float64{}
	for _, pos := range acc.Positions {
		j := indexOf(p.Codes, pos.Code)
		if j >= 0 {
			shares[j] = pos.Shares
			lastPx[j] = pos.Price
			if pos.Cost > 0 {
				avgCost[j] = pos.Cost
			} else {
				avgCost[j] = pos.Price
			}
			realized[j] = pos.Realized
		}
	}
	prevNAV := acc.Cash
	for _, pos := range acc.Positions {
		prevNAV += pos.Value
	}
	if prevNAV <= 0 {
		prevNAV = cfg.Capital
	}
	for i := start; i < T-1; i++ {
		fill := i + 1
		selected, skipped := pickTop(p, factor, i, cfg.TopN)
		ordered := eligibleOrdered(p, factor, i)
		target := composeTarget(ordered, shares, cfg.TopN, cfg.Buffer)

		var trades []Trade
		for j, sh := range shares {
			if sh == 0 || target[j] {
				continue
			}
			px, ok := tradePrice(p, fill, j, lastPx[j])
			if !ok || !canSell(p, fill, j) {
				trades = append(trades, makeTrade(p, dates[fill], j, "sell", sh, 0, 0, 0, skipReason(p, fill, j, "sell")))
				continue
			}
			amt := sh * px
			fee := amt * cfg.Cost
			acc.Cash += amt - fee
			realized[j] += (px-avgCost[j])*sh - fee
			trades = append(trades, makeTrade(p, dates[fill], j, "sell", sh, px, amt, fee, "filled"))
			delete(shares, j)
			delete(avgCost, j)
			lastPx[j] = px
		}

		nav := acc.Cash
		for j, sh := range shares {
			px, ok := tradePrice(p, fill, j, lastPx[j])
			if ok {
				lastPx[j] = px
			} else {
				px = lastPx[j]
			}
			nav += sh * px
		}

		holdable := make([]int, 0, len(target))
		holdSet := map[int]bool{}
		tjs := make([]int, 0, len(target))
		for j := range target {
			tjs = append(tjs, j)
		}
		sort.Ints(tjs)
		for _, j := range tjs {
			_, held := shares[j]
			px, hasPx := tradePrice(p, fill, j, lastPx[j])
			if hasPx {
				lastPx[j] = px
			}
			if held {
				holdable = append(holdable, j)
				holdSet[j] = true
				continue
			}
			if hasPx && canBuy(p, fill, j) {
				holdable = append(holdable, j)
				holdSet[j] = true
			} else {
				trades = append(trades, makeTrade(p, dates[fill], j, "buy", 0, 0, 0, 0, skipReason(p, fill, j, "buy")))
			}
		}

		if nHold := len(holdable); nHold > 0 {
			leftover := 0.0
			for j, sh := range shares {
				if holdSet[j] {
					continue
				}
				leftover += sh * lastPx[j]
			}
			alloc := nav - leftover
			if alloc < 0 {
				alloc = 0
			}
			each := alloc / float64(nHold)
			for _, j := range holdable {
				px := lastPx[j]
				if px <= 0 {
					continue
				}
				cur := shares[j] * px
				delta := each - cur
				if math.Abs(delta) < 1e-9 {
					continue
				}
				if delta > 0 {
					if !canBuy(p, fill, j) {
						continue
					}
					sh, amount, fee := buyLots(delta, acc.Cash, px, cfg.Cost)
					if sh <= 0 {
						continue
					}
					old := shares[j]
					avgCost[j] = (old*avgCost[j] + amount + fee) / (old + sh)
					acc.Cash -= amount + fee
					shares[j] += sh
					trades = append(trades, makeTrade(p, dates[fill], j, "buy", sh, px, amount, fee, "filled"))
					continue
				}
				if !canSell(p, fill, j) {
					trades = append(trades, makeTrade(p, dates[fill], j, "sell", 0, 0, 0, 0, skipReason(p, fill, j, "sell")))
					continue
				}
				sh, sellAmt := sellLots(shares[j], -delta, px)
				if sh <= 0 {
					continue
				}
				fee := sellAmt * cfg.Cost
				acc.Cash += sellAmt - fee
				realized[j] += (px-avgCost[j])*sh - fee
				shares[j] -= sh
				if shares[j] < 1e-12 {
					delete(shares, j)
					delete(avgCost, j)
				}
				trades = append(trades, makeTrade(p, dates[fill], j, "sell", sh, px, sellAmt, fee, "filled"))
			}
		}

		if math.Abs(acc.Cash) < 1e-6 {
			acc.Cash = 0
		}
		for j, sh := range shares {
			if sh <= 1e-12 {
				continue
			}
			if px, ok := markPx(p, fill, j, lastPx[j]); ok {
				lastPx[j] = px
			}
		}
		holdings := snapshot(p, shares, lastPx, avgCost, realized)
		nav = acc.Cash
		for i := range holdings {
			nav += holdings[i].Value
		}
		if nav > 0 {
			for i := range holdings {
				holdings[i].Weight = holdings[i].Value / nav
			}
		}
		ret := 0.0
		if prevNAV != 0 {
			ret = nav/prevNAV - 1
		}
		acc.Days = append(acc.Days, Day{
			Date: dates[i], FillDate: dates[fill],
			NAV: nav, Cash: acc.Cash, Ret: ret,
			Picks: selected, Skipped: skipped,
			Holdings: holdings, Trades: trades,
		})
		acc.Positions = holdings
		prevNAV = nav
	}

	last := T - 1
	lastSig := ""
	if n := len(acc.Days); n > 0 {
		lastSig = acc.Days[n-1].Date
	}
	if dates[last] != lastSig {
		sel, skip := pickTop(p, factor, last, cfg.TopN)
		acc.Pending = &Picks{Date: dates[last], Items: sel, Skipped: skip}
	} else {
		acc.Pending = nil
	}
	acc.UpdatedAt = time.Now()
	acc.Summary = summarize(acc)
	return nil
}

func pickTop(p *panel.Panel, factor panel.Col, i, topN int) (selected, skipped []Pick) {
	N := p.N()
	type row struct {
		j     int
		score float64
	}
	rows := make([]row, 0, N)
	for j := 0; j < N; j++ {
		s := factor[i*N+j]
		if math.IsNaN(s) || math.IsInf(s, 0) {
			continue
		}
		rows = append(rows, row{j, s})
	}
	sort.Slice(rows, func(a, b int) bool {
		if rows[a].score == rows[b].score {
			return rows[a].j < rows[b].j
		}
		return rows[a].score > rows[b].score
	})
	for _, r := range rows {
		if len(selected) >= topN {
			break
		}
		if why := ineligible(p, i, r.j); why != "" {
			skipped = append(skipped, makePick(p, r.j, r.score, len(skipped)+1, why))
			continue
		}
		selected = append(selected, makePick(p, r.j, r.score, len(selected)+1, "selected"))
	}
	return selected, skipped
}

// eligibleOrdered 返回信号日可交易股票，按因子分从高到低。排名只在这个集合里计。
func eligibleOrdered(p *panel.Panel, factor panel.Col, i int) []int {
	N := p.N()
	type row struct {
		j     int
		score float64
	}
	rows := make([]row, 0, N)
	for j := 0; j < N; j++ {
		s := factor[i*N+j]
		if math.IsNaN(s) || math.IsInf(s, 0) {
			continue
		}
		if ineligible(p, i, j) != "" {
			continue
		}
		rows = append(rows, row{j, s})
	}
	sort.Slice(rows, func(a, b int) bool {
		if rows[a].score == rows[b].score {
			return rows[a].j < rows[b].j
		}
		return rows[a].score > rows[b].score
	})
	out := make([]int, len(rows))
	for k, r := range rows {
		out[k] = r.j
	}
	return out
}

// composeTarget：持仓排名仍在 TopN+Buffer 内则保留；空出的名额只从当日 Top N 补。
func composeTarget(ordered []int, shares map[int]float64, topN, buffer int) map[int]bool {
	if topN < 1 {
		topN = 1
	}
	if buffer < 0 {
		buffer = 0
	}
	exitN := topN + buffer
	rank := make(map[int]int, len(ordered))
	for k, j := range ordered {
		rank[j] = k + 1
	}
	target := map[int]bool{}
	for j, sh := range shares {
		if sh <= 1e-12 {
			continue
		}
		if r, ok := rank[j]; ok && r <= exitN {
			target[j] = true
		}
	}
	for _, j := range ordered {
		if len(target) >= topN {
			break
		}
		if rank[j] > topN {
			break
		}
		target[j] = true
	}
	return target
}

func ineligible(p *panel.Panel, i, j int) string {
	closeCol := p.MustCol("close")
	N := p.N()
	px := closeCol[i*N+j]
	if math.IsNaN(px) || px <= 0 {
		return "skipped_missing"
	}
	if isSuspend(p, i, j) {
		return "skipped_suspend"
	}
	if isLimitUp(p, i, j) {
		return "skipped_limit_up"
	}
	if isLimitDown(p, i, j) {
		return "skipped_limit_down"
	}
	return ""
}

func canBuy(p *panel.Panel, i, j int) bool {
	if ineligible(p, i, j) == "skipped_missing" || isSuspend(p, i, j) || openLimitUp(p, i, j) {
		return false
	}
	return volOK(p, i, j)
}

func canSell(p *panel.Panel, i, j int) bool {
	if ineligible(p, i, j) == "skipped_missing" || isSuspend(p, i, j) || openLimitDown(p, i, j) {
		return false
	}
	return volOK(p, i, j)
}

func skipReason(p *panel.Panel, i, j int, side string) string {
	if isSuspend(p, i, j) {
		return "skipped_suspend"
	}
	if side == "buy" && openLimitUp(p, i, j) {
		return "skipped_limit_up"
	}
	if side == "sell" && openLimitDown(p, i, j) {
		return "skipped_limit_down"
	}
	if ineligible(p, i, j) == "skipped_missing" || !volOK(p, i, j) {
		return "skipped_untradeable"
	}
	return "skipped_untradeable"
}

func isSuspend(p *panel.Panel, i, j int) bool {
	if v, ok := at(p, "suspend", i, j); ok && v == 1 {
		return true
	}
	if v, ok := at(p, "vol", i, j); ok && v == 0 {
		return true
	}
	return false
}

func isLimitUp(p *panel.Panel, i, j int) bool {
	return limitHit(p, i, j, "up_limit", true)
}

func isLimitDown(p *panel.Panel, i, j int) bool {
	return limitHit(p, i, j, "down_limit", false)
}

func openLimitUp(p *panel.Panel, i, j int) bool {
	return openLimitHit(p, i, j, "up_limit", true)
}

func openLimitDown(p *panel.Panel, i, j int) bool {
	return openLimitHit(p, i, j, "down_limit", false)
}

func limitHit(p *panel.Panel, i, j int, bound string, up bool) bool {
	raw, ok1 := at(p, "raw_close", i, j)
	lim, ok2 := at(p, bound, i, j)
	return limitCmp(raw, lim, ok1, ok2, up)
}

func openLimitHit(p *panel.Panel, i, j int, bound string, up bool) bool {
	raw, ok1 := at(p, "raw_open", i, j)
	if !ok1 {
		raw, ok1 = at(p, "raw_close", i, j)
	}
	lim, ok2 := at(p, bound, i, j)
	return limitCmp(raw, lim, ok1, ok2, up)
}

func limitCmp(raw, lim float64, ok1, ok2, up bool) bool {
	if !ok1 || !ok2 || lim <= 0 {
		return false
	}
	if up {
		return raw+1e-6 >= lim-0.01
	}
	return raw-1e-6 <= lim+0.01
}

func tradePrice(p *panel.Panel, i, j int, fallback float64) (float64, bool) {
	if px, ok := at(p, "qfq_open", i, j); ok && px > 0 {
		return px, true
	}
	if px, ok := at(p, "open", i, j); ok && px > 0 {
		return px, true
	}
	return markPx(p, i, j, fallback)
}

func markPx(p *panel.Panel, i, j int, fallback float64) (float64, bool) {
	if px, ok := at(p, "qfq_close", i, j); ok && px > 0 {
		return px, true
	}
	closeCol, ok := p.Col("close")
	if !ok {
		if fallback > 0 {
			return fallback, true
		}
		return 0, false
	}
	return markPrice(closeCol, p.N(), i, j, fallback)
}

func volOK(p *panel.Panel, i, j int) bool {
	v, ok := at(p, "vol", i, j)
	if !ok {
		return true
	}
	return v > 0
}

func at(p *panel.Panel, name string, i, j int) (float64, bool) {
	c, ok := p.Col(name)
	if !ok {
		return 0, false
	}
	v := c[i*p.N()+j]
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

func markPrice(closeCol panel.Col, N, i, j int, fallback float64) (float64, bool) {
	px := closeCol[i*N+j]
	if !math.IsNaN(px) && px > 0 {
		return px, true
	}
	if fallback > 0 {
		return fallback, true
	}
	return 0, false
}

func snapshot(p *panel.Panel, shares, lastPx, avgCost, realized map[int]float64) []Position {
	js := make([]int, 0, len(shares))
	for j, sh := range shares {
		if sh > 1e-12 {
			js = append(js, j)
		}
	}
	sort.Ints(js)
	out := make([]Position, 0, len(js))
	for _, j := range js {
		px := lastPx[j]
		cost := avgCost[j]
		sh := shares[j]
		real := realized[j]
		name, label := stockMeta(p, j)
		out = append(out, Position{
			Code: p.Codes[j], Name: name, Label: label,
			Shares: sh, Cost: cost, Price: px, Value: sh * px,
			PnL: (px-cost)*sh + real, Realized: real,
		})
	}
	return out
}

func buyLots(delta, cash, px, costRate float64) (shares, amount, fee float64) {
	if px <= 0 {
		return 0, 0, 0
	}
	lotNotional := px * LotSize
	want := math.Floor(delta / lotNotional)
	afford := math.Floor(cash / (lotNotional * (1 + costRate)))
	lots := want
	if afford < lots {
		lots = afford
	}
	if lots < 1 {
		return 0, 0, 0
	}
	shares = lots * LotSize
	amount = shares * px
	fee = amount * costRate
	return shares, amount, fee
}

func sellLots(have, sellAmt, px float64) (shares, amount float64) {
	if px <= 0 || have <= 0 {
		return 0, 0
	}
	lots := math.Floor(sellAmt / (px * LotSize))
	maxLots := math.Floor(have / LotSize)
	if lots > maxLots {
		lots = maxLots
	}
	if lots < 1 {
		return 0, 0
	}
	shares = lots * LotSize
	return shares, shares * px
}

func makePick(p *panel.Panel, j int, score float64, rank int, status string) Pick {
	name, label := stockMeta(p, j)
	return Pick{Code: p.Codes[j], Name: name, Label: label, Score: score, Rank: rank, Status: status}
}

func makeTrade(p *panel.Panel, date string, j int, side string, shares, px, amount, fee float64, status string) Trade {
	name, label := stockMeta(p, j)
	return Trade{Date: date, Code: p.Codes[j], Name: name, Label: label, Side: side, Shares: shares, Price: px, Amount: amount, Cost: fee, Status: status}
}

func stockMeta(p *panel.Panel, j int) (name, label string) {
	if p == nil || j < 0 || j >= p.N() {
		return "", ""
	}
	return p.StockName(j), p.Label(j)
}

func summarize(acc *Account) *Summary {
	s := &Summary{SyncedAt: time.Now(), NAV: acc.Cash}
	if n := len(acc.Positions); n > 0 {
		s.Holdings = n
		for _, p := range acc.Positions {
			s.NAV += p.Value
		}
	}
	if n := len(acc.Days); n > 0 {
		s.Days = n
		s.LastDate = acc.Days[n-1].Date
		s.LastFill = acc.Days[n-1].FillDate
		s.NAV = acc.Days[n-1].NAV
		peak, maxDD := acc.Days[0].NAV, 0.0
		for _, d := range acc.Days {
			if d.NAV > peak {
				peak = d.NAV
			}
			if peak > 0 {
				dd := d.NAV/peak - 1
				if dd < maxDD {
					maxDD = dd
				}
			}
		}
		s.MaxDD = maxDD
		for _, pk := range acc.Days[n-1].Picks {
			s.PickCodes = append(s.PickCodes, pk.Code)
		}
	}
	if acc.Pending != nil {
		s.LastDate = acc.Pending.Date
		s.PickCodes = s.PickCodes[:0]
		for _, pk := range acc.Pending.Items {
			s.PickCodes = append(s.PickCodes, pk.Code)
		}
	}
	if acc.Config.Capital > 0 {
		s.Return = s.NAV/acc.Config.Capital - 1
		s.PnL = s.NAV - acc.Config.Capital
	}
	return s
}

func indexOf(codes []string, code string) int {
	for i, c := range codes {
		if c == code {
			return i
		}
	}
	return -1
}
