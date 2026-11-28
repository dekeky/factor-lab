package data

import "testing"

func TestQuarterPeriodsInclusive(t *testing.T) {
	got := quarterPeriods("20000101", "20001231")
	want := []string{"20000331", "20000630", "20000930", "20001231"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("period %d: got %s want %s", i, got[i], want[i])
		}
	}
	got = quarterPeriods("20240101", "20240228")
	if len(got) != 0 {
		t.Errorf("no quarter end in Jan-Feb 2024, got %v", got)
	}
	got = quarterPeriods("20231001", "20240228")
	if len(got) != 1 || got[0] != "20231231" {
		t.Errorf("expected only 20231231, got %v", got)
	}
}
