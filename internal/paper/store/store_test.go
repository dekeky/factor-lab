package store

import (
	"path/filepath"
	"testing"
	"time"

	"factor-lab/internal/paper/account"
	"factor-lab/internal/paper/ledger"
	"factor-lab/internal/paper/matching"
)

func TestInsertAccountAndInitSnapshot(t *testing.T) {
	st := openTemp(t)
	acc := account.Account{
		Name: "t", Expr: "alpha = close",
		Config: account.Config{TopN: 2, Capital: 1e6, Cost: 0.003, Start: "2024-01-02"},
		CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	id, err := st.InsertAccount(acc)
	if err != nil {
		t.Fatal(err)
	}
	if id < 1 {
		t.Fatalf("id %d", id)
	}
	snap := ledger.Snapshot{AccountID: id, Date: "2024-01-01", Cash: 1e6, NAV: 1e6}
	if err := st.InsertSnapshot(snap); err != nil {
		t.Fatal(err)
	}
	book, err := st.LatestBook(id)
	if err != nil {
		t.Fatal(err)
	}
	if book.Cash != 1e6 || book.LastSettled != "2024-01-01" {
		t.Fatalf("%+v", book)
	}
}

func TestPlanUniquePerExecDay(t *testing.T) {
	st := openTemp(t)
	id, err := st.InsertAccount(account.Account{Name: "t", Expr: "x", Config: account.Config{TopN: 1, Capital: 1}, CreatedAt: time.Now(), UpdatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	p := matching.Plan{AccountID: id, SignalDate: "2024-01-01", ExecAt: "2024-01-02 09:30:00", Status: "pending",
		Intents: []matching.Intent{{Code: "A", Side: matching.SideOpen, Shares: 100}},
		Picks:   []matching.Pick{{Code: "A", Score: 1.2, Rank: 1}, {Code: "B", Score: 0.9, Rank: 2}}}
	pid, err := st.InsertPlan(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.GetPlan(pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Picks) != 2 || got.Picks[0].Code != "A" || got.Picks[1].Rank != 2 {
		t.Fatalf("picks %+v", got.Picks)
	}
	if err := st.UpdatePlanPicks(pid, []matching.Pick{{Code: "C", Score: 3, Rank: 1}}); err != nil {
		t.Fatal(err)
	}
	got, err = st.GetPlan(pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Picks) != 1 || got.Picks[0].Code != "C" {
		t.Fatalf("updated picks %+v", got.Picks)
	}
	p.SignalDate = "2024-01-02"
	if _, err := st.InsertPlan(p); err == nil {
		t.Fatal("expected unique exec day conflict")
	}
}

func TestMarkPlanDoneWritesActualShares(t *testing.T) {
	st := openTemp(t)
	id, err := st.InsertAccount(account.Account{Name: "t", Expr: "x", Config: account.Config{TopN: 1, Capital: 1}, CreatedAt: time.Now(), UpdatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	pid, err := st.InsertPlan(matching.Plan{AccountID: id, SignalDate: "2024-01-01", ExecAt: "2024-01-02 09:30:00", Status: "pending",
		Intents: []matching.Intent{{Code: "A", Side: matching.SideOpen, Shares: 100}}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.MarkPlanDone(pid, []matching.Intent{{Code: "A", Side: matching.SideOpen, Shares: 100, ActualShares: 90}})
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("expected update")
	}
	plan, err := st.GetPlan(pid)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "done" || plan.Intents[0].ActualShares != 90 || plan.Intents[0].Shares != 100 {
		t.Fatalf("%+v", plan)
	}
	again, err := st.MarkPlanDone(pid, []matching.Intent{{Code: "A", ActualShares: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if again {
		t.Fatal("second done should be no-op")
	}
	plan, _ = st.GetPlan(pid)
	if plan.Intents[0].ActualShares != 90 {
		t.Fatalf("overwrote actuals %+v", plan.Intents[0])
	}
}

func openTemp(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "paper.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}
