package tushare

import (
	"context"
	"math"
	"sync"
	"time"
)

// rateLimiter 是一个轻量的令牌桶限流器。
//
// 之所以不引入 x/time/rate，是为了让本 SDK 保持零第三方依赖：
// 调用方只需 go get 本仓库即可接入，不必担心依赖版本冲突。
type rateLimiter struct {
	mu     sync.Mutex
	qps    float64
	burst  float64
	tokens float64
	last   time.Time
}

func newRateLimiter(qps float64, burst int) *rateLimiter {
	if qps <= 0 {
		return nil
	}
	if burst < 1 {
		burst = 1
	}
	return &rateLimiter{
		qps:    qps,
		burst:  float64(burst),
		tokens: float64(burst),
		last:   time.Now(),
	}
}

// wait 阻塞直到取得一个令牌或 ctx 结束。
func (l *rateLimiter) wait(ctx context.Context) error {
	if l == nil {
		return nil
	}
	for {
		l.mu.Lock()
		now := time.Now()
		if elapsed := now.Sub(l.last).Seconds(); elapsed > 0 {
			l.tokens = math.Min(l.burst, l.tokens+elapsed*l.qps)
			l.last = now
		}
		if l.tokens >= 1 {
			l.tokens--
			l.mu.Unlock()
			return nil
		}
		need := (1 - l.tokens) / l.qps
		l.mu.Unlock()

		if err := sleepCtx(ctx, time.Duration(need*float64(time.Second))); err != nil {
			return err
		}
	}
}
