import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import type { Combo, Factor, Meta, Paper, PaperFill, PaperIntent, PaperPick, PaperPosition } from '../types'
import { PaperNavChart } from '../components/Charts'
import { DatePicker } from '../components/DatePicker'
import { Modal } from '../components/Modal'
import { Select } from '../components/Select'
import { KLineModal, StockName, type KLineTarget } from '../components/KLineModal'
import { Busy, Empty, PageHead, dt, fmt, pct, signCls } from '../components/Layout'

const STATUS: Record<string, string> = {
  selected: '入选',
  filled: '成交',
  pending: '待成交',
  open: '开仓',
  exit: '清仓',
  add: '加仓',
  reduce: '减仓',
  hold: '不动',
  buy: '开仓',
  sell: '清仓',
  skipped_suspend: '停牌',
  skipped_limit_up: '涨停',
  skipped_limit_down: '跌停',
  skipped_missing: '无行情',
  skipped_untradeable: '不可交易',
}

type DayTab = 'plan' | 'picks' | 'trades' | 'holdings'

function money(v: number | undefined | null, digits = 0) {
  if (v === undefined || v === null || Number.isNaN(v)) return '—'
  const n = Math.abs(v) < Math.pow(10, -digits) / 2 ? 0 : v
  return n.toLocaleString('zh-CN', { maximumFractionDigits: digits, minimumFractionDigits: digits })
}

function statusTag(s: string) {
  if (!s) return null
  const cls = s === 'selected' || s === 'filled' || s === 'enter' ? 'green' : s.startsWith('skipped') || s === 'pending' ? 'amber' : 'gray'
  return <span className={`tag ${cls}`}>{STATUS[s] ?? s}</span>
}

function paperSourceName(p: Paper) {
  const a = p.account
  return a.combo_name || a.factor_name || '—'
}

function paperTitle(p: Paper) {
  const src = paperSourceName(p)
  return src !== '—' ? src : p.account.name || '模拟盘'
}

function lots(v: number | undefined | null) {
  if (v === undefined || v === null || Number.isNaN(v) || v === 0) return '—'
  return Math.round(v).toLocaleString('zh-CN')
}

function signedPct(v: number | undefined | null, digits = 2) {
  if (v === undefined || v === null || Number.isNaN(v)) return '—'
  const n = v * 100
  const prefix = n > 0 ? '+' : ''
  return prefix + n.toFixed(digits) + '%'
}

function galleryTime(iso?: string) {
  if (!iso) return ''
  const ts = new Date(iso).getTime()
  if (Number.isNaN(ts)) return ''
  const diff = Date.now() - ts
  if (diff < 0) return '刚刚'
  if (diff >= 3 * 24 * 60 * 60 * 1000) return dt(iso)
  const minutes = Math.floor(diff / 60_000)
  if (minutes < 1) return '刚刚'
  if (minutes < 60) return `${minutes} 分钟前`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours} 小时前`
  return `${Math.floor(hours / 24)} 天前`
}

function paperStatus(p: Paper) {
  if ((p.plans ?? []).some((x) => x.status === 'pending')) return '待成交'
  if ((p.snapshots ?? []).length > 1) return '运行中'
  return '排队中'
}

function paperNav(p: Paper) {
  const snaps = p.snapshots ?? []
  return snaps[snaps.length - 1]?.nav ?? p.book.cash
}

function paperReturn(p: Paper) {
  const cap = p.account.config.capital
  if (!cap) return 0
  return paperNav(p) / cap - 1
}

function paperMaxDD(p: Paper) {
  let peak = 0
  let dd = 0
  for (const s of p.snapshots ?? []) {
    if (s.nav > peak) peak = s.nav
    if (peak > 0) dd = Math.min(dd, s.nav / peak - 1)
  }
  return dd
}

function ymd(s: string | undefined): string {
  return (s || '').slice(0, 10)
}

function fillDays(p: Paper): number {
  const days = new Set<string>()
  for (const f of p.fills ?? []) {
    if (f.status === 'filled' && f.shares > 0) days.add(ymd(f.date))
  }
  return days.size
}

export function PaperPage({ factors, meta }: { factors: Factor[]; meta: Meta | null }) {
  const [list, setList] = useState<Paper[]>([])
  const [listReady, setListReady] = useState(false)
  const [acc, setAcc] = useState<Paper | null>(null)
  const [sel, setSel] = useState<number | null>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [creating, setCreating] = useState(false)
  const [kline, setKline] = useState<KLineTarget | null>(null)
  const [inspectKey, setInspectKey] = useState('')

  const loadList = async () => {
    const rows = await api.papers()
    setList(rows)
    return rows
  }

  const open = async (id: number) => {
    setSel(id)
    setBusy(true)
    setErr('')
    try {
      const a = await api.paper(id)
      setAcc(a)
      await loadList()
    } catch (e) {
      setErr((e as Error).message)
      setSel(null)
      setAcc(null)
    } finally {
      setBusy(false)
    }
  }

  const close = () => {
    setAcc(null)
    setSel(null)
    setInspectKey('')
  }

  useEffect(() => {
    loadList()
      .catch((e) => setErr((e as Error).message))
      .finally(() => setListReady(true))
  }, [])

  const latestKey = acc?.snapshots?.length ? acc.snapshots[acc.snapshots.length - 1].date : ''
  const latestDay = useMemo(() => (acc && latestKey ? dayFromSnapshot(acc, latestKey) : null), [acc, latestKey])
  const inspectDay = useMemo(() => (acc && inspectKey ? dayFromSnapshot(acc, inspectKey) : null), [acc, inspectKey])

  async function onCreate(body: { name: string; factor_id?: number; combo_id?: number; top_n: number; buffer: number; capital: number; cost: number; start: string }) {
    setBusy(true)
    setErr('')
    try {
      const a = await api.createPaper(body)
      setCreating(false)
      setAcc(a)
      setSel(a.account.id)
      await loadList()
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  async function onSync() {
    if (!sel) return
    setBusy(true)
    setErr('')
    try {
      const a = await api.syncPaper(sel)
      setAcc(a)
      await loadList()
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  async function onDelete(id: number) {
    if (!confirm('删除该模拟盘？账本不可恢复。')) return
    setBusy(true)
    setErr('')
    try {
      await api.deletePaper(id)
      if (sel === id) {
        setAcc(null)
        setSel(null)
      }
      await loadList()
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const navDays = acc?.snapshots ?? []

  const openInspect = (date: string) => {
    if (!date || !acc) return
    setInspectKey(date)
  }

  const browsing = sel == null
  const sourceKind = acc?.account.combo_id ? '组合' : '因子'

  return (
    <>
      <PageHead
        title={acc ? paperTitle(acc) : '实盘模拟'}
        desc={
          acc
            ? `${sourceKind} · Top ${acc.account.config.top_n} · 阈值 ${acc.account.config.rebalance_threshold ?? 0}`
            : '用虚拟资金按日选股、调仓，看账户净值怎么变。'
        }
        actions={
          <>
            {!browsing && (
              <button className="btn ghost" onClick={close}>
                ← 模拟盘列表
              </button>
            )}
            <button className="btn" disabled={browsing || busy} onClick={onSync}>
              同步新行情
            </button>
            <button className="btn primary" onClick={() => setCreating(true)}>
              新建模拟盘
            </button>
          </>
        }
      />

      {err && (
        <div className="note err mb12">
          <span>⚠</span>
          <div>{err}</div>
        </div>
      )}

      {browsing ? (
        !listReady ? (
          <Busy text="正在加载模拟盘…" />
        ) : list.length === 0 ? (
          <div className="paper-empty">
            <div className="paper-empty-icon">▣</div>
            <h3>还没有模拟盘</h3>
            <p>选一个评价过的因子或组合，用虚拟资金按日选股，看净值怎么走。</p>
            <button className="btn primary" onClick={() => setCreating(true)}>
              新建模拟盘
            </button>
          </div>
        ) : (
          <div className="paper-gallery">
            {list.map((a) => (
              <PaperAccountCard key={a.account.id} account={a} onOpen={() => void open(a.account.id)} onDelete={() => void onDelete(a.account.id)} />
            ))}
            <button type="button" className="paper-tile paper-tile-new" onClick={() => setCreating(true)}>
              <span className="plus">+</span>
              <span>新建模拟盘</span>
            </button>
          </div>
        )
      ) : busy && !acc ? (
        <Busy text="正在计算模拟盘…" />
      ) : acc ? (
        <>
          <div className="grid c4 mb12">
            <div className="kpi">
              <div className="lbl">净值</div>
              <div className="num">{money(paperNav(acc))}</div>
              <div className="foot">本金 {money(acc.account.config.capital)}</div>
            </div>
            <div className="kpi">
              <div className="lbl">累计盈利</div>
              <div className={`num ${signCls(paperReturn(acc))}`}>{money(paperNav(acc) - acc.account.config.capital)}</div>
              <div className="foot">
                {pct(paperReturn(acc), 2)} · {fillDays(acc)} 个成交日
              </div>
            </div>
            <div className="kpi">
              <div className="lbl">最大回撤</div>
              <div className="num neg">{pct(paperMaxDD(acc), 2)}</div>
              <div className="foot">持仓 {(acc.book.positions ?? []).filter((h) => h.shares > 0).length} 只</div>
            </div>
            <div className="kpi">
              <div className="lbl">现金</div>
              <div className="num">{money(acc.book.cash)}</div>
              <div className="foot">
                单边成本 {(acc.account.config.cost * 100).toFixed(2)}%
                <button className="btn xs danger" style={{ marginLeft: 8 }} onClick={() => onDelete(acc.account.id)}>
                  删除
                </button>
              </div>
            </div>
          </div>

          {navDays.length > 0 && (
            <div className="card">
              <h3>
                净值曲线
                <span className="hint">
                  红三角买入、绿三角卖出（换股日），点击查看当日明细
                  {navDays.length > 0 ? ` · 截至 ${navDays[navDays.length - 1].date}` : ''}
                </span>
                <span className="right">
                  <button type="button" className="btn xs" disabled={!latestKey} onClick={() => openInspect(latestKey)}>
                    当日明细
                  </button>
                </span>
              </h3>
              <PaperNavChart snapshots={navDays} fills={acc.fills} capital={acc.account.config.capital} onInspectDay={openInspect} />
            </div>
          )}

          <div className="card">
            <h3>
              当日明细
              <span className="hint">
                {latestKey ? `${latestKey} · ` : ''}
                收盘出选股和计划，下一交易日开盘成交；持仓与成交以当天账本为准
              </span>
            </h3>
            {!latestDay && <Empty text="这一天没有记录" />}
            {latestDay && <PaperDayDetail day={latestDay} onOpen={setKline} />}
          </div>
        </>
      ) : null}

      {creating && (
        <CreatePaperModal
          factors={factors}
          lastTradingDay={meta?.range?.to ?? ''}
          onClose={() => setCreating(false)}
          onCreate={onCreate}
          busy={busy}
        />
      )}
      {inspectDay && (
        <Modal
          title={`当日明细 · ${inspectDay.date}`}
          subtitle={inspectDay.exec_at ? `计划成交 ${inspectDay.exec_at} · 净值 ${money(inspectDay.nav)}` : `净值 ${money(inspectDay.nav)}`}
          width={860}
          onClose={() => setInspectKey('')}
          closeOnEscape={!kline}
        >
          <PaperDayDetail day={inspectDay} onOpen={setKline} />
        </Modal>
      )}
      {kline && <KLineModal target={kline} onClose={() => setKline(null)} />}
    </>
  )
}

function PaperAccountCard({
  account,
  onOpen,
  onDelete,
}: {
  account: Paper
  onOpen: () => void
  onDelete: () => void
}) {
  const title = paperTitle(account)
  const ret = paperReturn(account)
  const synced = account.account.updated_at
  return (
    <div className="paper-tile">
      <button type="button" className="paper-tile-body" onClick={onOpen}>
        <div className="paper-tile-title">{title}</div>
        <div className={`paper-tile-ret ${signCls(ret)}`}>{signedPct(ret, 2)}</div>
        <div className="paper-tile-chart">
          <PaperSpark snapshots={account.snapshots} capital={account.account.config.capital} />
        </div>
        <div className="paper-tile-meta">
          <div className="paper-tile-meta-left">
            <span>
              {money(paperNav(account))} · {paperStatus(account)}
            </span>
            <span className="sep">·</span>
            <span>
              {account.account.combo_id ? '组合' : '因子'} Top {account.account.config.top_n}
            </span>
          </div>
          {synced ? (
            <span className="time" title={dt(synced)}>
              开户 {galleryTime(synced)}
            </span>
          ) : null}
        </div>
      </button>
      <button
        type="button"
        className="paper-tile-del"
        title="删除"
        aria-label={`删除 ${title}`}
        onClick={(e) => {
          e.stopPropagation()
          onDelete()
        }}
      >
        ×
      </button>
    </div>
  )
}

function PaperSpark({ snapshots, capital }: { snapshots?: { date: string; nav: number }[]; capital: number }) {
  if (!snapshots || snapshots.length < 2 || !(capital > 0)) return null
  const values = snapshots.map((d) => (d.nav / capital - 1) * 100)
  const last = values[values.length - 1] ?? 0
  const min = Math.min(0, ...values)
  const max = Math.max(0, ...values)
  const span = max - min || 1
  const width = 160
  const height = 56
  const coords = values.map((value, index) => {
    const x = (index / (values.length - 1)) * width
    const y = height - ((value - min) / span) * (height - 6) - 3
    return { x, y }
  })
  const line = coords.map((p, i) => `${i === 0 ? 'M' : 'L'}${p.x.toFixed(2)} ${p.y.toFixed(2)}`).join(' ')
  const area = `${line} L${width} ${height} L0 ${height} Z`
  return (
    <svg viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none" className={signCls(last)} aria-hidden>
      <path d={area} fill="currentColor" opacity="0.14" />
      <path d={line} fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  )
}

function comboUsable(c: Combo) {
  return c.items.length >= 2 && c.items.every((it) => it.available && !it.missing)
}

type DayView = {
  pending?: boolean
  date: string
  exec_at: string
  picks: PaperPick[]
  intents: PaperIntent[]
  holdings: PaperPosition[]
  trades: PaperFill[]
  nav: number
  cash: number
  ret: number
}

function dayFromSnapshot(p: Paper, date: string): DayView | null {
  if (!date) return null
  const snap = (p.snapshots ?? []).find((s) => s.date === date)
  const plan = (p.plans ?? []).find((x) => x.signal_date === date)
  if (!snap && !plan) return null
  return {
    pending: plan?.status === 'pending',
    date,
    exec_at: plan?.exec_at || '',
    picks: plan?.picks ?? [],
    intents: plan?.intents ?? [],
    holdings: snap?.holdings ?? [],
    trades: (p.fills ?? []).filter((f) => ymd(f.date) === date),
    nav: snap?.nav ?? 0,
    cash: snap?.cash ?? 0,
    ret: snap?.ret ?? 0,
  }
}

function PaperDayDetail({ day, onOpen }: { day: DayView; onOpen: (row: KLineTarget) => void }) {
  const [tab, setTab] = useState<DayTab>('plan')
  return (
    <>
      <div className="tabs-bar">
        <button type="button" className={tab === 'plan' ? 'on' : ''} onClick={() => setTab('plan')}>
          交易计划
        </button>
        <button type="button" className={tab === 'picks' ? 'on' : ''} onClick={() => setTab('picks')}>
          每日选股
        </button>
        <button type="button" className={tab === 'trades' ? 'on' : ''} onClick={() => setTab('trades')}>
          交易记录
        </button>
        <button type="button" className={tab === 'holdings' ? 'on' : ''} onClick={() => setTab('holdings')}>
          每日持仓
        </button>
      </div>
      {tab === 'plan' && <PlanTable rows={day.intents} onOpen={onOpen} />}
      {tab === 'picks' && <PickTable rows={day.picks} onOpen={onOpen} />}
      {tab === 'trades' && <TradeTable rows={day.trades ?? []} pending={!!day.pending} onOpen={onOpen} />}
      {tab === 'holdings' && <HoldingTable rows={day.holdings ?? []} onOpen={onOpen} />}
    </>
  )
}

function PlanTable({ rows, onOpen }: { rows: PaperIntent[]; onOpen: (row: KLineTarget) => void }) {
  return (
    <div className="tbl-wrap">
      <table className="tbl">
        <thead>
          <tr>
            <th className="num">名次</th>
            <th>股票</th>
            <th>方向</th>
            <th className="num">计划股数</th>
            <th className="num">实际股数</th>
            <th className="num">得分</th>
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 && (
            <tr>
              <td colSpan={6} className="muted">
                无交易计划
              </td>
            </tr>
          )}
          {rows.map((r) => (
            <tr key={r.code}>
              <td className="num">{r.rank || '—'}</td>
              <td className="name">
                <StockName row={r} onOpen={onOpen} />
              </td>
              <td>{STATUS[r.side] ?? r.side}</td>
              <td className="num">{lots(r.shares)}</td>
              <td className="num">{lots(r.actual_shares)}</td>
              <td className="num">{fmt(r.score, 4)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function PickTable({ rows, onOpen }: { rows: PaperPick[]; onOpen: (row: KLineTarget) => void }) {
  return (
    <div className="tbl-wrap">
      <table className="tbl even-3">
        <thead>
          <tr>
            <th className="num">名次</th>
            <th>股票</th>
            <th className="num">得分</th>
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 && (
            <tr>
              <td colSpan={3} className="muted">
                无选股记录
              </td>
            </tr>
          )}
          {rows.map((r) => (
            <tr key={r.code}>
              <td className="num">{r.rank}</td>
              <td className="name">
                <StockName row={r} onOpen={onOpen} />
              </td>
              <td className="num">{fmt(r.score, 4)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function HoldingTable({ rows, onOpen }: { rows: PaperPosition[]; onOpen: (row: KLineTarget) => void }) {
  return (
    <div className="tbl-wrap">
      <table className="tbl">
        <thead>
          <tr>
            <th>股票</th>
            <th className="num">股数</th>
            <th className="num">买入价</th>
            <th className="num">现价</th>
            <th className="num">累计盈利</th>
            <th className="num">市值</th>
            <th className="num">权重</th>
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 && (
            <tr>
              <td colSpan={7} className="muted">
                空仓
              </td>
            </tr>
          )}
          {rows.map((h) => (
            <tr key={h.code}>
              <td className="name">
                <StockName row={h} onOpen={onOpen} />
              </td>
              <td className="num">{lots(h.shares)}</td>
              <td className="num">{fmt(h.cost, 3)}</td>
              <td className="num">{fmt(h.price, 3)}</td>
              <td className={`num ${signCls(h.pnl)}`}>{money(h.pnl)}</td>
              <td className="num">{money(h.value)}</td>
              <td className="num">{pct(h.weight, 1)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function TradeTable({
  rows,
  pending,
  onOpen,
}: {
  rows: PaperFill[]
  pending?: boolean
  onOpen: (row: KLineTarget) => void
}) {
  return (
    <div className="tbl-wrap">
      <table className="tbl">
        <thead>
          <tr>
            <th>股票</th>
            <th>方向</th>
            <th className="num">股数</th>
            <th className="num">价格</th>
            <th className="num">金额</th>
            <th className="num">费用</th>
            <th>状态</th>
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 && (
            <tr>
              <td colSpan={7} className="muted">
                {pending ? '无调仓' : '无成交'}
              </td>
            </tr>
          )}
          {rows.map((tr, i) => (
            <tr key={i}>
              <td className="name">
                <StockName row={tr} onOpen={onOpen} />
              </td>
              <td>{tr.side === 'buy' ? '买' : '卖'}</td>
              <td className="num">{lots(tr.shares)}</td>
              <td className="num">{tr.price ? fmt(tr.price, 3) : '—'}</td>
              <td className="num">{tr.amount ? money(tr.amount) : '—'}</td>
              <td className="num">{tr.fee ? money(tr.fee, 2) : '—'}</td>
              <td>{statusTag(tr.status)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function CreatePaperModal({
  factors,
  lastTradingDay,
  onClose,
  onCreate,
  busy,
}: {
  factors: Factor[]
  lastTradingDay: string
  onClose: () => void
  onCreate: (b: { name: string; factor_id?: number; combo_id?: number; top_n: number; buffer: number; capital: number; cost: number; start: string }) => void
  busy: boolean
}) {
  const usable = factors.filter((f) => f.available)
  const [combos, setCombos] = useState<Combo[]>([])
  const [source, setSource] = useState(usable[0] ? `f:${usable[0].id}` : '')
  const [topN, setTopN] = useState(10)
  const [buffer, setBuffer] = useState(20)
  const [capital, setCapital] = useState(1_000_000)
  const [cost, setCost] = useState(0.003)
  const [start, setStart] = useState(lastTradingDay)

  useEffect(() => {
    if (lastTradingDay) setStart((cur) => cur || lastTradingDay)
  }, [lastTradingDay])

  useEffect(() => {
    api.combos().then((rows) => {
      setCombos(rows)
      setSource((cur) => {
        if (cur) return cur
        const c = rows.find(comboUsable)
        return c ? `c:${c.id}` : ''
      })
    }).catch(() => {})
  }, [])

  const usableCombos = combos.filter(comboUsable)
  const factorId = source.startsWith('f:') ? Number(source.slice(2)) : 0
  const comboId = source.startsWith('c:') ? Number(source.slice(2)) : 0
  const f = usable.find((x) => x.id === factorId)
  const c = usableCombos.find((x) => x.id === comboId)
  const sourceName = f?.name || c?.name || ''

  return (
    <Modal
      title="新建模拟盘"
      subtitle="冻结当前因子或组合表达式，之后逐日打分。持仓只有排名跌出 Top N + 调仓阈值才换成新股；已发生的成交不会因重算改写。"
      width={480}
      onClose={onClose}
      footer={
        <>
          <button className="btn" onClick={onClose}>
            取消
          </button>
          <button
            className="btn primary"
            disabled={!source || busy || topN < 1}
            onClick={() =>
              onCreate({
                name: sourceName ? sourceName + ' 模拟盘' : '',
                factor_id: factorId || undefined,
                combo_id: comboId || undefined,
                top_n: topN,
                buffer,
                capital,
                cost,
                start: start || lastTradingDay,
              })
            }
          >
            {busy ? '开户中…' : '开始模拟'}
          </button>
        </>
      }
    >
      <div className="field mb12">
        <label>信号来源</label>
        <Select
          value={source}
          onChange={setSource}
          groups={[
            ...(usableCombos.length
              ? [{ label: '因子组合', items: usableCombos.map((x) => ({ value: `c:${x.id}`, label: x.name, hint: `${x.items.length} 个因子` })) }]
              : []),
            { label: '单因子', items: usable.map((x) => ({ value: `f:${x.id}`, label: x.name, hint: x.category })) },
          ]}
          placeholder="选择因子或组合"
        />
      </div>
      <div className="row mb12">
        <div className="field" style={{ flex: 1 }}>
          <label>Top N</label>
          <input type="number" min={1} max={200} value={topN} onChange={(e) => setTopN(Math.max(1, Number(e.target.value) || 1))} />
        </div>
        <div className="field" style={{ flex: 1 }}>
          <label>调仓阈值</label>
          <input
            type="number"
            min={0}
            max={200}
            value={buffer}
            title="持仓排名跌出 Top N + 该值后才换股。0 表示刚跌出 Top N 就换。"
            onChange={(e) => setBuffer(Math.max(0, Number(e.target.value) || 0))}
          />
        </div>
      </div>
      <div className="row mb12">
        <div className="field" style={{ flex: 1 }}>
          <label>本金</label>
          <input type="number" min={1} value={capital} onChange={(e) => setCapital(Number(e.target.value) || 0)} />
        </div>
        <div className="field" style={{ flex: 1 }}>
          <label>单边成本</label>
          <input type="number" step={0.0001} min={0} value={cost} onChange={(e) => setCost(Number(e.target.value) || 0)} />
        </div>
      </div>
      <div className="field mb12">
        <label>起始日</label>
        <DatePicker value={start} onChange={setStart} placeholder="当前交易日" clearable={false} />
      </div>
    </Modal>
  )
}
