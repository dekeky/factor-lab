import { useCallback, useEffect, useMemo, useState } from 'react'
import { api, setUnauthorizedHandler } from './api'
import type { Factor, Health, Meta, SessionState } from './types'
import { PAGES, Sidebar, Topbar, type PageKey } from './components/Layout'
import { SettingsModal } from './components/SettingsModal'
import { LoginScreen } from './components/LoginScreen'
import { FactoryPage } from './pages/FactoryPage'
import { LibraryPage } from './pages/LibraryPage'
import { ComposePage } from './pages/ComposePage'
import { PaperPage } from './pages/PaperPage'
import { HelpPage } from './pages/HelpPage'

function readHash(): PageKey {
  const h = location.hash.replace(/^#\/?/, '').split('?')[0]
  return (PAGES.some((p) => p.key === h) ? h : 'factory') as PageKey
}

export default function App() {
  const [page, setPage] = useState<PageKey>(readHash())
  const [session, setSession] = useState<SessionState | null>(null)
  const [health, setHealth] = useState<Health | null>(null)
  const [meta, setMeta] = useState<Meta | null>(null)
  const [factors, setFactors] = useState<Factor[]>([])
  const [bootErr, setBootErr] = useState('')
  const [loaded, setLoaded] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [bannerDismissed, setBannerDismissed] = useState(false)

  // 鉴权门：服务端启用令牌时，未登录只渲染登录页，不发任何业务请求。
  const gated = session !== null && session.required && !session.authed
  const ready = session !== null && !gated

  const refreshSession = useCallback(() => {
    api
      .session()
      .then(setSession)
      .catch(() => setSession({ required: false, authed: true }))
  }, [])

  useEffect(() => {
    refreshSession()
    // 任何接口返回 401（Cookie 过期/被清除）都回到登录门，而不是留下一堆红字报错
    setUnauthorizedHandler(() => setSession({ required: true, authed: false }))
    return () => setUnauthorizedHandler(null)
  }, [refreshSession])

  // 模型配置变更后刷新 health，让侧栏与各页的 LLM 状态立刻更新
  const refreshHealth = useCallback(() => {
    api.health().then(setHealth).catch(() => {})
  }, [])

  const refresh = useCallback(async () => {
    try {
      const fs = await api.factors()
      setFactors(fs)
      setBootErr('')
    } catch (e) {
      setBootErr((e as Error).message)
    } finally {
      setLoaded(true)
    }
  }, [])

  useEffect(() => {
    if (!ready) return
    api.health().then(setHealth).catch((e) => setBootErr((e as Error).message))
    api.meta().then(setMeta).catch(() => {})
    refresh()
  }, [ready, refresh])

  useEffect(() => {
    const onHash = () => setPage(readHash())
    window.addEventListener('hashchange', onHash)
    if (!location.hash) location.hash = '#/factory'
    return () => window.removeEventListener('hashchange', onHash)
  }, [])

  const nav = useCallback((p: PageKey) => {
    location.hash = '#/' + p
    setPage(p)
    window.scrollTo({ top: 0 })
  }, [])

  const counts = useMemo(() => ({ library: factors.length, factory: 0 }), [factors])

  if (gated) return <LoginScreen onDone={refreshSession} />
  if (!session) {
    return (
      <div className="login-wrap">
        <div className="busy-line">正在连接服务…</div>
      </div>
    )
  }

  return (
    <div className="app">
      <Sidebar page={page} onNav={nav} counts={counts} llm={health?.llm} health={health} onSettings={() => setSettingsOpen(true)} />
      <div className="main">
        {!bannerDismissed && (
          <SysBanners bootErr={bootErr} health={health} onOpenSettings={() => setSettingsOpen(true)} onDismiss={() => setBannerDismissed(true)} />
        )}
        {page !== 'factory' && page !== 'compose' && <Topbar page={page} health={health} meta={meta} />}
        <div className={`content${page === 'factory' || page === 'compose' ? ' content-factory' : ''}`}>
          {page === 'factory' && (
            <FactoryPage meta={meta} llm={health?.llm ?? 'unconfigured'} health={health} onSaved={refresh} />
          )}
          {page === 'library' && (
            <LibraryPage factors={factors} meta={meta} onChanged={refresh} onNav={(p) => nav(p)} />
          )}
          {page === 'compose' && <ComposePage factors={factors} meta={meta} health={health} llm={health?.llm ?? 'unconfigured'} onChanged={refresh} />}
          {page === 'paper' && <PaperPage factors={factors} meta={meta} />}
          {page === 'help' && <HelpPage />}

          {page !== 'help' && !loaded && !bootErr && <div className="busy-line">正在加载因子库…</div>}
        </div>
      </div>

      {settingsOpen && <SettingsModal onClose={() => setSettingsOpen(false)} onSaved={refreshHealth} />}
    </div>
  )
}

function humanizeDataError(raw: string): string {
  const s = raw.toLowerCase()
  if (s.includes('stock_st') || s.includes('st_')) return 'ST 列表'
  if (s.includes('suspend')) return '停牌'
  if (s.includes('daily_basic')) return '每日指标'
  if (s.includes('income') || s.includes('fina') || s.includes('balancesheet') || s.includes('cashflow')) return '财务数据'
  if (s.includes('index_weight') || s.includes('index')) return '指数成分'
  return ''
}

function summarizeOptionalErrors(errors: string[]): string {
  const names = [...new Set(errors.map(humanizeDataError).filter(Boolean))]
  const timeout = errors.some((e) => /deadline|timeout|超时/i.test(e))
  const what = names.length ? names.join('、') : '部分可选数据'
  return `${what}${timeout ? '请求超时' : '未能加载'}，相关因子暂不可用。`
}

function SysBanners({
  bootErr,
  health,
  onOpenSettings,
  onDismiss,
}: {
  bootErr: string
  health: Health | null
  onOpenSettings: () => void
  onDismiss: () => void
}) {
  if (bootErr) {
    return (
      <div className="sys-banner err">
        <div className="grow">{bootErr}</div>
        <button type="button" onClick={onDismiss} aria-label="关闭">
          ×
        </button>
      </div>
    )
  }
  if (health && health.panel.dates === 0) {
    return (
      <div className="sys-banner warn">
        <div className="grow">
          行情尚未加载。打开
          <button type="button" onClick={onOpenSettings} style={{ textDecoration: 'underline', margin: '0 4px' }}>
            设置
          </button>
          填写 Tushare Token 后保存。
        </div>
        <button type="button" onClick={onDismiss} aria-label="关闭">
          ×
        </button>
      </div>
    )
  }
  const optional = health?.data_report?.optional_errors
  if (optional && optional.length > 0) {
    return (
      <div className="sys-banner warn" title={optional.join('\n')}>
        <div className="grow">{summarizeOptionalErrors(optional)} 可稍后在设置里保存以重试。</div>
        <button type="button" onClick={onDismiss} aria-label="关闭">
          ×
        </button>
      </div>
    )
  }
  return null
}
