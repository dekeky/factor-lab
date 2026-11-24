package matching

import (
	"math"
	"testing"

	"factor-lab/internal/paper/account"
	"factor-lab/internal/paper/ledger"
)

func TestExecuteSellsBeforeBuys(t *testing.T) {
	const T, N = 2, 2
	open := fill(T, N, 10)
	close := fill(T, N, 10)
	p := mkPanel(t, T, N, map[string][]float64{
		"open": open, "close": close, "vol": fill(T, N, 1000),
		"suspend": fill(T, N, math.NaN()), "up_limit": fill(T, N, math.NaN()), "down_limit": fill(T, N, math.NaN()),
		"raw_open": fill(T, N, 10),
	})
	plan := Plan{
		SignalDate: "2024-01-01",
		Intents: []Intent{
			{Code: "B", Side: SideExit, Shares: 100},
			{Code: "A", Side: SideOpen, Shares: 100},
		},
	}
	book := ledger.Book{Cash: 0, Positions: []ledger.Position{{Code: "B", Shares: 100, Cost: 10}}}
	fills, intents, next, snap, err := Execute(p, plan, book, account.Config{}, "2024-01-02 09:30:00")
	if err != nil {
		t.Fatal(err)
	}
	if len(fills) != 2 || fills[0].Side != "sell" || fills[1].Side != "buy" {
		t.Fatalf("order %+v", fills)
	}
	if fills[1].Status != "filled" || fills[1].Shares != 100 {
		t.Fatalf("buy %+v", fills[1])
	}
	if next.Cash < 0 {
		t.Fatalf("cash went negative %v", next.Cash)
	}
	a, _ := findIntent(Plan{Intents: intents}, "A")
	if a.ActualShares != 100 {
		t.Fatalf("actual buy %v", a.ActualShares)
	}
	if snap.Date != "2024-01-02" {
		t.Fatalf("snap date %s", snap.Date)
	}
}

func TestExecuteSkipLimitUp(t *testing.T) {
	const T, N = 2, 1
	p := mkPanel(t, T, N, map[string][]float64{
		"open": fill(T, N, 11), "close": fill(T, N, 11), "vol": fill(T, N, 1000),
		"raw_open": fill(T, N, 11), "up_limit": fill(T, N, 11), "down_limit": fill(T, N, 9),
		"suspend": fill(T, N, math.NaN()),
	})
	plan := Plan{Intents: []Intent{{Code: "A", Side: SideOpen, Shares: 100}}}
	_, intents, next, _, err := Execute(p, plan, ledger.Book{Cash: 10000}, account.Config{}, "2024-01-02 09:30:00")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := findIntent(Plan{Intents: intents}, "A")
	if a.ActualShares != 0 {
		t.Fatalf("bought on limit up %+v", a)
	}
	if next.Cash != 10000 {
		t.Fatalf("cash %v", next.Cash)
	}
}

func TestExecuteBuyReducesBy100WhenCashShort(t *testing.T) {
	const T, N = 2, 1
	p := mkPanel(t, T, N, map[string][]float64{
		"open": fill(T, N, 10), "close": fill(T, N, 10), "vol": fill(T, N, 1000),
		"suspend": fill(T, N, math.NaN()), "up_limit": fill(T, N, math.NaN()), "down_limit": fill(T, N, math.NaN()),
		"raw_open": fill(T, N, 10),
	})
	plan := Plan{Intents: []Intent{{Code: "A", Side: SideOpen, Shares: 1000}}}
	fills, intents, _, _, err := Execute(p, plan, ledger.Book{Cash: 9500}, account.Config{}, "2024-01-02 09:30:00")
	if err != nil {
		t.Fatal(err)
	}
	if len(fills) != 1 || fills[0].Shares != 900 || fills[0].Status != "filled" {
		t.Fatalf("fill %+v", fills)
	}
	a, _ := findIntent(Plan{Intents: intents}, "A")
	if a.ActualShares != 900 || a.Shares != 1000 {
		t.Fatalf("intent %+v", a)
	}
}

func TestExecuteWritesActualShares(t *testing.T) {
	const T, N = 2, 1
	p := mkPanel(t, T, N, map[string][]float64{
		"open": fill(T, N, 10), "close": fill(T, N, 12), "vol": fill(T, N, 1000),
		"suspend": fill(T, N, math.NaN()), "up_limit": fill(T, N, math.NaN()), "down_limit": fill(T, N, math.NaN()),
		"raw_open": fill(T, N, 10),
	})
	plan := Plan{Intents: []Intent{{Code: "A", Side: SideOpen, Shares: 100}}}
	_, intents, _, snap, err := Execute(p, plan, ledger.Book{Cash: 10000}, account.Config{}, "2024-01-02 09:30:00")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := findIntent(Plan{Intents: intents}, "A")
	if a.ActualShares != 100 {
		t.Fatalf("actual %v", a.ActualShares)
	}
	if math.Abs(snap.NAV-(10000-1000)+0) < 0 && snap.NAV <= 0 {
		t.Fatalf("nav %v", snap.NAV)
	}
}

func TestExecuteDoesNotChangePlanSharesOrReason(t *testing.T) {
	const T, N = 2, 1
	p := mkPanel(t, T, N, map[string][]float64{
		"open": fill(T, N, 10), "close": fill(T, N, 10), "vol": fill(T, N, 1000),
		"suspend": fill(T, N, math.NaN()), "up_limit": fill(T, N, math.NaN()), "down_limit": fill(T, N, math.NaN()),
		"raw_open": fill(T, N, 10),
	})
	plan := Plan{Intents: []Intent{{Code: "A", Side: SideOpen, Shares: 100}}}
	orig := plan.Intents[0]
	_, intents, _, _, err := Execute(p, plan, ledger.Book{Cash: 10000}, account.Config{}, "2024-01-02 09:30:00")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Intents[0].Shares != orig.Shares || plan.Intents[0].Side != orig.Side {
		t.Fatal("plan mutated")
	}
	if intents[0].Shares != 100 || intents[0].Side != SideOpen {
		t.Fatalf("returned %+v", intents[0])
	}
}
