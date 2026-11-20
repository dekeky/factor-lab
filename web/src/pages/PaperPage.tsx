import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import type { Combo, Factor, PaperAccount, PaperDay, PaperPick } from '../types'
import { PaperNavChart } from '../components/Charts'
import { DatePicker } from '../components/DatePicker'
import { Modal } from '../components/Modal'
import { Select } from '../components/Select'
import { KLineModal, StockName, type KLineTarget } from '../components/KLineModal'
import { Busy, Empty, PageHead, dt, fmt, pct, signCls } from '../components/Layout'

const STATUS: Record<string, string> = {
  selected: '入选',
  filled: '成交',
  skipped_suspend: '停牌',
  skipped_limit_up: '涨停',
  skipped_limit_down: '跌停',
  skipped_missing: '无行情',
  skipped_untradeable: '不可交易',
}

function money(v: number | undefined | null, digits = 0) {
  if (v === undefined || v === null || Number.isNaN(v)) return '—'
  const n = Math.abs(v) < Math.pow(10, -digits) / 2 ? 0 : v
  return n.toLocaleString('zh-CN', { maximumFractionDigits: digits, minimumFractionDigits: digits })
}

function statusTag(s: string) {
  const cls = s === 'selected' || s === 'filled' ? 'green' : s.startsWith('skipped') ? 'amber' : 'gray'
  return <span className={`tag ${cls}`}>{STATUS[s] ?? s}</span>
}

function paperSourceName(a: PaperAccount) {
  return a.combo_name || a.factor_name || '—'
}

function paperTitle(a: PaperAccount) {
  const src = paperSourceName(a)
  return src !== '—' ? src : a.name || '模拟盘'
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

function paperStatus(a: PaperAccount) {
  if (a.pending) return '待成交'
  if ((a.summary?.days ?? 0) > 0) return '运行中'
  return '未同步'
}

function paperNav(a: PaperAccount) {
  return a.summary?.nav ?? a.cash
}

export function PaperPage({ factors }: { factors: Factor[] }) {
  const [list, setList] = useState<PaperAccount[]>([])
  const [listReady, setListReady] = useState(false)
  const [acc, setAcc] = useState<PaperAccount | null>(null)
  const [sel, setSel] = useState<number | null>(null)
  const [dayKey, setDayKey] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [creating, setCreating] = useState(false)
  const [kline, setKline] = useState<KLineTarget | null>(null)
  const [inspect, setInspect] = useState(false)

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
      const last = a.pending?.date || a.days?.[a.days.length - 1]?.date || ''
      setDayKey(last)
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
    setDayKey('')
    setInspect(false)
  }

  useEffect(() => {
    loadList()
      .catch((e) => setErr((e as Error).message))
      .finally(() => setListReady(true))
  }, [])

  const day = useMemo(() => {
    if (!acc) return null
    if (acc.pending && dayKey === acc.pending.date) {
      return {
        pending: true,
        date: acc.pending.date,
        fill_date: acc.pending.fill_date || '',
        picks: acc.pending.items,
        skipped: acc.pending.skipped,
        holdings: acc.positions || [],
        trades: [] as PaperDay['trades'],
        nav: acc.summary?.nav ?? acc.cash,
        cash: acc.cash,
        ret: 0,
      }
    }
    return (acc.days ?? []).find((d) => d.date === dayKey) ?? null
  }, [acc, dayKey])

  const dayOpts = useMemo(() => {
    if (!acc) return []
    const days = [...(acc.days ?? [])].reverse().map((d) => ({ value: d.date, label: d.date }))
    if (acc.pending) {
      return [{ value: acc.pending.date, label: acc.pending.date + '（待成交）' }, ...days.filter((d) => d.value !== acc.pending?.date)]
    }
    return days
  }, [acc])

  async function onCreate(body: { name: string; factor_id?: number; combo_id?: number; top_n: number; buffer: number; capital: number; cost: number; start: string }) {
    setBusy(true)
    setErr('')
    try {
      const a = await api.createPaper(body)
      setCreating(false)
      setAcc(a)
      setSel(a.id)
      setDayKey(a.pending?.date || a.days?.[a.days.length - 1]?.date || '')
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

  const navDays = acc?.days ?? []

  const openInspect = (signalDate: string) => {
    if (!signalDate) return
    setDayKey(signalDate)
    setInspect(true)
  }

  const browsing = sel == null
  const sourceKind = acc?.combo_id ? '组合' : '因子'

  return (
    <>
      <PageHead
        title={acc ? paperTitle(acc) : '实盘模拟'}
        desc={
          acc
            ? `${sourceKind} · Top ${acc.config.top_n} · 阈值 ${acc.config.buffer ?? 0}`
            : '冻结因子或组合每日打分，选出 Top N。持仓排名跌出 Top N + 调仓阈值才换股；T+1 前复权开盘成交，停牌与涨跌停从股票池剔除。'
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
            <h3>把评价过的因子用起来</h3>
            <p>用虚拟资金按日线打分调仓，查看持仓与净值。选一个因子或组合，按 Top N 逐日记账。</p>
            <button className="btn primary" onClick={() => setCreating(true)}>
              新建模拟盘
            </button>
          </div>
        ) : (
          <div className="paper-gallery">
            {list.map((a) => (
              <PaperAccountCard key={a.id} account={a} onOpen={() => void open(a.id)} onDelete={() => void onDelete(a.id)} />
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
              <div className="num">{money(acc.summary?.nav)}</div>
              <div className="foot">本金 {money(acc.config.capital)}</div>
            </div>
            <div className="kpi">
              <div className="lbl">累计盈利</div>
              <div className={`num ${signCls(acc.summary?.pnl ?? acc.summary?.return)}`}>{money(acc.summary?.pnl)}</div>
              <div className="foot">
                {pct(acc.summary?.return, 2)} · {acc.summary?.days ?? 0} 个成交日
              </div>
            </div>
            <div className="kpi">
              <div className="lbl">最大回撤</div>
              <div className="num neg">{pct(acc.summary?.max_dd, 2)}</div>
              <div className="foot">持仓 {acc.summary?.holdings ?? 0} 只</div>
            </div>
            <div className="kpi">
              <div className="lbl">现金</div>
              <div className="num">{money(acc.cash)}</div>
              <div className="foot">
                单边成本 {(acc.config.cost * 100).toFixed(2)}%
                <button className="btn xs danger" style={{ marginLeft: 8 }} onClick={() => onDelete(acc.id)}>
                  删除
                </button>
              </div>
            </div>
          </div>

          {navDays.length > 0 && (
            <div className="card">
              <h3>
                净值曲线
                <span className="hint">红三角买入、绿三角卖出（换股日），点击查看当日明细</span>
                <span className="right">
                  <button type="button" className="btn xs" disabled={!dayKey} onClick={() => openInspect(dayKey)}>
                    当日明细
                  </button>
                </span>
              </h3>
              <PaperNavChart days={navDays} capital={acc.config.capital} onInspectDay={openInspect} />
            </div>
          )}

          <div className="card">
            <h3>
              每日选股
              <span className="hint">信号日打分，T+1 前复权开盘成交</span>
              <span className="right">
                <Select value={dayKey} onChange={setDayKey} items={dayOpts} minWidth={180} size="sm" />
              </span>
            </h3>
            {!day && <Empty text="这一天没有记录" />}
            {day && <PaperDayDetail day={day} onOpen={setKline} />}
          </div>
        </>
      ) : null}

      {creating && (
        <CreatePaperModal
          factors={factors}
          onClose={() => setCreating(false)}
          onCreate={onCreate}
          busy={busy}
        />
      )}
      {inspect && day && (
        <Modal
          title={`当日明细 · ${day.fill_date || day.date}`}
          subtitle={`信号日 ${day.date} · 净值 ${money(day.nav)}`}
          width={860}
          onClose={() => setInspect(false)}
          closeOnEscape={!kline}
        >
          <PaperDayDetail day={day} onOpen={setKline} />
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
  account: PaperAccount
  onOpen: () => void
  onDelete: () => void
}) {
  const title = paperTitle(account)
  const ret = account.summary?.return
  const synced = account.summary?.synced_at || account.updated_at
  return (
    <div className="paper-tile">
      <button type="button" className="paper-tile-body" onClick={onOpen}>
        <div className="paper-tile-title">{title}</div>
        <p className="paper-tile-sub">
          {money(paperNav(account))} · {paperStatus(account)}
        </p>
        <div className="paper-tile-chart">
          <PaperSpark days={account.days} capital={account.config.capital} />
        </div>
        <div className="paper-tile-meta">
          <div className="paper-tile-meta-left">
            <span>
              {account.combo_id ? '组合' : '因子'} · Top {account.config.top_n}
            </span>
            <span className="sep">·</span>
            <span className={signCls(ret)}>{signedPct(ret, 2)}</span>
          </div>
          {synced ? (
            <span className="time" title={dt(synced)}>
              上次同步 {galleryTime(synced)}
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

function PaperSpark({ days, capital }: { days?: PaperDay[]; capital: number }) {
  if (!days || days.length < 2 || !(capital > 0)) return null
  const values = days.map((d) => (d.nav / capital - 1) * 100)
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
  fill_date: string
  picks: PaperPick[]
  skipped?: PaperPick[]
  holdings: PaperDay['holdings']
  trades: PaperDay['trades']
  nav: number
  cash: number
  ret: number
}

function PaperDayDetail({ day, onOpen }: { day: DayView; onOpen: (row: KLineTarget) => void }) {
  return (
    <>
      {day.pending && (
        <div className="note info mb12">
          <span>ℹ</span>
          <div>
            {day.date} 的选股待下一交易日开盘成交
            {day.fill_date ? `（预计 ${day.fill_date}）` : ''}。
          </div>
        </div>
      )}
      <PickTable title="入选" rows={day.picks} onOpen={onOpen} />
      {(day.skipped?.length ?? 0) > 0 && <PickTable title="当日剔除（停牌 / 涨跌停）" rows={day.skipped!} onOpen={onOpen} />}
      <h4 className="paper-h4">持仓</h4>
      <div className="tbl-wrap mb12">
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
            {(day.holdings ?? []).length === 0 && (
              <tr>
                <td colSpan={7} className="muted">
                  空仓
                </td>
              </tr>
            )}
            {(day.holdings ?? []).map((h) => (
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
      {(day.trades ?? []).length > 0 && (
        <>
          <h4 className="paper-h4">成交 {day.fill_date}</h4>
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
                {(day.trades ?? []).map((tr, i) => (
                  <tr key={i}>
                    <td className="name">
                      <StockName row={tr} onOpen={onOpen} />
                    </td>
                    <td>{tr.side === 'buy' ? '买' : '卖'}</td>
                    <td className="num">{lots(tr.shares)}</td>
                    <td className="num">{tr.price ? fmt(tr.price, 3) : '—'}</td>
                    <td className="num">{tr.amount ? money(tr.amount) : '—'}</td>
                    <td className="num">{tr.cost ? money(tr.cost, 2) : '—'}</td>
                    <td>{statusTag(tr.status)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
    </>
  )
}

function PickTable({ title, rows, onOpen }: { title: string; rows: PaperPick[]; onOpen: (row: KLineTarget) => void }) {
  return (
    <>
      <h4 className="paper-h4">{title}</h4>
      <div className="tbl-wrap mb12">
        <table className="tbl">
          <thead>
            <tr>
              <th className="num">名次</th>
              <th>股票</th>
              <th className="num">得分</th>
              <th>状态</th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 && (
              <tr>
                <td colSpan={4} className="muted">
                  无
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
                <td>{statusTag(r.status)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  )
}

function CreatePaperModal({
  factors,
  onClose,
  onCreate,
  busy,
}: {
  factors: Factor[]
  onClose: () => void
  onCreate: (b: { name: string; factor_id?: number; combo_id?: number; top_n: number; buffer: number; capital: number; cost: number; start: string }) => void
  busy: boolean
}) {
  const usable = factors.filter((f) => f.available)
  const [combos, setCombos] = useState<Combo[]>([])
  const [source, setSource] = useState(usable[0] ? `f:${usable[0].id}` : '')
  const [topN, setTopN] = useState(10)
  const [buffer, setBuffer] = useState(0)
  const [capital, setCapital] = useState(1_000_000)
  const [cost, setCost] = useState(0.003)
  const [start, setStart] = useState('')

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
                start,
              })
            }
          >
            {busy ? '计算中…' : '开始模拟'}
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
        <label>起始日（可空）</label>
        <DatePicker value={start} onChange={setStart} placeholder="面板首日" />
      </div>
    </Modal>
  )
}
