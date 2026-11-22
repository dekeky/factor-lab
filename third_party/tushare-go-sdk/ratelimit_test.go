package tushare

import (
	"context"
	"testing"
	"time"
)

func TestAPIRateLimitIndependent(t *testing.T) {
	cli, err := NewClient("test-token", WithAPIRateLimit(2))
	if err != nil {
		t.Fatal(err)
	}
	cli.apiWindow = 80 * time.Millisecond

	ctx := context.Background()
	if err := cli.waitAPI(ctx, "daily"); err != nil {
		t.Fatalf("daily #1: %v", err)
	}
	if err := cli.waitAPI(ctx, "daily"); err != nil {
		t.Fatalf("daily #2: %v", err)
	}

	short, cancel := context.WithTimeout(ctx, 8*time.Millisecond)
	defer cancel()
	if err := cli.waitAPI(short, "daily"); err == nil {
		t.Fatal("daily #3 should wait on the same limiter")
	}

	if err := cli.waitAPI(ctx, "daily_basic"); err != nil {
		t.Fatalf("daily_basic should have its own 2/window budget: %v", err)
	}
}
