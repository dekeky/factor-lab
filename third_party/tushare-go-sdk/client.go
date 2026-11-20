package tushare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"
)

// SDK 常量。
const (
	// DefaultBaseURL 是 tushare pro 的官方数据接口地址。
	DefaultBaseURL = "https://api.tushare.pro"
	// DefaultTimeout 是单次请求的默认超时。
	DefaultTimeout = 30 * time.Second
	// DefaultRetryMax 是默认重试次数（不含首次请求）。
	DefaultRetryMax = 2
	// DefaultRetryBase 是退避起始间隔。
	DefaultRetryBase = 500 * time.Millisecond
	// DefaultRetryCap 是退避间隔上限。
	DefaultRetryCap = 8 * time.Second
	// DefaultUserAgent 是默认 UA。
	DefaultUserAgent = "tushare-go-sdk/" + Version
	// EnvToken 是默认读取 token 的环境变量名。
	EnvToken = "TUSHARE_TOKEN"
	// Version 是 SDK 版本。
	Version = "0.1.0"

	maxBodySize = 64 << 20
)

// Params 是接口参数集合，对应 Python SDK 中 query 的 **kwargs。
//
// 键为服务端参数名（如 ts_code、trade_date），值为 string、int、float64、
// 字符串切片等可 JSON 序列化的类型。空值不建议放入，直接省略即可。
type Params map[string]any

// Client 是 tushare pro 数据接口客户端，并发安全，可全局复用一个实例。
type Client struct {
	token   string
	baseURL string
	ua      string
	http    *http.Client
	timeout time.Duration

	retryMax  int
	retryBase time.Duration
	retryCap  time.Duration

	limiter *rateLimiter
	logger  *slog.Logger
}

// NewClient 创建客户端。
//
//	token 可在 https://tushare.pro/user/token 获取；
//	未传 token 时尝试读取环境变量 TUSHARE_TOKEN。
func NewClient(token string, opts ...Option) (*Client, error) {
	cfg := defaultConfig()
	for _, o := range opts {
		if o == nil {
			continue
		}
		if err := o(cfg); err != nil {
			return nil, err
		}
	}

	token = strings.TrimSpace(token)
	if token == "" {
		token = strings.TrimSpace(os.Getenv(EnvToken))
	}
	if token == "" {
		return nil, fmt.Errorf("tushare: token 为空，请传参或设置环境变量 %s", EnvToken)
	}

	hc := cfg.http
	if hc == nil {
		hc = &http.Client{
			Timeout: cfg.timeout,
			Transport: &http.Transport{
				Proxy:               http.ProxyFromEnvironment,
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 16,
				IdleConnTimeout:     90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		}
	}

	c := &Client{
		token:     token,
		baseURL:   cfg.baseURL,
		ua:        cfg.userAgent,
		http:      hc,
		timeout:   cfg.timeout,
		retryMax:  cfg.retryMax,
		retryBase: cfg.retryBase,
		retryCap:  cfg.retryCap,
		logger:    cfg.logger,
	}
	if cfg.qps > 0 {
		c.limiter = newRateLimiter(cfg.qps, cfg.burst)
	}
	return c, nil
}

// NewClientFromEnv 从环境变量 TUSHARE_TOKEN 创建客户端。
func NewClientFromEnv(opts ...Option) (*Client, error) {
	return NewClient("", opts...)
}

// BaseURL 返回当前服务地址（便于排查代理配置）。
func (c *Client) BaseURL() string { return c.baseURL }

type requestBody struct {
	APIName string         `json:"api_name"`
	Token   string         `json:"token"`
	Params  map[string]any `json:"params"`
	Fields  string         `json:"fields"`
}

type responseData struct {
	Fields  []string `json:"fields"`
	Items   [][]any  `json:"items"`
	HasMore bool     `json:"has_more"`
}

type responseBody struct {
	Code      int           `json:"code"`
	Msg       string        `json:"msg"`
	RequestID string        `json:"request_id"`
	Detail    string        `json:"detail"`
	Data      *responseData `json:"data"`
}

// Query 是 SDK 的底层入口，等价于 Python SDK 的 pro.query(api_name, **kwargs)。
//
//	tbl, err := cli.Query(ctx, "daily", tushare.Params{"ts_code": "000001.SZ"},
//		tushare.WithFields("ts_code", "trade_date", "close"))
//
// 结果集为空不返回错误，此时 tbl.Len() == 0。
func (c *Client) Query(ctx context.Context, apiName string, params Params, opts ...CallOption) (*Table, error) {
	if strings.TrimSpace(apiName) == "" {
		return nil, &APIError{Kind: KindParam, Msg: "api_name 不能为空"}
	}
	return c.query(ctx, apiName, params, newCallConfig(opts))
}

func (c *Client) query(ctx context.Context, apiName string, params Params, cfg *callConfig) (*Table, error) {
	if cfg.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.timeout)
		defer cancel()
	}

	payload, err := c.buildPayload(apiName, params, cfg)
	if err != nil {
		return nil, err
	}

	delay := c.retryBase
	var lastErr error
	for attempt := 0; ; attempt++ {
		if err := c.limiter.wait(ctx); err != nil {
			return nil, err
		}
		tbl, retryable, err := c.doOnce(ctx, apiName, payload)
		if err == nil {
			return tbl, nil
		}
		lastErr = err

		var ae *APIError
		_ = errors.As(err, &ae)
		if !retryable || attempt >= c.retryMax || ctx.Err() != nil {
			if ctx.Err() != nil && lastErr != nil {
				return nil, fmt.Errorf("%w: %w", lastErr, ctx.Err())
			}
			return nil, lastErr
		}

		wait := delay
		if ae != nil && ae.RetryAfter > 0 {
			wait = time.Duration(ae.RetryAfter) * time.Second
		}
		wait = withJitter(wait)
		if c.logger != nil {
			c.logger.WarnContext(ctx, "tushare 请求失败，准备重试",
				slog.String("api", apiName),
				slog.Int("attempt", attempt+1),
				slog.Duration("wait", wait),
				slog.String("error", err.Error()))
		}
		if err := sleepCtx(ctx, wait); err != nil {
			return nil, fmt.Errorf("%w（等待重试时被中断: %w）", lastErr, err)
		}
		if delay *= 2; delay > c.retryCap {
			delay = c.retryCap
		}
	}
}

func (c *Client) buildPayload(apiName string, params Params, cfg *callConfig) ([]byte, error) {
	p := make(map[string]any, len(params)+2)
	for k, v := range params {
		if v == nil {
			continue
		}
		p[k] = v
	}
	if cfg.offset > 0 {
		p["offset"] = cfg.offset
	}
	if cfg.limit > 0 {
		p["limit"] = cfg.limit
	}
	body := requestBody{
		APIName: apiName,
		Token:   c.token,
		Params:  p,
		Fields:  strings.Join(cfg.fields, ","),
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, &APIError{Kind: KindParam, API: apiName, Msg: "参数无法序列化", Err: err}
	}
	return buf, nil
}

// doOnce 执行一次 HTTP 调用，返回结果、是否值得重试、错误。
func (c *Client) doOnce(ctx context.Context, apiName string, payload []byte) (*Table, bool, error) {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(payload))
	if err != nil {
		return nil, false, &APIError{Kind: KindNetwork, API: apiName, Msg: "构造请求失败", Err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.ua != "" {
		req.Header.Set("User-Agent", c.ua)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, true, &APIError{Kind: KindNetwork, API: apiName, Msg: "请求失败", Err: err}
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return nil, true, &APIError{Kind: KindNetwork, API: apiName, HTTPStatus: resp.StatusCode, Msg: "读取响应失败", Err: err}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests,
			c.httpError(apiName, resp, raw)
	}

	var env responseBody
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&env); err != nil {
		return nil, true, &APIError{
			Kind: KindDecode, API: apiName, HTTPStatus: resp.StatusCode,
			Msg: "响应不是合法的 tushare 数据（可能是网关错误页）", Err: err,
		}
	}
	if env.Code != 0 {
		ae := classify(env.Code, env.Msg, apiName, resp.StatusCode)
		ae.RequestID = env.RequestID
		ae.Detail = env.Detail
		return nil, ae.Retryable(), ae
	}

	tbl := &Table{API: apiName}
	if env.Data != nil {
		tbl.Fields = env.Data.Fields
		tbl.HasMore = env.Data.HasMore
		tbl.Items = make([][]any, len(env.Data.Items))
		for i, row := range env.Data.Items {
			cells := make([]any, len(row))
			for j, v := range row {
				cells[j] = normalizeCell(v)
			}
			tbl.Items[i] = cells
		}
	}

	if c.logger != nil {
		c.logger.DebugContext(ctx, "tushare 请求完成",
			slog.String("api", apiName),
			slog.Int("rows", tbl.Len()),
			slog.Bool("has_more", tbl.HasMore),
			slog.Duration("elapsed", time.Since(start)))
	}
	return tbl, false, nil
}

func (c *Client) httpError(apiName string, resp *http.Response, raw []byte) *APIError {
	msg := strings.TrimSpace(string(raw))
	if len(msg) > 300 {
		msg = msg[:300] + "..."
	}
	if strings.HasPrefix(msg, "<") {
		msg = http.StatusText(resp.StatusCode)
	}
	kind := KindServer
	if resp.StatusCode == http.StatusTooManyRequests {
		kind = KindRateLimited
	} else if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		kind = KindParam
	}
	ae := &APIError{Kind: kind, API: apiName, HTTPStatus: resp.StatusCode, Msg: msg}
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		if secs, err := time.ParseDuration(ra + "s"); err == nil {
			ae.RetryAfter = int(secs.Seconds())
		}
	}
	return ae
}

// normalizeCell 把 JSON Number 归一化为 int64 或 float64，保留整数精度。
func normalizeCell(v any) any {
	num, ok := v.(json.Number)
	if !ok {
		return v
	}
	s := num.String()
	if !strings.ContainsAny(s, ".eE") {
		if i, err := num.Int64(); err == nil {
			return i
		}
	}
	f, err := num.Float64()
	if err != nil {
		return s
	}
	return f
}

// Paginate 按 offset/limit 自动翻页，逐页回调，避免一次性拉全量数据。
//
// 页大小取 WithLimit 的值（默认由服务端决定），当服务端返回 has_more=false 时结束：
//
//	err := cli.Paginate(ctx, "daily", tushare.Params{"trade_date": "20240102"},
//		func(page *tushare.Table) error {
//			// 处理每页数据
//			return nil
//		}, tushare.WithLimit(5000))
func (c *Client) Paginate(ctx context.Context, apiName string, params Params, fn func(*Table) error, opts ...CallOption) error {
	if fn == nil {
		return &APIError{Kind: KindParam, API: apiName, Msg: "回调函数不能为 nil"}
	}
	base := newCallConfig(opts)
	pageSize := base.limit
	if pageSize <= 0 {
		pageSize = 5000
	}
	offset := base.offset

	for page := 0; page < 10000; page++ {
		cfg := *base
		cfg.offset = offset
		cfg.limit = pageSize
		tbl, err := c.query(ctx, apiName, params, &cfg)
		if err != nil {
			return err
		}
		if tbl.Len() > 0 {
			if err := fn(tbl); err != nil {
				return err
			}
		}
		if !tbl.HasMore || tbl.Len() == 0 {
			return nil
		}
		offset += tbl.Len()
	}
	return &APIError{Kind: KindServer, API: apiName, Msg: "分页超过 10000 页，已中止"}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func withJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	// ±20% 抖动，避免多协程同时重试造成尖峰。
	delta := int64(float64(d) * 0.2)
	if delta <= 0 {
		return d
	}
	return d + time.Duration(rand.Int63n(2*delta)-delta)
}

// encodeParams 把生成的参数结构体转换为 Params（走 JSON，尊重 omitempty）。
func encodeParams(v any) (Params, error) {
	if v == nil {
		return nil, nil
	}
	buf, err := json.Marshal(v)
	if err != nil {
		return nil, &APIError{Kind: KindParam, Msg: "参数结构体无法序列化", Err: err}
	}
	var raw map[string]any
	dec := json.NewDecoder(bytes.NewReader(buf))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return nil, &APIError{Kind: KindParam, Msg: "参数结构体无法解析", Err: err}
	}
	out := make(Params, len(raw))
	for k, val := range raw {
		out[k] = normalizeCell(val)
	}
	return out, nil
}
