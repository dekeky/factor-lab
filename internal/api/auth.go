package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// 会话鉴权：token 只在登录时提交一次，之后靠 HttpOnly Cookie 维持，
// 前端不再把令牌写进 localStorage，也不放进 URL（避免 XSS 读取与日志泄露）。
const (
	sessionCookie = "fl_session"
	sessionTTL    = 12 * time.Hour
	// 登录失败限流：同一 IP 在 1 分钟内连续失败达到上限即短暂锁定，防在线爆破
	loginMaxFail = 5
	loginWindow  = time.Minute
)

// sessionResp 是 GET/POST /api/session 的响应。
type sessionResp struct {
	Required bool   `json:"required"` // 服务端是否启用了访问令牌
	Authed   bool   `json:"authed"`   // 当前请求是否已通过鉴权
	Message  string `json:"message,omitempty"`
}

// tokenDigest 把访问令牌转成可安全下发的 Cookie 值（单向散列，不回传原文）。
func tokenDigest(token string) string {
	sum := sha256.Sum256([]byte("factor-lab/session/v1:" + token))
	return hex.EncodeToString(sum[:])
}

// authed 判断请求是否已通过鉴权：Cookie 会话优先，其次 X-API-Token 头
// （给 curl / 自动化脚本用）。两者都用常量时间比较，避免时序侧信道。
func (s *Server) authed(r *http.Request) bool {
	want := tokenDigest(s.Opt.APIToken)
	if c, err := r.Cookie(sessionCookie); err == nil {
		if subtle.ConstantTimeCompare([]byte(c.Value), []byte(want)) == 1 {
			return true
		}
	}
	if tok := r.Header.Get("X-API-Token"); tok != "" {
		if subtle.ConstantTimeCompare([]byte(tok), []byte(s.Opt.APIToken)) == 1 {
			return true
		}
	}
	return false
}

// setSessionCookie 下发会话 Cookie；HTTPS（含反代）才加 Secure，避免本机 HTTP 部署失效。
func setSessionCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	c := &http.Cookie{
		Name:     sessionCookie,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   maxAge,
		Secure:   r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
	}
	if maxAge > 0 {
		c.Expires = time.Now().Add(time.Duration(maxAge) * time.Second)
	}
	http.SetCookie(w, c)
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, sessionResp{
			Required: s.Opt.APIToken != "",
			Authed:   s.Opt.APIToken == "" || s.authed(r),
		})
	case http.MethodPost:
		if s.Opt.APIToken == "" {
			writeErr(w, http.StatusBadRequest, "服务未启用访问令牌，无需登录")
			return
		}
		if !s.loginAllowed(clientIP(r)) {
			writeErr(w, http.StatusTooManyRequests, "尝试次数过多，请 1 分钟后再试")
			return
		}
		var req struct {
			Token string `json:"token"`
		}
		if !readJSON(w, r, &req) {
			return
		}
		if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(req.Token)), []byte(s.Opt.APIToken)) != 1 {
			s.noteLoginFail(clientIP(r))
			writeErr(w, http.StatusUnauthorized, "访问令牌不正确")
			return
		}
		s.noteLoginOK(clientIP(r))
		setSessionCookie(w, r, tokenDigest(s.Opt.APIToken), int(sessionTTL.Seconds()))
		writeJSON(w, http.StatusOK, sessionResp{Required: true, Authed: true})
	case http.MethodDelete:
		// 退出登录：立刻失效（清 Cookie + 过期时间置零）
		setSessionCookie(w, r, "", -1)
		writeJSON(w, http.StatusOK, sessionResp{Required: s.Opt.APIToken != "", Authed: false})
	default:
		writeErr(w, http.StatusMethodNotAllowed, "方法不支持")
	}
}

func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		if i := strings.IndexByte(v, ','); i > 0 {
			return strings.TrimSpace(v[:i])
		}
		return strings.TrimSpace(v)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---- 登录限流（进程内，按 IP 计数）----

type loginTries struct {
	fails int
	since time.Time
}

var (
	loginMu  sync.Mutex
	loginMap = map[string]*loginTries{}
)

func (s *Server) loginAllowed(ip string) bool {
	loginMu.Lock()
	defer loginMu.Unlock()
	t := loginMap[ip]
	if t == nil || time.Since(t.since) > loginWindow {
		return true
	}
	return t.fails < loginMaxFail
}

func (s *Server) noteLoginFail(ip string) {
	loginMu.Lock()
	defer loginMu.Unlock()
	t := loginMap[ip]
	if t == nil || time.Since(t.since) > loginWindow {
		loginMap[ip] = &loginTries{fails: 1, since: time.Now()}
		return
	}
	t.fails++
	// 避免长期运行时 map 无限膨胀
	if len(loginMap) > 4096 {
		for k, v := range loginMap {
			if time.Since(v.since) > loginWindow {
				delete(loginMap, k)
			}
		}
	}
}

func (s *Server) noteLoginOK(ip string) {
	loginMu.Lock()
	defer loginMu.Unlock()
	delete(loginMap, ip)
}

// ---- 中间件 ----

// auth 保护 /api/*（健康检查与会话接口放行）：Cookie 会话或 X-API-Token 头。
// 未配置 token 时不做鉴权（仅建议本机或内网部署）。
func (s *Server) auth(next http.Handler) http.Handler {
	if s.Opt.APIToken == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if !strings.HasPrefix(p, "/api/") || p == "/api/health" || p == "/api/session" {
			next.ServeHTTP(w, r)
			return
		}
		if s.authed(r) {
			next.ServeHTTP(w, r)
			return
		}
		writeErr(w, http.StatusUnauthorized, "未授权：请先登录")
	})
}

// securityHeaders 注入基础安全响应头；API 一律 no-store，避免指标/密钥掩码被缓存。
func securityHeaders(next http.Handler) http.Handler {
	const csp = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; " +
		"frame-src 'self' https://s.tradingview.com https://www.tradingview.com https://cn.tradingview.com https://www.tradingview-widget.com; " +
		"frame-ancestors 'self'; base-uri 'self'; form-action 'self'"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		} else if strings.HasSuffix(r.URL.Path, ".html") || r.URL.Path == "/" {
			h.Set("Content-Security-Policy", csp)
		}
		next.ServeHTTP(w, r)
	})
}

// cors 只在显式配置了来源时开启（默认同源，不下发任何 CORS 头）。
func cors(origins []string) func(http.Handler) http.Handler {
	allow := map[string]bool{}
	for _, o := range origins {
		if o = strings.TrimSpace(o); o != "" {
			allow[o] = true
		}
	}
	return func(next http.Handler) http.Handler {
		if len(allow) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if origin := r.Header.Get("Origin"); origin != "" && allow[origin] {
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Content-Type, X-API-Token")
				h.Set("Access-Control-Allow-Credentials", "true")
				h.Set("Vary", "Origin")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
