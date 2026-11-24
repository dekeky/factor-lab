package matching

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"factor-lab/internal/panel"
	"factor-lab/internal/paper/account"
	"factor-lab/internal/paper/ledger"
)

const (
	LotSize     = 100.0
	WeightDrift = 0.03

	SideOpen   = "open"   // 开仓
	SideExit   = "exit"   // 清仓
	SideAdd    = "add"    // 加仓
	SideReduce = "reduce" // 减仓
	SideHold   = "hold"   // 不动
)

type Intent struct {
	Code         string  `json:"code"`
	Name         string  `json:"name,omitempty"`
	Label        string  `json:"label,omitempty"`
	Side         string  `json:"side"`
	Shares       float64 `json:"shares"`
	ActualShares float64 `json:"actual_shares"`
	Score        float64 `json:"score"`
	Rank         int     `json:"rank"`
}

func (in Intent) buying() bool {
	return (in.Side == SideOpen || in.Side == SideAdd) && in.Shares > 0
}

func (in Intent) selling() bool {
	return (in.Side == SideExit || in.Side == SideReduce) && in.Shares > 0
}

func (in *Intent) UnmarshalJSON(b []byte) error {
	var w struct {
		Code             string  `json:"code"`
		Name             string  `json:"name"`
		Label            string  `json:"label"`
		Side             string  `json:"side"`
		Shares           float64 `json:"shares"`
		ActualShares     float64 `json:"actual_shares"`
		Score            float64 `json:"score"`
		Rank             int     `json:"rank"`
		BuyShares        float64 `json:"buy_shares"`
		SellShares       float64 `json:"sell_shares"`
		ActualBuyShares  float64 `json:"actual_buy_shares"`
		ActualSellShares float64 `json:"actual_sell_shares"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	in.Code, in.Name, in.Label = w.Code, w.Name, w.Label
	in.Score, in.Rank = w.Score, w.Rank
	in.Shares, in.ActualShares = w.Shares, w.ActualShares
	if in.Shares == 0 {
		if w.BuyShares > 0 {
			in.Shares = w.BuyShares
		} else if w.SellShares > 0 {
			in.Shares = w.SellShares
		}
	}
	if in.ActualShares == 0 {
		in.ActualShares = w.ActualBuyShares + w.ActualSellShares
	}
	in.Side = migrateSide(w.Side, w.BuyShares, w.SellShares)
	return nil
}

func migrateSide(side string, buy, sell float64) string {
	switch side {
	case SideOpen, SideExit, SideAdd, SideReduce:
		return side
	case "buy":
		return SideOpen
	case "sell":
		return SideExit
	case "hold":
		if buy > 0 {
			return SideAdd
		}
		if sell > 0 {
			return SideReduce
		}
		return SideHold
	default:
		return side
	}
}

// Pick 信号日因子排名前 TopN，与调仓 Intent 分开存：阈值缓冲下计划持仓可以不是当天前 N。
type Pick struct {
	Code  string  `json:"code"`
	Name  string  `json:"name,omitempty"`
	Label string  `json:"label,omitempty"`
	Score float64 `json:"score"`
	Rank  int     `json:"rank"`
}

type Plan struct {
	ID         int64    `json:"id,omitempty"`
	AccountID  int      `json:"account_id"`
	SignalDate string   `json:"signal_date"`
	ExecAt     string   `json:"exec_at,omitempty"`
	Intents    []Intent `json:"intents"`
	Picks      []Pick   `json:"picks,omitempty"`
	Status     string   `json:"status"`
}

func Generate(p *panel.Panel, factor panel.Col, acc account.Account, book ledger.Book, signalDate string) (Plan, error) {
	if p == nil || p.T() < 1 {
		return Plan{}, fmt.Errorf("面板为空")
	}
	sigIdx := dateIndex(p, signalDate)
	if sigIdx < 0 {
		return Plan{}, fmt.Errorf("打分日 %s 不在日期轴上", signalDate)
	}
	if len(factor) != p.Size() {
		return Plan{}, fmt.Errorf("因子列长度与面板不匹配")
	}
	cfg := acc.Config
	if cfg.TopN < 1 {
		cfg.TopN = 1
	}
	if cfg.RebalanceThreshold < 0 {
		cfg.RebalanceThreshold = 0
	}

	ranked := rankScores(p, factor, sigIdx)
	rankOf := map[string]int{}
	scoreOf := map[string]float64{}
	for _, r := range ranked {
		rankOf[r.code] = r.rank
		scoreOf[r.code] = r.score
	}

	held := map[string]float64{}
	for _, pos := range book.Positions {
		if pos.Shares > 1e-12 {
			held[pos.Code] = pos.Shares
		}
	}

	exitN := cfg.TopN + cfg.RebalanceThreshold
	var sells []Intent
	keep := map[string]bool{}
	for code, sh := range held {
		rk, ok := rankOf[code]
		if !ok || rk > exitN {
			sells = append(sells, Intent{
				Code: code, Side: SideExit, Shares: sh,
				Score: scoreOrNaN(scoreOf, code), Rank: rk,
			})
			continue
		}
		keep[code] = true
	}

	var enters []string
	if len(held) == 0 {
		for _, r := range ranked {
			if len(enters) >= cfg.TopN {
				break
			}
			enters = append(enters, r.code)
		}
	} else {
		slots := cfg.TopN - len(keep)
		if slots < 0 {
			slots = 0
		}
		for _, r := range ranked {
			if slots == 0 {
				break
			}
			if r.rank > cfg.TopN {
				break
			}
			if keep[r.code] {
				continue
			}
			enters = append(enters, r.code)
			slots--
		}
	}

	nav := book.Cash
	for code, sh := range held {
		px, ok := closePx(p, sigIdx, code)
		if ok {
			nav += sh * px
		}
	}
	nTarget := len(keep) + len(enters)
	each := 0.0
	if nTarget > 0 {
		each = nav / float64(nTarget)
	}

	var intents []Intent
	intents = append(intents, sells...)

	for _, code := range enters {
		in := Intent{
			Code: code, Side: SideOpen,
			Score: scoreOrNaN(scoreOf, code), Rank: rankOf[code],
		}
		in.Shares = buyLots(each, closePxOrZero(p, sigIdx, code), cfg)
		intents = append(intents, in)
	}

	keepCodes := make([]string, 0, len(keep))
	for c := range keep {
		keepCodes = append(keepCodes, c)
	}
	sort.Strings(keepCodes)
	for _, code := range keepCodes {
		cur := held[code]
		px, ok := closePx(p, sigIdx, code)
		in := Intent{
			Code: code, Side: SideHold,
			Score: scoreOrNaN(scoreOf, code), Rank: rankOf[code],
		}
		if !ok || nTarget == 0 {
			intents = append(intents, in)
			continue
		}
		w := 0.0
		if nav > 0 {
			w = cur * px / nav
		}
		targetW := 1 / float64(nTarget)
		targetSh := buyLots(each, px, cfg)
		if math.Abs(w-targetW) <= WeightDrift {
			intents = append(intents, in)
			continue
		}
		if targetSh > cur {
			if sh := lotFloor(targetSh - cur); sh > 0 {
				in.Side = SideAdd
				in.Shares = sh
			}
		} else if targetSh < cur {
			if sh := sellLots(cur-targetSh, cur); sh > 0 {
				in.Side = SideReduce
				in.Shares = sh
			}
		}
		intents = append(intents, in)
	}

	sort.SliceStable(intents, func(i, j int) bool {
		if intents[i].Rank == 0 && intents[j].Rank != 0 {
			return false
		}
		if intents[j].Rank == 0 && intents[i].Rank != 0 {
			return true
		}
		if intents[i].Rank != intents[j].Rank {
			return intents[i].Rank < intents[j].Rank
		}
		return intents[i].Code < intents[j].Code
	})
	attachIntentMeta(p, intents)

	return Plan{
		AccountID:  acc.ID,
		SignalDate: signalDate,
		Intents:    intents,
		Picks:      topPicks(p, ranked, cfg.TopN),
		Status:     "pending",
	}, nil
}

// PicksOnDate 信号日因子排名前 topN，供生成计划与历史回填共用。
func PicksOnDate(p *panel.Panel, factor panel.Col, signalDate string, topN int) ([]Pick, error) {
	if p == nil || p.T() < 1 {
		return nil, fmt.Errorf("面板为空")
	}
	if len(factor) != p.Size() {
		return nil, fmt.Errorf("因子列长度与面板不匹配")
	}
	i := dateIndex(p, signalDate)
	if i < 0 {
		return nil, fmt.Errorf("打分日 %s 不在日期轴上", signalDate)
	}
	if topN < 1 {
		topN = 1
	}
	return topPicks(p, rankScores(p, factor, i), topN), nil
}

func topPicks(p *panel.Panel, ranked []ranked, n int) []Pick {
	if n < 1 || len(ranked) == 0 {
		return []Pick{}
	}
	if n > len(ranked) {
		n = len(ranked)
	}
	out := make([]Pick, n)
	for i := 0; i < n; i++ {
		name, label := stockMeta(p, ranked[i].code)
		out[i] = Pick{
			Code: ranked[i].code, Name: name, Label: label,
			Score: ranked[i].score, Rank: ranked[i].rank,
		}
	}
	return out
}

func attachIntentMeta(p *panel.Panel, intents []Intent) {
	for i := range intents {
		intents[i].Name, intents[i].Label = stockMeta(p, intents[i].Code)
	}
}

func stockMeta(p *panel.Panel, code string) (name, label string) {
	j := codeIndex(p, code)
	if j < 0 {
		return "", code
	}
	return p.StockName(j), p.Label(j)
}

type ranked struct {
	code  string
	score float64
	rank  int
	j     int
}

func rankScores(p *panel.Panel, factor panel.Col, i int) []ranked {
	N := p.N()
	rows := make([]ranked, 0, N)
	for j := 0; j < N; j++ {
		s := factor[i*N+j]
		if math.IsNaN(s) || math.IsInf(s, 0) {
			continue
		}
		rows = append(rows, ranked{code: p.Codes[j], score: s, j: j})
	}
	sort.Slice(rows, func(a, b int) bool {
		if rows[a].score == rows[b].score {
			return rows[a].j < rows[b].j
		}
		return rows[a].score > rows[b].score
	})
	for k := range rows {
		rows[k].rank = k + 1
	}
	return rows
}

func dateIndex(p *panel.Panel, ymd string) int {
	for i, d := range p.Dates {
		if d.Format("2006-01-02") == ymd {
			return i
		}
	}
	return -1
}

func closePx(p *panel.Panel, i int, code string) (float64, bool) {
	j := codeIndex(p, code)
	if j < 0 {
		return 0, false
	}
	if px, ok := at(p, "qfq_close", i, j); ok && px > 0 {
		return px, true
	}
	if px, ok := at(p, "close", i, j); ok && px > 0 {
		return px, true
	}
	return 0, false
}

func closePxOrZero(p *panel.Panel, i int, code string) float64 {
	px, _ := closePx(p, i, code)
	return px
}

func codeIndex(p *panel.Panel, code string) int {
	for j, c := range p.Codes {
		if c == code {
			return j
		}
	}
	return -1
}

func at(p *panel.Panel, name string, i, j int) (float64, bool) {
	c, ok := p.Col(name)
	if !ok {
		return 0, false
	}
	v := c[i*p.N()+j]
	if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return 0, false
	}
	return v, true
}

func buyLots(budget, px float64, cfg account.Config) float64 {
	if px <= 0 || budget <= 0 {
		return 0
	}
	unit := px * (1 + cfg.SlippageBps/1e4) * (1 + cfg.Cost)
	lots := math.Floor(budget / unit / LotSize)
	if lots < 1 {
		return 0
	}
	return lots * LotSize
}

func lotFloor(shares float64) float64 {
	lots := math.Floor(shares / LotSize)
	if lots < 1 {
		return 0
	}
	return lots * LotSize
}

func sellLots(want, have float64) float64 {
	sh := lotFloor(want)
	if sh <= 0 {
		return 0
	}
	if sh > have {
		return have
	}
	remain := have - sh
	if remain > 0 && remain < LotSize {
		return have
	}
	return sh
}

func scoreOrNaN(m map[string]float64, code string) float64 {
	if v, ok := m[code]; ok {
		return v
	}
	return math.NaN()
}

