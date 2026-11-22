import { useEffect, useState, type ReactNode } from 'react'
import type { Health, Meta } from '../types'
import { applyTheme, readTheme, type Theme } from '../lib/theme'

export type PageKey = 'factory' | 'library' | 'compose' | 'paper' | 'help'

/** 界面展示用模型名：去掉内部引擎前缀 eino: */
export function displayLLM(name?: string) {
  if (!name || name === 'unconfigured') return ''
  return name.replace(/^eino:/, '')
}

function Ico({ children }: { children: ReactNode }) {
  return (
    <svg className="ico-svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      {children}
    </svg>
  )
}

const NAV_ICON: Record<PageKey | 'settings', ReactNode> = {
  factory: (
    <Ico>
      <polyline points="7 8 3 12 7 16" />
      <polyline points="17 8 21 12 17 16" />
      <line x1="14" y1="4" x2="10" y2="20" />
    </Ico>
  ),
  compose: (
    <Ico>
      <rect x="4" y="4" width="16" height="16" rx="1" />
      <line x1="12" y1="8" x2="12" y2="16" />
      <line x1="8" y1="12" x2="16" y2="12" />
    </Ico>
  ),
  library: (
    <Ico>
      <line x1="8" y1="6" x2="21" y2="6" />
      <line x1="8" y1="12" x2="21" y2="12" />
      <line x1="8" y1="18" x2="21" y2="18" />
      <line x1="3" y1="6" x2="3.01" y2="6" />
      <line x1="3" y1="12" x2="3.01" y2="12" />
      <line x1="3" y1="18" x2="3.01" y2="18" />
    </Ico>
  ),
  paper: (
    <Ico>
      <polyline points="3 17 9 11 13 15 21 7" />
      <polyline points="15 7 21 7 21 13" />
    </Ico>
  ),
  help: (
    <Ico>
      <circle cx="12" cy="12" r="9" />
      <path d="M9.5 9.5a2.5 2.5 0 0 1 5 1c0 1.5-2.5 2-2.5 3.5" />
      <line x1="12" y1="17" x2="12.01" y2="17" />
    </Ico>
  ),
  settings: (
    <Ico>
      <line x1="4" y1="6" x2="20" y2="6" />
      <line x1="4" y1="12" x2="20" y2="12" />
      <line x1="4" y1="18" x2="20" y2="18" />
      <circle cx="8" cy="6" r="2" />
      <circle cx="16" cy="12" r="2" />
      <circle cx="10" cy="18" r="2" />
    </Ico>
  ),
}

export const PAGES: { key: PageKey; label: string; group: string; desc: string }[] = [
  { key: 'factory', label: '因子工厂', group: '研究', desc: '写公式并评价' },
  { key: 'compose', label: '因子组合', group: '研究', desc: '多因子加权合成' },
  { key: 'library', label: '因子库', group: '研究', desc: '管理与批量评价' },
  { key: 'paper', label: '实盘模拟', group: '实盘', desc: '虚拟资金按日选股' },
  { key: 'help', label: '帮助', group: '帮助', desc: '因子研究教材、DSL 与字段参考' },
]

const GROUPS: { name: string; keys: PageKey[] }[] = [
  { name: '研究', keys: ['factory', 'compose', 'library'] },
  { name: '实盘', keys: ['paper'] },
  { name: '帮助', keys: ['help'] },
]

export function Sidebar({
  page,
  onNav,
  counts,
  llm,
  health,
  onSettings,
}: {
  page: PageKey
  onNav: (p: PageKey) => void
  counts: Record<string, number>
  llm?: string
  health?: Health | null
  onSettings?: () => void
}) {
  const online = !!llm && llm !== 'unconfigured'
  const src = health?.data_source ? health.data_source.split(':')[0] : ''
  return (
    <aside className="sidebar">
      <div className="brand">
        <div className="name">factor-lab</div>
        <div className="tagline">把选股规则写成公式</div>
      </div>
      <nav>
        {GROUPS.map((g) => (
          <div key={g.name}>
            <div className="group-label">{g.name}</div>
            {g.keys.map((k) => {
              const p = PAGES.find((x) => x.key === k)!
              return (
                <button key={k} className={`nav-item ${page === k ? 'on' : ''}`} onClick={() => onNav(k)} title={p.desc}>
                  <span className="ico">{NAV_ICON[k]}</span>
                  <span>{p.label}</span>
                  {counts[k] ? <span className="badge">{counts[k]}</span> : null}
                </button>
              )
            })}
          </div>
        ))}
      </nav>
      <ThemeSwitch />
      <button className="nav-item sidebar-set" onClick={onSettings} title="配置 Tushare Token 与 AI 助手模型">
        <span className="ico">{NAV_ICON.settings}</span>
        <span>设置</span>
      </button>
      <div className="foot">
        <div>{online ? displayLLM(llm) : '模型未配置'}</div>
        {src && health ? (
          <div>
            {src} {health.panel.dates}×{health.panel.stocks}
          </div>
        ) : (
          <div>行情未加载</div>
        )}
      </div>
    </aside>
  )
}

export function ThemeSwitch() {
  const [theme, setTheme] = useState<Theme>(readTheme)
  useEffect(() => {
    const on = () => setTheme(readTheme())
    window.addEventListener('fl-theme', on)
    return () => window.removeEventListener('fl-theme', on)
  }, [])
  return (
    <div className="theme-switch" role="group" aria-label="外观">
      <button type="button" className={theme === 'dark' ? 'on' : ''} onClick={() => applyTheme('dark')}>
        深色
      </button>
      <button type="button" className={theme === 'light' ? 'on' : ''} onClick={() => applyTheme('light')}>
        浅色
      </button>
    </div>
  )
}

export function Topbar({
  health,
  meta,
}: {
  page: PageKey
  health: Health | null
  meta: Meta | null
}) {
  const srcShort = (s?: string) => (s ? s.split(':')[0] : '')
  return (
    <header className="topbar">
      <div className="tape">
        {health && (
          <>
            <span title={health.data_source}>{srcShort(health.data_source) || '行情'}</span>
            <span>
              <b>{health.panel.dates}</b> 日 × <b>{health.panel.stocks}</b> 股
            </span>
            <span>
              {health.range.from} — {health.range.to}
            </span>
          </>
        )}
        {meta && (
          <span>
            {meta.fields.length} 字段 / {meta.operators.length} 算子
          </span>
        )}
      </div>
    </header>
  )
}

export function PageHead({
  title,
  desc,
  actions,
}: {
  title: string
  desc?: string
  actions?: React.ReactNode
}) {
  return (
    <div className="page-head">
      <div>
        <h2>{title}</h2>
        {desc && <p>{desc}</p>}
      </div>
      {actions && <div className="actions">{actions}</div>}
    </div>
  )
}

export function Empty({ text, hint }: { text: string; hint?: string }) {
  return (
    <div className="empty">
      <p>{text}</p>
      {hint && <p className="subtle mt4">{hint}</p>}
    </div>
  )
}

export function Busy({ text = '计算中…' }: { text?: string }) {
  return (
    <div className="busy-line">
      <span className="spinner" />
      {text}
    </div>
  )
}

export const CATEGORY_LABEL: Record<string, string> = {
  alpha101: 'Alpha101',
  alpha158: 'Alpha158',
  momentum: '动量',
  reversal: '反转',
  volatility: '波动',
  volume: '量价',
  valuation: '估值',
  quality: '质量',
  size: '规模',
  combo: '组合',
  user: '自建',
  llm: 'AI',
  classic: '经典',
}

export const SOURCE_LABEL: Record<string, string> = {
  classic: '内置',
  user: '自建',
  llm: 'AI',
  composed: '组合',
}

export function catLabel(c: string) {
  return CATEGORY_LABEL[c] ?? c
}

export function srcTag(source: string) {
  const cls = source === 'classic' ? 'gray' : source === 'llm' ? 'purple' : source === 'composed' ? 'amber' : 'blue'
  return <span className={`tag ${cls}`}>{SOURCE_LABEL[source] ?? source}</span>
}

/** 数字着色：正向红、负向绿（中国市场惯例） */
export function signCls(v: number | undefined | null) {
  if (v === undefined || v === null || Number.isNaN(v)) return 'muted'
  return v > 0 ? 'pos' : v < 0 ? 'neg' : 'muted'
}

export function fmt(v: number | undefined | null, digits = 4) {
  if (v === undefined || v === null || Number.isNaN(v)) return '—'
  return v.toFixed(digits)
}

export function pct(v: number | undefined | null, digits = 1) {
  if (v === undefined || v === null || Number.isNaN(v)) return '—'
  return (v * 100).toFixed(digits) + '%'
}

/** 时间戳 → 「2026-08-01 11:30:00」（本地时区，秒级） */
export function dt(iso: string | undefined | null) {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '—'
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}
