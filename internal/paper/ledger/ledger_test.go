package ledger

import (
	"math"
	"testing"
)

func TestApplyBuyDebitsCashAndFee(t *testing.T) {
	book := Book{AccountID: 1, Cash: 10000}
	next, err := Apply(book, []Fill{{
		Code: "A", Side: "buy", Shares: 100, Price: 10, Amount: 1000, Fee: 3, Status: "filled",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(next.Cash-8997) > 1e-9 {
		t.Fatalf("cash %v, want 8997", next.Cash)
	}
	if len(next.Positions) != 1 || next.Positions[0].Code != "A" || next.Positions[0].Shares != 100 {
		t.Fatalf("positions %+v", next.Positions)
	}
	if math.Abs(next.Positions[0].Cost-10.03) > 1e-9 {
		t.Fatalf("avg cost %v, want 10.03", next.Positions[0].Cost)
	}
}

func TestApplySellCreditsCashMinusFee(t *testing.T) {
	book := Book{AccountID: 1, Cash: 1000, Positions: []Position{{Code: "A", Shares: 200, Cost: 10}}}
	next, err := Apply(book, []Fill{{
		Code: "A", Side: "sell", Shares: 100, Price: 12, Amount: 1200, Fee: 3.6, Status: "filled",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(next.Cash-2196.4) > 1e-9 {
		t.Fatalf("cash %v, want 2196.4", next.Cash)
	}
	if len(next.Positions) != 1 || next.Positions[0].Shares != 100 {
		t.Fatalf("positions %+v", next.Positions)
	}
	if math.Abs(next.Positions[0].Cost-10) > 1e-9 {
		t.Fatalf("remaining cost %v, want 10", next.Positions[0].Cost)
	}
}

func TestApplySkippedDoesNotChangeBook(t *testing.T) {
	book := Book{AccountID: 1, Cash: 5000, Positions: []Position{{Code: "A", Shares: 100, Cost: 9}}}
	next, err := Apply(book, []Fill{{
		Code: "B", Side: "buy", Shares: 0, Status: "skipped_suspend",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if next.Cash != book.Cash || len(next.Positions) != 1 || next.Positions[0].Shares != 100 {
		t.Fatalf("book changed: %+v", next)
	}
}
