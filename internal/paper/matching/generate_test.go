package matching

import (
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
	if !ok || a.Side != "buy" || a.Reason != ReasonEnter || a.BuyShares != 500 {
		t.Fatalf("A %+v", a)
	}
	b, ok := findIntent(plan, "B")
	if !ok || b.Side != "buy" || b.Reason != ReasonEnter || b.BuyShares != 500 {
		t.Fatalf("B %+v", b)
	}
	if _, ok := findIntent(plan, "C"); ok {
		t.Fatal("C should not enter")
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
	if !ok || c.Side != "sell" || c.Reason != ReasonExit || c.SellShares != 100 {
		t.Fatalf("C %+v", c)
	}
	a, ok := findIntent(plan, "A")
	if !ok || a.Side != "buy" || a.Reason != ReasonEnter {
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
	if a.BuyShares != 900 {
		t.Fatalf("BuyShares %v, want 900", a.BuyShares)
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
	if !ok || a.BuyShares != 0 || a.Side != "buy" {
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
		if in.ActualBuyShares != 0 || in.ActualSellShares != 0 {
			t.Fatalf("actuals set: %+v", in)
		}
	}
}
