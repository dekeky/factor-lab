package matching

import (
	"fmt"
	"math"
	"strings"
	"time"

	"factor-lab/internal/panel"
	"factor-lab/internal/paper/account"
	"factor-lab/internal/paper/ledger"
)

func Execute(p *panel.Panel, plan Plan, book ledger.Book, cfg account.Config, execAt string) (fills []ledger.Fill, intents []Intent, next ledger.Book, snap ledger.Snapshot, err error) {
	if p == nil {
		return nil, nil, ledger.Book{}, ledger.Snapshot{}, fmt.Errorf("面板为空")
	}
	day, err := execDate(execAt)
	if err != nil {
		return nil, nil, ledger.Book{}, ledger.Snapshot{}, err
	}
	idx := dateIndex(p, day)
	if idx < 0 {
		return nil, nil, ledger.Book{}, ledger.Snapshot{}, fmt.Errorf("成交日 %s 不在日期轴上", day)
	}

	intents = cloneIntents(plan.Intents)
	pos := map[string]float64{}
	for _, h := range book.Positions {
		pos[h.Code] = h.Shares
	}

	for i := range intents {
		sh := intents[i].SellShares
		if sh <= 0 {
			continue
		}
		code := intents[i].Code
		have := pos[code]
		want := sh
		if have < want {
			want = have
		}
		f := tradeFill(p, idx, execAt, plan, intents[i], "sell", want, cfg)
		fills = append(fills, f)
		if f.Status == "filled" {
			intents[i].ActualSellShares = f.Shares
			pos[code] = have - f.Shares
		}
	}

	cashBook, err := ledger.Apply(book, fills)
	if err != nil {
		return nil, nil, ledger.Book{}, ledger.Snapshot{}, err
	}

	for i := range intents {
		sh := intents[i].BuyShares
		if sh <= 0 {
			continue
		}
		f := buyFill(p, idx, execAt, plan, intents[i], sh, cashBook.Cash, cfg)
		fills = append(fills, f)
		if f.Status == "filled" {
			intents[i].ActualBuyShares = f.Shares
			cashBook, err = ledger.Apply(cashBook, []ledger.Fill{f})
			if err != nil {
				return nil, nil, ledger.Book{}, ledger.Snapshot{}, err
			}
		}
	}

	next, err = ledger.Apply(book, fills)
	if err != nil {
		return nil, nil, ledger.Book{}, ledger.Snapshot{}, err
	}
	snap = snapshotOf(p, idx, next, day)
	return fills, intents, next, snap, nil
}

func buyFill(p *panel.Panel, i int, execAt string, plan Plan, in Intent, shares, cash float64, cfg account.Config) ledger.Fill {
	j := codeIndex(p, in.Code)
	if j < 0 {
		return skipFill(execAt, plan, in, "buy", "skipped_untradeable")
	}
	if why := skipBuyReason(p, i, j); why != "" {
		return skipFill(execAt, plan, in, "buy", why)
	}
	raw, slipPx, ok := buyPrice(p, i, j, cfg)
	if !ok {
		return skipFill(execAt, plan, in, "buy", "skipped_untradeable")
	}
	need := func(n float64) float64 { return n * slipPx * (1 + cfg.Cost) }
	for shares >= LotSize && cash+1e-9 < need(shares) {
		shares -= LotSize
	}
	if shares < LotSize {
		return skipFill(execAt, plan, in, "buy", "skipped_untradeable")
	}
	amt := shares * slipPx
	return ledger.Fill{
		AccountID: plan.AccountID, PlanID: plan.ID, Date: execAt, Code: in.Code,
		Side: "buy", Shares: shares, Price: slipPx, Amount: amt,
		Fee: amt * cfg.Cost, Slippage: shares * raw * cfg.SlippageBps / 1e4,
		Status: "filled",
	}
}

func tradeFill(p *panel.Panel, i int, execAt string, plan Plan, in Intent, side string, shares float64, cfg account.Config) ledger.Fill {
	j := codeIndex(p, in.Code)
	if j < 0 || shares <= 0 {
		return skipFill(execAt, plan, in, side, "skipped_untradeable")
	}
	if why := skipSellReason(p, i, j); why != "" {
		return skipFill(execAt, plan, in, side, why)
	}
	raw, slipPx, ok := sellPrice(p, i, j, cfg)
	if !ok {
		return skipFill(execAt, plan, in, side, "skipped_untradeable")
	}
	amt := shares * slipPx
	return ledger.Fill{
		AccountID: plan.AccountID, PlanID: plan.ID, Date: execAt, Code: in.Code,
		Side: side, Shares: shares, Price: slipPx, Amount: amt,
		Fee: amt * cfg.Cost, Slippage: shares * raw * cfg.SlippageBps / 1e4,
		Status: "filled",
	}
}

func skipFill(execAt string, plan Plan, in Intent, side, status string) ledger.Fill {
	return ledger.Fill{
		AccountID: plan.AccountID, PlanID: plan.ID, Date: execAt, Code: in.Code,
		Side: side, Status: status,
	}
}

func skipBuyReason(p *panel.Panel, i, j int) string {
	if isSuspend(p, i, j) {
		return "skipped_suspend"
	}
	if openLimitUp(p, i, j) {
		return "skipped_limit_up"
	}
	if !volOK(p, i, j) {
		return "skipped_untradeable"
	}
	return ""
}

func skipSellReason(p *panel.Panel, i, j int) string {
	if isSuspend(p, i, j) {
		return "skipped_suspend"
	}
	if openLimitDown(p, i, j) {
		return "skipped_limit_down"
	}
	if !volOK(p, i, j) {
		return "skipped_untradeable"
	}
	return ""
}

func buyPrice(p *panel.Panel, i, j int, cfg account.Config) (raw, slipped float64, ok bool) {
	raw, ok = openPx(p, i, j)
	if !ok {
		return 0, 0, false
	}
	return raw, raw * (1 + cfg.SlippageBps/1e4), true
}

func sellPrice(p *panel.Panel, i, j int, cfg account.Config) (raw, slipped float64, ok bool) {
	raw, ok = openPx(p, i, j)
	if !ok {
		return 0, 0, false
	}
	return raw, raw * (1 - cfg.SlippageBps/1e4), true
}

func openPx(p *panel.Panel, i, j int) (float64, bool) {
	if px, ok := at(p, "qfq_open", i, j); ok {
		return px, true
	}
	return at(p, "open", i, j)
}

func markPx(p *panel.Panel, i, j int) (float64, bool) {
	if px, ok := at(p, "qfq_close", i, j); ok {
		return px, true
	}
	return at(p, "close", i, j)
}

func isSuspend(p *panel.Panel, i, j int) bool {
	if v, ok := atAllowZero(p, "suspend", i, j); ok && v == 1 {
		return true
	}
	if v, ok := atAllowZero(p, "vol", i, j); ok && v == 0 {
		return true
	}
	return false
}

func openLimitUp(p *panel.Panel, i, j int) bool {
	return openLimitHit(p, i, j, "up_limit", true)
}

func openLimitDown(p *panel.Panel, i, j int) bool {
	return openLimitHit(p, i, j, "down_limit", false)
}

func openLimitHit(p *panel.Panel, i, j int, bound string, up bool) bool {
	raw, ok1 := at(p, "raw_open", i, j)
	if !ok1 {
		raw, ok1 = at(p, "raw_close", i, j)
	}
	lim, ok2 := at(p, bound, i, j)
	if !ok1 || !ok2 || lim <= 0 {
		return false
	}
	if up {
		return raw+1e-6 >= lim-0.01
	}
	return raw-1e-6 <= lim+0.01
}

func volOK(p *panel.Panel, i, j int) bool {
	v, ok := atAllowZero(p, "vol", i, j)
	if !ok {
		return true
	}
	return v > 0
}

func atAllowZero(p *panel.Panel, name string, i, j int) (float64, bool) {
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

func execDate(execAt string) (string, error) {
	s := strings.TrimSpace(execAt)
	if len(s) < 10 {
		return "", fmt.Errorf("exec_at 为空或格式错误")
	}
	day := s[:10]
	if _, err := time.Parse("2006-01-02", day); err != nil {
		return "", fmt.Errorf("exec_at 不是 YYYY-MM-DD HH:MM:SS")
	}
	if len(s) != 19 {
		return "", fmt.Errorf("exec_at 不是 YYYY-MM-DD HH:MM:SS")
	}
	if _, err := time.Parse("2006-01-02 15:04:05", s); err != nil {
		return "", fmt.Errorf("exec_at 不是 YYYY-MM-DD HH:MM:SS")
	}
	return day, nil
}

func cloneIntents(in []Intent) []Intent {
	out := make([]Intent, len(in))
	copy(out, in)
	return out
}

func snapshotOf(p *panel.Panel, i int, book ledger.Book, day string) ledger.Snapshot {
	hold := cloneHoldings(book.Positions)
	nav := book.Cash
	for k := range hold {
		j := codeIndex(p, hold[k].Code)
		if j >= 0 {
			if px, ok := markPx(p, i, j); ok {
				hold[k].Price = px
				hold[k].Value = hold[k].Shares * px
			}
		}
		nav += hold[k].Value
		hold[k].PnL = (hold[k].Price - hold[k].Cost) * hold[k].Shares
	}
	if nav > 0 {
		for k := range hold {
			hold[k].Weight = hold[k].Value / nav
		}
	}
	return ledger.Snapshot{
		AccountID: book.AccountID, Date: day, Cash: book.Cash, NAV: nav, Holdings: hold,
	}
}

func cloneHoldings(in []ledger.Position) []ledger.Position {
	if len(in) == 0 {
		return nil
	}
	out := make([]ledger.Position, len(in))
	copy(out, in)
	return out
}
