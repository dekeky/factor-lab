package tushare

import (
	"errors"
	"fmt"
	"strings"
)

// ErrorKind 表示错误的归类，便于调用方用 errors.As 做分支处理。
type ErrorKind int

const (
	// KindUnknown 未归类的错误。
	KindUnknown ErrorKind = iota
	// KindTokenInvalid token 为空、无效或已过期，重试无意义。
	KindTokenInvalid
	// KindPermission 账号权限或积分不足（tushare 的积分门槛）。
	KindPermission
	// KindParam 请求参数不合法，例如 ts_code 与 trade_date 同时缺失。
	KindParam
	// KindRateLimited 触发频次限制，可按 RetryAfter 等待后重试。
	KindRateLimited
	// KindServer 服务端错误或网关异常。
	KindServer
	// KindNetwork 网络层错误（超时、连接重置、DNS 失败等）。
	KindNetwork
	// KindDecode 响应体无法解析为 tushare 协议。
	KindDecode
)

func (k ErrorKind) String() string {
	switch k {
	case KindTokenInvalid:
		return "token_invalid"
	case KindPermission:
		return "permission"
	case KindParam:
		return "param"
	case KindRateLimited:
		return "rate_limited"
	case KindServer:
		return "server"
	case KindNetwork:
		return "network"
	case KindDecode:
		return "decode"
	default:
		return "unknown"
	}
}

// APIError 是 SDK 对外暴露的统一错误类型。
//
// 永远不包含 token 与请求体，避免敏感信息进入日志。
type APIError struct {
	// Kind 错误归类。
	Kind ErrorKind
	// Code 是 tushare 业务返回码，0 表示成功；网络层错误时为 0。
	Code int
	// Msg 是服务端返回的错误描述，或 SDK 生成的说明。
	Msg string
	// API 是触发错误的 api_name，便于定位。
	API string
	// HTTPStatus 是 HTTP 状态码，网络层错误时为 0。
	HTTPStatus int
	// RequestID 是服务端返回的请求 ID，排查问题时提供给 tushare 官方。
	RequestID string
	// Detail 是服务端返回的补充说明（部分错误码会有）。
	Detail string
	// RetryAfter 是建议的重试等待时间（服务端给出的频次限制窗口）。
	RetryAfter int // 秒

	// Err 保留底层错误，支持 errors.Is / errors.Unwrap。
	Err error
}

// Error 实现 error 接口。
func (e *APIError) Error() string {
	var b strings.Builder
	b.WriteString("tushare")
	if e.API != "" {
		b.WriteString("[" + e.API + "]")
	}
	b.WriteString(": ")
	if e.Kind != KindUnknown {
		b.WriteString(e.Kind.String())
		b.WriteString(": ")
	}
	if e.Msg != "" {
		b.WriteString(e.Msg)
	} else if e.Err != nil {
		b.WriteString(e.Err.Error())
	} else {
		b.WriteString("未知错误")
	}
	if e.Code != 0 {
		fmt.Fprintf(&b, " (code=%d)", e.Code)
	}
	if e.HTTPStatus != 0 && e.HTTPStatus != 200 {
		fmt.Fprintf(&b, " (http=%d)", e.HTTPStatus)
	}
	if e.RequestID != "" {
		fmt.Fprintf(&b, " (request_id=%s)", e.RequestID)
	}
	return b.String()
}

// Unwrap 返回底层错误。
func (e *APIError) Unwrap() error { return e.Err }

// Retryable 表示该错误是否值得重试。
func (e *APIError) Retryable() bool {
	switch e.Kind {
	case KindRateLimited, KindServer, KindNetwork, KindDecode:
		return true
	default:
		return false
	}
}

// ErrNoRows 表示结果集为空（仅 Scan 单行时返回）。
var ErrNoRows = errors.New("tushare: 结果集为空")

// IsKind 判断 err 是否为指定归类的 *APIError。
func IsKind(err error, kind ErrorKind) bool {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Kind == kind
	}
	return false
}

// IsRateLimited 判断是否为频次限制错误。
func IsRateLimited(err error) bool { return IsKind(err, KindRateLimited) }

// IsPermission 判断是否为权限/积分不足错误。
func IsPermission(err error) bool { return IsKind(err, KindPermission) }

// IsTokenInvalid 判断是否为 token 无效错误。
func IsTokenInvalid(err error) bool { return IsKind(err, KindTokenInvalid) }

// classify 把 tushare 的 {code, msg} 映射为错误归类。
//
// 官方文档未完整公开返回码，因此优先按已知码匹配，再用关键词兜底。
func classify(code int, msg string, api string, httpStatus int) *APIError {
	e := &APIError{Code: code, Msg: msg, API: api, HTTPStatus: httpStatus}
	switch code {
	case -2001:
		e.Kind = KindPermission
	case -2002, -2003, 40101:
		// 40101：token 未上传或无效（实测返回）。
		e.Kind = KindTokenInvalid
	case 40001:
		e.Kind = KindParam
	case 40203:
		e.Kind = KindRateLimited
		e.RetryAfter = 60
	default:
		e.Kind = classifyByMessage(msg)
	}
	return e
}

func classifyByMessage(msg string) ErrorKind {
	m := strings.ToLower(msg)
	switch {
	case strings.Contains(m, "每分钟") || strings.Contains(m, "频率") ||
		strings.Contains(m, "频次") || strings.Contains(m, "too many") ||
		strings.Contains(m, "rate limit"):
		return KindRateLimited
	case strings.Contains(m, "token") || strings.Contains(msg, "令牌"):
		// 上传缺失、无效、过期等都以 token 为关键词
		return KindTokenInvalid
	case strings.Contains(msg, "权限") || strings.Contains(msg, "积分") ||
		strings.Contains(m, "permission") || strings.Contains(m, "privilege"):
		return KindPermission
	case strings.Contains(msg, "参数") || strings.Contains(m, "invalid param") ||
		strings.Contains(m, "parameter"):
		return KindParam
	default:
		return KindUnknown
	}
}
