import { useEffect, useRef, useState } from 'react'
import { api } from '../api'

/**
 * 登录门：服务端启用 -api-token 时先校验访问令牌。
 * 令牌只在登录请求里出现一次，服务端下发 HttpOnly Cookie，之后由浏览器自动携带；
 * 前端既不写 localStorage，也不把令牌拼进 URL。
 */
export function LoginScreen({ onDone }: { onDone: () => void }) {
  const [token, setToken] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    inputRef.current?.focus()
  }, [])

  const submit = async () => {
    const t = token.trim()
    if (!t) {
      setErr('请输入访问令牌')
      return
    }
    setBusy(true)
    setErr('')
    try {
      await api.login(t)
      onDone()
    } catch (e) {
      setErr((e as Error).message)
      setBusy(false)
    }
  }

  return (
    <div className="login-wrap">
      <div className="login-card">
        <div className="login-brand">
          <span className="mark">FL</span>
          <div>
            <div className="login-title">factor-lab</div>
            <div className="login-sub">AI 因子研究平台</div>
          </div>
        </div>
        <p className="login-hint">
          该服务已启用访问令牌保护。请输入部署时配置的令牌（<code className="inline">FACTOR_LAB_TOKEN</code>）。
        </p>
        <form
          className="login-form"
          onSubmit={(e) => {
            e.preventDefault()
            submit()
          }}
        >
          <label htmlFor="fl-token">访问令牌</label>
          <input
            id="fl-token"
            ref={inputRef}
            type="password"
            autoComplete="current-password"
            placeholder="粘贴访问令牌"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            disabled={busy}
          />
          {err && <div className="login-err">{err}</div>}
          <button className="btn primary" type="submit" disabled={busy}>
            {busy ? '验证中…' : '登录'}
          </button>
        </form>
        <div className="login-foot">令牌校验通过后会以安全 Cookie 记住登录状态（12 小时）。</div>
      </div>
    </div>
  )
}
