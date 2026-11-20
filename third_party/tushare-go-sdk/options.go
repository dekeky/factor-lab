package tushare

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Option 用于配置 Client，构造时校验并在失败时快速返回错误。
type Option func(*clientConfig) error

type clientConfig struct {
	baseURL   string
	http      *http.Client
	timeout   time.Duration
	retryMax  int
	retryBase time.Duration
	retryCap  time.Duration
	qps       float64
	burst     int
	logger    *slog.Logger
	userAgent string
}

func defaultConfig() *clientConfig {
	return &clientConfig{
		baseURL:   DefaultBaseURL,
		timeout:   DefaultTimeout,
		retryMax:  DefaultRetryMax,
		retryBase: DefaultRetryBase,
		retryCap:  DefaultRetryCap,
		userAgent: DefaultUserAgent,
	}
}

// WithBaseURL 覆盖服务地址，默认 https://api.tushare.pro。
//
// 主要用于指向代理网关或在测试中指向 httptest 服务。
func WithBaseURL(url string) Option {
	return func(c *clientConfig) error {
		u := strings.TrimSpace(url)
		if u == "" {
			return fmt.Errorf("tushare: BaseURL 不能为空")
		}
		c.baseURL = strings.TrimRight(u, "/")
		return nil
	}
}

// WithHTTPClient 指定自定义 http.Client，可用于接入代理、连接池调优或链路追踪。
//
// SDK 会自行控制单次请求的超时（WithTimeout），无需在自定义客户端上重复设置。
func WithHTTPClient(hc *http.Client) Option {
	return func(c *clientConfig) error {
		if hc == nil {
			return fmt.Errorf("tushare: HTTPClient 不能为 nil")
		}
		c.http = hc
		return nil
	}
}

// WithTimeout 设置单次 HTTP 请求超时，默认 30s。
func WithTimeout(d time.Duration) Option {
	return func(c *clientConfig) error {
		if d <= 0 {
			return fmt.Errorf("tushare: Timeout 必须为正数，实际 %v", d)
		}
		c.timeout = d
		return nil
	}
}

// WithRetry 配置重试策略：最多重试 max 次，指数退避从 base 开始、上限 cap。
//
// 仅对网络错误、5xx、频次限制与解码失败重试；token 无效、权限不足、参数错误不重试。
// 传 max=0 表示关闭重试。
func WithRetry(max int, base, cap time.Duration) Option {
	return func(c *clientConfig) error {
		if max < 0 {
			return fmt.Errorf("tushare: 重试次数不能为负")
		}
		if base <= 0 {
			base = DefaultRetryBase
		}
		if cap <= 0 {
			cap = DefaultRetryCap
		}
		if cap < base {
			cap = base
		}
		c.retryMax, c.retryBase, c.retryCap = max, base, cap
		return nil
	}
}

// WithRateLimit 启用客户端侧限流（令牌桶），qps 为每秒请求数，burst 为突发容量。
//
// tushare 按接口维度限制每分钟调用次数，客户端限流能在批量抓取时减少 40203 错误。
// qps<=0 表示关闭限流（默认）。
func WithRateLimit(qps float64, burst int) Option {
	return func(c *clientConfig) error {
		if qps <= 0 {
			c.qps, c.burst = 0, 0
			return nil
		}
		if burst < 1 {
			burst = 1
		}
		c.qps, c.burst = qps, burst
		return nil
	}
}

// WithLogger 注入 slog 日志器，Debug 级别记录每次请求，Warn 级别记录重试。
//
// 日志中不会出现 token。
func WithLogger(l *slog.Logger) Option {
	return func(c *clientConfig) error {
		c.logger = l
		return nil
	}
}

// WithUserAgent 设置 User-Agent，默认包含 SDK 名称与版本。
func WithUserAgent(ua string) Option {
	return func(c *clientConfig) error {
		c.userAgent = strings.TrimSpace(ua)
		return nil
	}
}

// CallOption 用于单次调用的参数覆盖（字段裁剪、分页、单次超时）。
type CallOption func(*callConfig)

type callConfig struct {
	fields  []string
	offset  int
	limit   int
	timeout time.Duration
}

func newCallConfig(opts []CallOption) *callConfig {
	c := &callConfig{}
	for _, o := range opts {
		if o != nil {
			o(c)
		}
	}
	return c
}

// WithFields 只返回指定字段（服务端按 fields 裁剪列），可显著减少传输量。
//
//	tbl, err := cli.Daily(ctx, p, tushare.WithFields("ts_code", "trade_date", "close"))
func WithFields(fields ...string) CallOption {
	return func(c *callConfig) {
		clean := make([]string, 0, len(fields))
		for _, f := range fields {
			if f = strings.TrimSpace(f); f != "" {
				clean = append(clean, f)
			}
		}
		c.fields = clean
	}
}

// WithOffset 指定起始行号（服务端分页）。
func WithOffset(n int) CallOption {
	return func(c *callConfig) {
		if n > 0 {
			c.offset = n
		}
	}
}

// WithLimit 指定本次返回的最大行数（服务端单次上限与积分相关，通常 2000~6000）。
func WithLimit(n int) CallOption {
	return func(c *callConfig) {
		if n > 0 {
			c.limit = n
		}
	}
}

// WithCallTimeout 覆盖本次调用的超时时间。
func WithCallTimeout(d time.Duration) CallOption {
	return func(c *callConfig) {
		if d > 0 {
			c.timeout = d
		}
	}
}
