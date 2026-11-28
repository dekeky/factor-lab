package data

import "testing"

func TestBatchTradeDatesPairs(t *testing.T) {
	got := batchTradeDates([]string{"d1", "d2", "d3"})
	if len(got) != 2 {
		t.Fatalf("batches %d, want 2: %v", len(got), got)
	}
	if len(got[0]) != 2 || got[0][0] != "d1" || got[0][1] != "d2" {
		t.Errorf("first batch %v, want [d1 d2]", got[0])
	}
	if len(got[1]) != 1 || got[1][0] != "d3" {
		t.Errorf("second batch %v, want [d3]", got[1])
	}
	if got := batchTradeDates(nil); got != nil {
		t.Errorf("empty dates should be nil, got %v", got)
	}
}

func TestShouldSplitBatch(t *testing.T) {
	cases := []struct {
		n, days int
		hasMore bool
		want    bool
	}{
		{n: 6001, days: 2, hasMore: false, want: true},
		{n: 6000, days: 2, hasMore: true, want: true},
		{n: 6000, days: 2, hasMore: false, want: true},
		{n: 100, days: 2, hasMore: false, want: false},
		{n: 6000, days: 1, hasMore: true, want: false},
		{n: 7000, days: 1, hasMore: false, want: false},
	}
	for _, c := range cases {
		got := shouldSplitBatch(c.n, c.hasMore, c.days)
		if got != c.want {
			t.Errorf("shouldSplitBatch(%d, hasMore=%v, days=%d)=%v, want %v",
				c.n, c.hasMore, c.days, got, c.want)
		}
	}
}
