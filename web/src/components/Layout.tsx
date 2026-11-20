import type { Health, Meta } from '../types'

export type PageKey = 'factory' | 'library' | 'compose' | 'paper' | 'help'

/** 界面展示用模型名：去掉内部引擎前缀 eino: */
export function displayLLM(name?: string) {
  if (!name || name === 'unconfigured') return ''
  return name.replace(/^eino:/, '')
}

export const PAGES: { key: PageKey; label: string; icon: string; group: string; desc: string }[] = [
  { key: 'factory', label: '因子工厂', icon: '⚙', group: '研究', desc: '用 DSL 编写因子并即时评价' },
  { key: 'compose', label: '因子组合', icon: '⊕', group: '研究', desc: '多因子加权合成与同质化检查' },
  { key: 'library', label: '因子库', icon: '▤', group: '研究', desc: '管理、批量评价、导入导出' },
  { key: 'paper', label: '实盘模拟', icon: '▣', group: '实盘', desc: '冻结因子逐日打分调仓' },
  { key: 'help', label: '帮助文档', icon: '?', group: '帮助', desc: '新手教程、名词解释、公式与算子参考' },
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
  onSettings,
}: {
  page: PageKey
  onNav: (p: PageKey) => void
  counts: Record<string, number>
  llm?: string
  onSettings?: () => void
}) {
  const online = !!llm && llm !== 'unconfigured'
  return (
    <aside className="sidebar">
      <div className="brand">
        <div className="name">
          <span className="mark">FL</span>
          factor-lab
        </div>
        <div className="tagline">AI 因子研究平台</div>
      </div>
      <nav>
        {GROUPS.map((g) => (
          <div key={g.name}>
            <div className="group-label">{g.name}</div>
            {g.keys.map((k) => {
              const p = PAGES.find((x) => x.key === k)!
              return (
                <button key={k} className={`nav-item ${page === k ? 'on' : ''}`} onClick={() => onNav(k)} title={p.desc}>
                  <span className="ico">{p.icon}</span>
                  <span>{p.label}</span>
                  {counts[k] ? <span className="badge">{counts[k]}</span> : null}
                </button>
              )
            })}
          </div>
        ))}
      </nav>
      <button className="nav-item sidebar-set" onClick={onSettings} title="配置 Tushare Token 与 AI 助手模型">
        <span className="ico">⚙</span>
        <span>设置</span>
        <span className={`dot ${online ? 'ok' : 'off'}`} style={{ marginLeft: 'auto' }} />
      </button>
      <div className="foot">
        <div>
          <span className={`dot ${online ? 'ok' : 'off'}`} />
          LLM {online ? displayLLM(llm) : '未配置'}
        </div>
        <div>数据源 · 本地面板</div>
        <div style={{ opacity: 0.7 }} title="前端构建时间，用于确认是否最新版本">
          build {__BUILD_ID__}
        </div>
      </div>
    </aside>
  )
}

export function Topbar({
  page,
  health,
  meta,
}: {
  page: PageKey
  health: Health | null
  meta: Meta | null
}) {
  const cur = PAGES.find((p) => p.key === page)!
  const srcShort = (s?: string) => (s ? s.split(':')[0] : '')
  return (
    <header className="topbar">
      <div className="title">{cur.label}</div>
      <div className="crumb">{cur.desc}</div>
      <div className="spacer" />
      {health && (
        <>
          <span className="pill ok" title={health.data_source}>
            <span className="dot ok" />
            {srcShort(health.data_source) || '行情'}
          </span>
          <span className="pill">
            面板 <b>{health.panel.dates}</b> 日 × <b>{health.panel.stocks}</b> 股
          </span>
          <span className="pill">
            {health.range.from} ~ {health.range.to}
          </span>
        </>
      )}
      {meta && (
        <span className="pill">
          字段 <b>{meta.fields.length}</b> · 算子 <b>{meta.operators.length}</b>
        </span>
      )}
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
      <div className="big">∅</div>
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
