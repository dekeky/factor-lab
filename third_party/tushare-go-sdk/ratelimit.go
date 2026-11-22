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

type windowLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	times  []time.Time
}

func newWindowLimiter(limit int, window time.Duration) *windowLimiter {
	if limit <= 0 || window <= 0 {
		return nil
	}
	return &windowLimiter{limit: limit, window: window}
}

func (l *windowLimiter) wait(ctx context.Context) error {
	if l == nil {
		return nil
	}
	for {
		l.mu.Lock()
		now := time.Now()
		cutoff := now.Add(-l.window)
		i := 0
		for i < len(l.times) && !l.times[i].After(cutoff) {
			i++
		}
		if i > 0 {
			l.times = append([]time.Time(nil), l.times[i:]...)
		}
		if len(l.times) < l.limit {
			l.times = append(l.times, now)
			l.mu.Unlock()
			return nil
		}
		wait := l.times[0].Add(l.window).Sub(now)
		l.mu.Unlock()
		if wait < time.Millisecond {
			wait = time.Millisecond
		}
		if err := sleepCtx(ctx, wait); err != nil {
			return err
		}
	}
}

func (c *Client) waitAPI(ctx context.Context, apiName string) error {
	if c == nil {
		return nil
	}
	if c.apiRPM > 0 {
		return c.limiterFor(apiName).wait(ctx)
	}
	if c.limiter != nil {
		return c.limiter.wait(ctx)
	}
	return nil
}

func (c *Client) limiterFor(apiName string) interface{ wait(context.Context) error } {
	c.apiMu.Lock()
	defer c.apiMu.Unlock()
	if c.apiLim == nil {
		c.apiLim = map[string]interface{ wait(context.Context) error }{}
	}
	if l, ok := c.apiLim[apiName]; ok {
		return l
	}
	// 测试可把 apiWindow 设成短窗口，用滑动窗口精确卡次数。
	// 生产用令牌桶：500 次/分钟 ≈ 8.3 QPS，burst=2，避免开闸 500 次打满额度。
	if c.apiWindow > 0 {
		limit := int(c.apiRPM)
		if limit < 1 {
			limit = 1
		}
		l := newWindowLimiter(limit, c.apiWindow)
		c.apiLim[apiName] = l
		return l
	}
	qps := c.apiRPM / 60
	if qps <= 0 {
		qps = 500.0 / 60
	}
	l := newRateLimiter(qps, 2)
	c.apiLim[apiName] = l
	return l
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
