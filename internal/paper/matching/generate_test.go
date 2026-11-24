package matching

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"factor-lab/internal/panel"
	"factor-lab/internal/paper/account"
	"factor-lab/internal/paper/ledger"
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

func findIntent(plan Plan, code string) (Intent, bool) {
	for _, in := range plan.Intents {
		if in.Code == code {
			return in, true
		}
	}
	return Intent{}, false
}

func TestGenerateEmptyBookEntersTopN(t *testing.T) {
	const T, N = 2, 3
	close := fill(T, N, 10)
	score := []float64{3, 2, 1, 3, 2, 1}
	p := mkPanel(t, T, N, map[string][]float64{"close": close})
	p.SetNames([]string{"甲", "乙", "丙"})
	acc := account.Account{Config: account.Config{TopN: 2, Capital: 10000}}
	book := ledger.Book{Cash: 10000}
	plan, err := Generate(p, score, acc, book, "2024-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if plan.SignalDate != "2024-01-01" || plan.ExecAt != "" {
		t.Fatalf("dates %+v", plan)
	}
	a, ok := findIntent(plan, "A")
	if !ok || a.Side != SideOpen || a.Shares != 500 {
		t.Fatalf("A %+v", a)
	}
	b, ok := findIntent(plan, "B")
	if !ok || b.Side != SideOpen || b.Shares != 500 {
		t.Fatalf("B %+v", b)
	}
	if _, ok := findIntent(plan, "C"); ok {
		t.Fatal("C should not enter")
	}
	if len(plan.Picks) != 2 || plan.Picks[0].Code != "A" || plan.Picks[0].Rank != 1 || plan.Picks[1].Code != "B" {
		t.Fatalf("picks %+v", plan.Picks)
	}
	if plan.Picks[0].Label != "甲（A）" || plan.Picks[0].Name != "甲" {
		t.Fatalf("pick label %+v", plan.Picks[0])
	}
	direct, err := PicksOnDate(p, score, "2024-01-01", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(direct) != 2 || direct[0].Code != "A" || direct[1].Code != "B" {
		t.Fatalf("PicksOnDate %+v", direct)
	}
}

func TestGenerateExitAndEnterReasons(t *testing.T) {
	const T, N = 2, 3
	close := fill(T, N, 10)
	score := []float64{3, 2, 1, 3, 2, 1} // A>B>C
	p := mkPanel(t, T, N, map[string][]float64{"close": close})
	acc := account.Account{Config: account.Config{TopN: 1}}
	book := ledger.Book{Cash: 0, Positions: []ledger.Position{{Code: "C", Shares: 100}}}
	plan, err := Generate(p, score, acc, book, "2024-01-01")
	if err != nil {
		t.Fatal(err)
	}
	c, ok := findIntent(plan, "C")
	if !ok || c.Side != SideExit || c.Shares != 100 {
		t.Fatalf("C %+v", c)
	}
	a, ok := findIntent(plan, "A")
	if !ok || a.Side != SideOpen {
		t.Fatalf("A %+v", a)
	}
}

func TestGenerateBuyLotsReserveFeeAndSlippage(t *testing.T) {
	const T, N = 2, 1
	p := mkPanel(t, T, N, map[string][]float64{"close": fill(T, N, 10)})
	score := fill(T, N, 1)
	acc := account.Account{Config: account.Config{TopN: 1, Cost: 0.003, SlippageBps: 10}}
	plan, err := Generate(p, score, acc, ledger.Book{Cash: 10000}, "2024-01-01")
	if err != nil {
		t.Fatal(err)
	}
	a, ok := findIntent(plan, "A")
	if !ok {
		t.Fatal("missing A")
	}
	// 10 * 1.001 * 1.003 = 10.034003 → 10000/that/100 → 9 lots = 900, not 1000
	if a.Shares != 900 {
		t.Fatalf("Shares %v, want 900", a.Shares)
	}
}

func TestGenerateMissingCloseZeroShares(t *testing.T) {
	const T, N = 2, 1
	close := fill(T, N, math.NaN())
	p := mkPanel(t, T, N, map[string][]float64{"close": close})
	acc := account.Account{Config: account.Config{TopN: 1}}
	plan, err := Generate(p, fill(T, N, 1), acc, ledger.Book{Cash: 10000}, "2024-01-01")
	if err != nil {
		t.Fatal(err)
	}
	a, ok := findIntent(plan, "A")
	if !ok || a.Shares != 0 || a.Side != SideOpen {
		t.Fatalf("A %+v", a)
	}
}

func TestGenerateActualsAreZero(t *testing.T) {
	const T, N = 2, 1
	p := mkPanel(t, T, N, map[string][]float64{"close": fill(T, N, 10)})
	plan, err := Generate(p, fill(T, N, 1), account.Account{Config: account.Config{TopN: 1}}, ledger.Book{Cash: 10000}, "2024-01-01")
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range plan.Intents {
		if in.ActualShares != 0 {
			t.Fatalf("actuals set: %+v", in)
		}
	}
}

func TestGenerateHoldWhenWeightInsideDrift(t *testing.T) {
	const T, N = 2, 1
	p := mkPanel(t, T, N, map[string][]float64{"close": fill(T, N, 10)})
	acc := account.Account{Config: account.Config{TopN: 1}}
	book := ledger.Book{Cash: 0, Positions: []ledger.Position{{Code: "A", Shares: 500}}}
	plan, err := Generate(p, fill(T, N, 1), acc, book, "2024-01-01")
	if err != nil {
		t.Fatal(err)
	}
	a, ok := findIntent(plan, "A")
	if !ok || a.Side != SideHold || a.Shares != 0 {
		t.Fatalf("A %+v", a)
	}
}

func TestIntentUnmarshalLegacyJSON(t *testing.T) {
	raw := []byte(`{"code":"A","side":"hold","buy_shares":200,"sell_shares":0,"actual_buy_shares":100,"actual_sell_shares":0,"reason":"rebalance","score":1.2,"rank":3}`)
	var in Intent
	if err := json.Unmarshal(raw, &in); err != nil {
		t.Fatal(err)
	}
	if in.Side != SideAdd || in.Shares != 200 || in.ActualShares != 100 || in.Rank != 3 {
		t.Fatalf("%+v", in)
	}
	raw = []byte(`{"code":"B","side":"sell","buy_shares":0,"sell_shares":50,"actual_sell_shares":50,"reason":"exit"}`)
	if err := json.Unmarshal(raw, &in); err != nil {
		t.Fatal(err)
	}
	if in.Side != SideExit || in.Shares != 50 || in.ActualShares != 50 {
		t.Fatalf("%+v", in)
	}
}
