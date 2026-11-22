import { Fragment, useEffect, useMemo, useRef, useState } from 'react'
import { api } from '../api'
import type { Combo, ComboItem, ComboProposal, ComposeResult, Factor, Health, Meta, Summary } from '../types'
import { AssistPanel } from '../components/AssistPanel'
import { AiIcon } from '../components/AiIcon'
import { Modal } from '../components/Modal'
import { defaultEvalRange, ParamsPanel, type Params } from '../components/FactoryPanels'
import { FactoryResult } from '../components/FactoryResult'
import { Select } from '../components/Select'
import { catLabel, fmt, pct, signCls } from '../components/Layout'
import { colorAlpha, cssVar } from '../lib/theme'
import { exprBrief, fmtTime } from '../lib/evalHistory'
import { samePool, universeName } from '../lib/universes'

function clamp(v: number, lo: number, hi: number) {
  return Math.max(lo, Math.min(hi, v))
}

function stored(key: string, fallback: number) {
  try {
    const v = Number(localStorage.getItem(key))
    return Number.isFinite(v) && v > 0 ? v : fallback
  } catch {
    return fallback
  }
}

function storedStr(key: string, fallback = '') {
  try {
    return localStorage.getItem(key) || fallback
  } catch {
    return fallback
  }
}

const COMBO_SUGGESTIONS = ['帮我挑几个经济含义互补的因子', '解释当前组合的成分与权重', '给当前组合一个变体']
const COMBO_QUICK = ['解释当前组合', '优化权重配比', '推荐补充因子']
const MIN_FACTORS = 2

function md(iso?: string) {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '—'
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

function topAnn(s?: Summary) {
  const xs = s?.layer_mean
  if (!xs?.length) return undefined
  return xs[xs.length - 1] * 252
}

function poolAgeLabel(iso?: string) {
  if (!iso) return '池龄 0 月'
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return '池龄 0 月'
  const months = Math.max(0, Math.floor((Date.now() - t) / (30.4375 * 24 * 3600 * 1000)))
  return `池龄 ${months} 月`
}

/** 编辑器标签页：每个打开的组合一份独立上下文（与因子工厂的 FileTab 同构） */
interface ComboTab {
  key: string
  name: string
  method: 'equal' | 'manual'
  items: ComboItem[]
  comboId: number | null
  dirty: boolean
  lastExpr?: string
}

function newTab(name: string): ComboTab {
  return {
    key: `c${Date.now()}${Math.random().toString(36).slice(2, 6)}`,
    name,
    method: 'equal',
    items: [],
    comboId: null,
    dirty: false,
  }
}

interface ResultView {
  name: string
  result: ComposeResult
  params: Params
  ts: number
  durationMs: number
}

function corrBg(v: number) {
  const a = Math.min(1, Math.abs(v))
  if (v >= 0.7) return colorAlpha(cssVar('--up', '#f07171'), 0.1 + a * 0.35)
  if (v >= 0.4) return colorAlpha(cssVar('--warn', '#f5a524'), 0.08 + a * 0.22)
  return colorAlpha(cssVar('--down', '#3dd68c'), 0.05 + a * 0.12)
}

/**
 * 因子组合：Cursor 式 IDE 工作区（与因子工厂同构）。
 *
 * 左列 = 我的组合列表（未保存草稿 + 已保存组合）
 * 中列 = 组合编辑器（名称 / 权重方式 / 成分因子选择）
 * 右列 = 全高 AI 对话助手，宽度可拖拽。
 * 评价参数在点击「▶ 运行评价」时弹窗选择，结果以弹窗展示（复用工厂组件）。
 */
export function ComposePage({
  factors,
  meta,
  health,
  llm,
}: {
  factors: Factor[]
  meta: Meta | null
  health: Health | null
  llm: string
  onChanged: () => void
}) {
  const [tabs, setTabs] = useState<ComboTab[]>(() => [newTab('未命名组合')])
  const [activeKey, setActiveKey] = useState('')
  const [combos, setCombos] = useState<Combo[]>([])
  const [listFilter, setListFilter] = useState('')
  const [q, setQ] = useState('')
  const [params, setParams] = useState<Params>({
    horizon: 1, groups: 10, cost: 0.3, start: '', end: '', universe: storedStr('fw.evalUniverse'),
  })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [msg, setMsg] = useState('')
  const [chatWidth, setChatWidth] = useState(() => stored('cc.chatWidth', 320))
  const [chatOpen, setChatOpen] = useState(true)
  const [runOpen, setRunOpen] = useState(false)
  const [addOpen, setAddOpen] = useState(false)
  const [openRow, setOpenRow] = useState<number | null>(null)
  const [confirmDel, setConfirmDel] = useState(false)
  const [resultView, setResultView] = useState<ResultView | null>(null)
  const dragRef = useRef<{ start: number; base: number } | null>(null)

  const active = tabs.find((t) => t.key === activeKey) ?? tabs[0]
  const factorById = useMemo(() => new Map(factors.map((f) => [f.id, f])), [factors])
  const totalWeight = useMemo(
    () => active.items.reduce((sum, item) => sum + Math.max(0, active.method === 'equal' ? 1 : item.weight), 0),
    [active.items, active.method],
  )
  const savedCombo = combos.find((c) => c.id === active.comboId)
  const categoryTags = useMemo(() => {
    const seen = new Set<string>()
    const tags: string[] = []
    for (const item of active.items) {
      const cat = factorById.get(item.id)?.category
      if (!cat || seen.has(cat)) continue
      seen.add(cat)
      tags.push(catLabel(cat))
    }
    return tags.slice(0, 3)
  }, [active.items, factorById])

  useEffect(() => {
    if (!tabs.find((t) => t.key === activeKey)) setActiveKey(tabs[0].key)
  }, [tabs, activeKey])

  function loadCombos() {
    api.combos().then(setCombos).catch(() => {})
  }
  useEffect(() => { loadCombos() }, [])

  // 评价区间默认最近五年（夹在数据面板范围内）；股票池默认取当前面板
  useEffect(() => {
    if (!meta?.range) return
    const { start, end } = defaultEvalRange(meta.range)
    setParams((p) => ({ ...p, start: p.start || start, end: p.end || end }))
  }, [meta])
  useEffect(() => {
    const pool = health?.stock_pool
    if (!pool) return
    setParams((p) => ({ ...p, universe: p.universe || pool }))
  }, [health])
  useEffect(() => {
    if (!params.universe) return
    try { localStorage.setItem('fw.evalUniverse', params.universe) } catch { /* 忽略 */ }
  }, [params.universe])

  // 分隔条拖拽：x 调整右侧对话宽度
  useEffect(() => {
    const move = (e: PointerEvent) => {
      const d = dragRef.current
      if (!d) return
      const v = clamp(d.base + (d.start - e.clientX), 300, 680)
      setChatWidth(v)
      try { localStorage.setItem('cc.chatWidth', String(v)) } catch { /* 忽略隐私模式 */ }
    }
    const up = () => {
      dragRef.current = null
      document.body.classList.remove('fw-dragging-x')
    }
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', up)
    return () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', up)
    }
  }, [])

  function startDrag(e: React.PointerEvent<HTMLDivElement>) {
    e.preventDefault()
    dragRef.current = { start: e.clientX, base: chatWidth }
    document.body.classList.add('fw-dragging-x')
  }

  /** 左侧列表：已保存组合（搜索过滤，按更新时间倒序） */
  const savedCombos = useMemo(() => {
    const kw = listFilter.trim().toLowerCase()
    return combos
      .filter((c) => !kw || c.name.toLowerCase().includes(kw))
      .sort((a, b) => (b.updated_at > a.updated_at ? 1 : -1))
  }, [combos, listFilter])

  /** 未保存草稿（tab 且未入库） */
  const draftRows = useMemo(() => {
    const kw = listFilter.trim().toLowerCase()
    return tabs
      .filter((t) => t.comboId === null && (!kw || t.name.toLowerCase().includes(kw)))
      .map((t) => ({ key: t.key, name: t.name, dirty: t.dirty, active: t.key === active.key }))
  }, [tabs, listFilter, active.key])

  const savedRows = useMemo(
    () =>
      savedCombos.map((c) => {
        const t = tabs.find((x) => x.comboId === c.id)
        return { combo: c, open: !!t, active: t?.key === active.key, dirty: !!t?.dirty }
      }),
    [savedCombos, tabs, active.key],
  )

  /** 候选因子：未入选 + 搜索过滤 */
  const candidates = useMemo(() => {
    const kw = q.trim().toLowerCase()
    const picked = new Set(active?.items.map((i) => i.id) ?? [])
    return factors
      .filter((f) => !picked.has(f.id))
      .filter((f) => !kw || f.name.toLowerCase().includes(kw) || catLabel(f.category).toLowerCase().includes(kw))
      .sort((a, b) => {
        if (a.available !== b.available) return a.available ? -1 : 1
        if (!!a.summary !== !!b.summary) return a.summary ? -1 : 1
        return Math.abs(b.summary?.rank_ic_mean ?? 0) - Math.abs(a.summary?.rank_ic_mean ?? 0)
      })
      .slice(0, 80)
  }, [factors, q, active])

  /** 发给 AI 的上下文：已选因子（含权重方向）+ 因子库候选（含指标）+ 最近评价摘要 */
  const assistCtx = useMemo(() => {
    if (!active) return ''
    const lines = [
      `组合「${active.name}」（${active.method === 'equal' ? '等权' : '自定义权重'}）`,
      `【已选因子】${active.items.length} 个：`,
    ]
    for (const it of active.items) {
      lines.push(`- #${it.id} ${it.name} 权重 ${it.weight} ${it.direction < 0 ? '反向' : '正向'}`)
    }
    if (active.items.length === 0) lines.push('（暂无，等待选择）')

    const picked = new Set(active.items.map((i) => i.id))
    const pool = factors
      .filter((f) => f.available && !picked.has(f.id))
      .sort((a, b) => Math.abs(b.summary?.rank_ic_mean ?? 0) - Math.abs(a.summary?.rank_ic_mean ?? 0))
      .slice(0, 60)
    lines.push(`【候选因子】因子库可加入 ${pool.length} 个（按 |RankIC| 降序，用 id 引用）：`)
    for (const f of pool) {
      lines.push(`- #${f.id} ${f.name}（${catLabel(f.category)}）${f.summary ? `RankIC ${fmt(f.summary.rank_ic_mean, 3)} · ICIR ${fmt(f.summary.rank_ic_ir, 2)}` : '未评价'}`)
    }

    const saved = combos.find((c) => c.id === active.comboId)
    if (saved?.summary) {
      lines.push(`【最近评价】RankIC ${fmt(saved.summary.rank_ic_mean, 3)} · ICIR ${fmt(saved.summary.rank_ic_ir, 2)} · ${saved.summary.from} ~ ${saved.summary.to}`)
    }
    if (active.lastExpr) {
      lines.push('【合成表达式】')
      lines.push(active.lastExpr)
    }
    return lines.join('\n')
  }, [active, combos, factors])

  function patchActive(patch: Partial<ComboTab>) {
    setTabs((ts) => ts.map((t) => (t.key === active.key ? { ...t, ...patch } : t)))
  }

  function switchTab(key: string) {
    if (key === active.key) return
    setActiveKey(key)
    setQ('')
    setError('')
    setMsg('')
    setConfirmDel(false)
    setOpenRow(null)
    setAddOpen(false)
  }

  function closeTab(key: string) {
    setTabs((ts) => {
      const idx = ts.findIndex((t) => t.key === key)
      const next = ts.filter((t) => t.key !== key)
      if (!next.length) {
        const fresh = newTab('未命名组合')
        setActiveKey(fresh.key)
        return [fresh]
      }
      if (key === activeKey) setActiveKey(next[Math.min(idx, next.length - 1)].key)
      return next
    })
    setError('')
    setMsg('')
    setConfirmDel(false)
    setOpenRow(null)
    setAddOpen(false)
  }

  function addTab() {
    const used = new Set(tabs.map((t) => t.name))
    let n = 1
    let name = '未命名组合'
    while (used.has(name)) {
      n += 1
      name = `未命名组合 ${n}`
    }
    const t = newTab(name)
    setTabs((ts) => [...ts, t])
    setActiveKey(t.key)
    setError('')
    setMsg('')
    setConfirmDel(false)
    setOpenRow(null)
    setAddOpen(false)
  }

  function openCombo(c: Combo) {
    const exist = tabs.find((t) => t.comboId === c.id)
    if (exist) {
      switchTab(exist.key)
      return
    }
    const t: ComboTab = {
      ...newTab(c.name),
      method: c.method,
      items: c.items.map((it) => ({ id: it.id, name: it.name, weight: it.weight, direction: it.direction })),
      comboId: c.id,
      dirty: false,
    }
    setTabs((ts) => [...ts, t])
    setActiveKey(t.key)
    setError('')
    setMsg('')
    setConfirmDel(false)
    setOpenRow(null)
    setAddOpen(false)
  }

  function addItem(f: Factor) {
    patchActive({ items: [...active.items, { id: f.id, name: f.name, weight: 1, direction: 1 }], dirty: true })
  }
  function removeItem(id: number) {
    if (openRow === id) setOpenRow(null)
    patchActive({ items: active.items.filter((x) => x.id !== id), dirty: true })
  }
  function setItem(id: number, patch: Partial<ComboItem>) {
    patchActive({ items: active.items.map((x) => (x.id === id ? { ...x, ...patch } : x)), dirty: true })
  }

  function applyCombo(c: ComboProposal) {
    const byId = new Map(factors.map((f) => [f.id, f]))
    const items: ComboItem[] = []
    for (const it of c.items) {
      const f = byId.get(it.id)
      if (!f?.available) continue
      items.push({
        id: f.id,
        name: f.name,
        weight: it.weight > 0 ? it.weight : 1,
        direction: it.direction < 0 ? -1 : 1,
      })
    }
    if (items.length < MIN_FACTORS) {
      setError('AI 给出的组合无法落到当前因子库（需要至少 2 个可用因子）')
      return
    }
    const method = c.method === 'manual' ? 'manual' : 'equal'
    patchActive({
      name: c.name.trim() || active.name,
      method,
      items: method === 'equal' ? items.map((x) => ({ ...x, weight: 1 })) : items,
      dirty: true,
    })
    setError('')
    setMsg(`已将「${c.name.trim() || '组合'}」写入编辑器`)
  }

  function openRun() {
    if (active.items.length < MIN_FACTORS || busy) return
    setError('')
    setConfirmDel(false)
    setRunOpen(true)
  }

  async function doEvaluate() {
    if (active.items.length < MIN_FACTORS || busy) return
    setRunOpen(false)
    setBusy(true)
    setError('')
    const t0 = Date.now()
    try {
      // 已保存且无未保存改动 → 走组合评价接口（摘要写回卡片）；否则直接按当前成分计算
      const saved = active.comboId != null && !active.dirty
      const r = saved
        ? await api.evaluateCombo(active.comboId!, {
            horizon: params.horizon, groups: params.groups, cost: params.cost / 100,
            start: params.start, end: params.end,
            universe: meta?.universe_switchable ? params.universe : (health?.stock_pool || ''),
          })
        : await api.compose(
            active.items.map((it) => ({ id: it.id, name: it.name, weight: it.weight, direction: it.direction })),
            active.method,
            {
              horizon: params.horizon, groups: params.groups, cost: params.cost / 100,
              start: params.start, end: params.end,
              universe: meta?.universe_switchable ? params.universe : (health?.stock_pool || ''),
            },
          )
      patchActive({ lastExpr: r.expr })
      if (r.errors?.length) setError(r.errors.join('\n'))
      if (r.metrics) {
        setResultView({ name: active.name, result: r, params: { ...params }, ts: Date.now(), durationMs: Date.now() - t0 })
        if (saved) loadCombos() // 摘要已写回，刷新列表
      }
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  async function onSave() {
    const name = active.name.trim()
    if (!name) {
      setError('请填写组合名称')
      return
    }
    if (active.items.length < MIN_FACTORS) {
      setError(`组合至少需要 ${MIN_FACTORS} 个因子`)
      return
    }
    try {
      if (active.comboId != null) {
        await api.updateCombo(active.comboId, { name, method: active.method, items: active.items })
        patchActive({ dirty: false })
        setMsg(`已更新组合「${name}」`)
      } else {
        const c = await api.createCombo({ name, method: active.method, items: active.items })
        patchActive({ comboId: c.id, dirty: false })
        setMsg(`已创建组合「${name}」`)
      }
      setConfirmDel(false)
      loadCombos()
    } catch (e) {
      setError((e as Error).message)
    }
  }

  async function doDelete() {
    if (active.comboId == null) return
    try {
      await api.deleteCombo(active.comboId)
      setConfirmDel(false)
      closeTab(active.key)
      setMsg('组合已删除')
      loadCombos()
    } catch (e) {
      setError((e as Error).message)
    }
  }

  return (
    <div className="fw">
      <div className="fw-shell">
        {/* 左侧：我的组合（未保存草稿 + 已保存组合） */}
        <aside className="fw-lib">
          <div className="fw-lib-head">
            <span>我的组合</span>
            <span className="fw-lib-count">{draftRows.length + savedRows.length}</span>
            <button className="fw-lib-add" onClick={addTab} title="新建组合">＋</button>
          </div>
          <div className="fw-lib-search">
            <input
              type="text"
              value={listFilter}
              onChange={(e) => setListFilter(e.target.value)}
              placeholder="搜索组合…"
            />
          </div>
          <div className="fw-lib-list">
            {!draftRows.length && !savedRows.length && (
              <p className="subtle" style={{ padding: '10px 10px', lineHeight: 1.7 }}>
                {listFilter ? '无匹配组合' : '还没有组合。点右上「＋」新建，或在中间选好因子后「保存组合」。'}
              </p>
            )}
            {draftRows.map((r) => (
              <div
                key={r.key}
                className={`fw-lib-item${r.active ? ' on' : ''}`}
                onClick={() => switchTab(r.key)}
                title="未保存，编辑中"
                role="button"
                tabIndex={0}
                onKeyDown={(e) => { if (e.key === 'Enter') switchTab(r.key) }}
              >
                <span className={`fw-dot${r.dirty ? ' dirty' : ''}`} />
                <span className="fw-lib-name">{r.name}</span>
                <span className="ic">{tabs.find((t) => t.key === r.key)?.items.length ?? 0}</span>
                <button
                  className="fw-lib-x"
                  title="关闭文件（内容未保存将丢弃）"
                  onClick={(e) => { e.stopPropagation(); closeTab(r.key) }}
                >×</button>
              </div>
            ))}
            {savedRows.map(({ combo: c, open, active: isActive, dirty: isDirty }) => (
              <div
                key={c.id}
                className={`fw-lib-item${isActive ? ' on' : ''}`}
                onClick={() => openCombo(c)}
                title={c.items.map((i) => `${i.name}${i.direction < 0 ? '（反向）' : ''}`).join(' + ')}
                role="button"
                tabIndex={0}
                onKeyDown={(e) => { if (e.key === 'Enter') openCombo(c) }}
              >
                <span className={`fw-dot${isDirty ? ' dirty' : ''}`} />
                <span className="fw-lib-name">{c.name}</span>
                {c.summary ? <span className="ic">{c.summary.rank_ic_mean.toFixed(3)}</span> : <span className="ic">—</span>}
                {open && (
                  <button
                    className="fw-lib-x"
                    title="关闭文件（组合已保存，不会删除）"
                    onClick={(e) => {
                      e.stopPropagation()
                      const t = tabs.find((x) => x.comboId === c.id)
                      if (t) closeTab(t.key)
                    }}
                  >×</button>
                )}
              </div>
            ))}
          </div>
        </aside>

        {/* 中间：组合合同（标题 / 状态条 / 因子表 / 新增） */}
        <section className="fw-editor-col">
          <header className="fw-top cc-toolbar">
            <div className="fw-file">
              <span className={`fw-file-status ${active.dirty ? 'dirty' : active.comboId != null ? 'saved' : 'draft'}`}>
                {active.dirty ? '未保存' : active.comboId != null ? '已保存' : '草稿'}
              </span>
            </div>
            <div className="fw-top-actions">
              {confirmDel ? (
                <div className="cc-delete-confirm">
                  <span>确认删除？</span>
                  <button className="btn sm danger" onClick={doDelete}>删除</button>
                  <button className="btn sm ghost" onClick={() => setConfirmDel(false)}>取消</button>
                </div>
              ) : (
                active.comboId != null && (
                  <button className="btn sm ghost" onClick={() => setConfirmDel(true)} title="删除该组合">删除</button>
                )
              )}
              <span className="cc-action-divider" />
              <button
                className="btn sm"
                onClick={onSave}
                disabled={!active.name.trim() || active.items.length < MIN_FACTORS}
                title="把当前成分保存为组合"
              >
                保存
              </button>
              <button
                className="btn primary sm"
                onClick={openRun}
                disabled={busy || active.items.length < MIN_FACTORS}
              >
                {busy ? '评价中…' : '运行评价'}
              </button>
            </div>
          </header>

          {error && <div className="note err fw-flash">{error}</div>}
          {msg && <div className="note ok fw-flash">{msg}</div>}
          {busy && (
            <div className="fw-busy">
              <span className="spinner" />
              {samePool(params.universe, health?.stock_pool)
                ? '正在合成并评价组合…'
                : `正在加载「${universeName(params.universe, meta?.universes)}」成分与行情，随后评价…`}
            </div>
          )}

          <div className="fw-editor cc-editor">
            <div className="cc-body">
              <div className="cc-hero">
                <div className="field cc-name">
                  <label>组合名称</label>
                  <input
                    type="text"
                    value={active.name}
                    onChange={(e) => patchActive({ name: e.target.value, dirty: true })}
                    placeholder="如：价值质量精选"
                  />
                </div>
                <div className="cc-hero-row">
                  <div className="cc-hero-tags">
                    {categoryTags.map((tag) => (
                      <span className="cc-chip" key={tag}>{tag}</span>
                    ))}
                    <Select
                      value={active.method}
                      onChange={(v) => patchActive({ method: v as 'equal' | 'manual', dirty: true })}
                      size="sm"
                      minWidth={108}
                      items={[
                        { value: 'equal', label: '等权' },
                        { value: 'manual', label: '自定义权重' },
                      ]}
                    />
                    <span className="cc-chip">持有 {params.horizon} 日</span>
                  </div>
                  <span className="cc-hero-age">
                    {active.comboId != null ? '已保存' : '本地草稿'} · {poolAgeLabel(savedCombo?.created_at)}
                  </span>
                </div>
              </div>

              <div className="cc-note">
                下表各因子指标（IC / ICIR / 收益率 / 年化收益率）、入池时间，均取自该因子最近一次评价区间，并非组合合成后的结果。
              </div>

              <div className="cc-table-wrap">
                <table className="tbl cc-tbl">
                  <thead>
                    <tr>
                      <th>因子</th>
                      <th className="num">IC</th>
                      <th className="num">ICIR</th>
                      <th className="num">收益率</th>
                      <th className="num">年化收益率</th>
                      <th>入池 / 修改</th>
                      <th>方向</th>
                      <th className="num">权重</th>
                      <th>操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {active.items.length === 0 ? (
                      <tr>
                        <td colSpan={9} className="cc-empty-cell">
                          尚未添加因子。点下方「新增因子」从因子库挑入，或让 AI 推荐互补信号。
                        </td>
                      </tr>
                    ) : active.items.map((item) => {
                      const factor = factorById.get(item.id)
                      const s = factor?.summary
                      const rawWeight = active.method === 'equal' ? 1 : Math.max(0, item.weight)
                      const normalized = totalWeight > 0 ? rawWeight / totalWeight : 0
                      const ann = topAnn(s)
                      const expanded = openRow === item.id
                      const joined = md(savedCombo?.created_at)
                      const changed = factor && factor.updated_at && factor.updated_at !== factor.created_at
                        ? md(factor.updated_at)
                        : '—'
                      return (
                        <Fragment key={item.id}>
                          <tr className={expanded ? 'cc-row-open' : ''}>
                            <td>
                              <button
                                className="cc-factor-name"
                                onClick={() => setOpenRow(expanded ? null : item.id)}
                                title={item.name}
                              >
                                {item.name}
                              </button>
                            </td>
                            <td className="num mono">{s ? fmt(s.rank_ic_mean, 4) : '—'}</td>
                            <td className="num mono">{s ? fmt(s.rank_ic_ir, 4) : '—'}</td>
                            <td className={`num mono ${signCls(s?.top_excess_cum)}`}>{s ? pct(s.top_excess_cum, 2) : '—'}</td>
                            <td className={`num mono ${signCls(ann)}`}>{ann != null ? pct(ann, 2) : '—'}</td>
                            <td className="cc-join">
                              <span>入 {joined}</span>
                              <span>改 {changed}</span>
                            </td>
                            <td>
                              <button
                                className={`cc-dir${item.direction < 0 ? ' rev' : ''}`}
                                onClick={() => setItem(item.id, { direction: -item.direction })}
                                title="点击切换信号方向"
                              >
                                {item.direction < 0 ? '反向' : '正向'}
                              </button>
                            </td>
                            <td className="num">
                              {active.method === 'equal' ? (
                                <span className="cc-w">{(normalized * 100).toFixed(0)}%</span>
                              ) : (
                                <input
                                  className="cc-w-input"
                                  type="number"
                                  step={0.1}
                                  min={0}
                                  value={item.weight}
                                  onChange={(e) => setItem(item.id, { weight: Math.max(0, +e.target.value) })}
                                  aria-label={`${item.name}的权重`}
                                />
                              )}
                            </td>
                            <td>
                              <div className="ops">
                                <button
                                  className="cc-op"
                                  title={expanded ? '收起' : '展开'}
                                  onClick={() => setOpenRow(expanded ? null : item.id)}
                                >
                                  {expanded ? '⌃' : '⌄'}
                                </button>
                                <button className="cc-op danger" title="移除因子" onClick={() => removeItem(item.id)}>×</button>
                              </div>
                            </td>
                          </tr>
                          {expanded && (
                            <tr className="cc-expand">
                              <td colSpan={9}>
                                {factor ? catLabel(factor.category) : `因子 #${item.id}`}
                                {s ? ` · 评价 ${s.from} ~ ${s.to}` : ' · 未评价'}
                                {factor?.expr ? ` · ${exprBrief(factor.expr, 88)}` : ''}
                              </td>
                            </tr>
                          )}
                        </Fragment>
                      )
                    })}
                  </tbody>
                </table>
              </div>

              <div className="cc-cta">
                <button className="cc-cta-btn" onClick={() => setAddOpen(true)}>+ 新增因子</button>
                <button className="cc-cta-btn ghost" onClick={() => setChatOpen(true)}>让 AI 推荐</button>
              </div>
              <div className="cc-count">共 {active.items.length} 只因子</div>
              <p className="cc-fine">组合至少 {MIN_FACTORS} 个因子才可保存与评价；合成前会统一做横截面标准化。</p>
            </div>
          </div>
        </section>

        {chatOpen && <div className="fw-resizer-x" onPointerDown={startDrag} title="拖拽调整 AI 面板宽度" />}

        <aside className="fw-chat" style={{ width: chatOpen ? chatWidth : 40 }}>
          {chatOpen ? (
            <AssistPanel
              expr={assistCtx}
              mode="combo"
              llm={llm}
              fileName={`${active.name}.combo`}
              onCollapse={() => setChatOpen(false)}
              onApplyCombo={applyCombo}
              suggestions={COMBO_SUGGESTIONS}
              quick={COMBO_QUICK}
              placeholder="描述你的组合想法…（Enter 发送，Shift+Enter 换行）"
              intro={{
                title: '描述想法，让 AI 从因子库搭组合',
                desc: 'AI 只会挑选因子库里已有的因子（不会写新表达式）。方案校验通过后直接写到中间的编辑器，你可以改权重、换成分，再保存或运行评价。',
              }}
            />
          ) : (
            <button className="fw-chat-collapsed" onClick={() => setChatOpen(true)} title="展开 AI 助手">
              <span><AiIcon /></span><span>AI 助手</span>
            </button>
          )}
        </aside>
      </div>

      {addOpen && (
        <Modal
          title="新增因子"
          subtitle="从因子库挑入当前组合，可连续添加"
          onClose={() => setAddOpen(false)}
          width={760}
          footer={
            <>
              <span className="subtle">已选 {active.items.length} 只</span>
              <button className="btn primary sm" style={{ marginLeft: 'auto' }} onClick={() => setAddOpen(false)}>完成</button>
            </>
          }
        >
          <div className="cc-pool-search cc-add-search">
            <span>⌕</span>
            <input
              type="text"
              placeholder="搜索名称或类别"
              value={q}
              onChange={(e) => setQ(e.target.value)}
              autoFocus
            />
            {q && <button onClick={() => setQ('')} title="清空搜索">×</button>}
          </div>
          <div className="cc-add-table">
            <table className="tbl cc-tbl">
              <thead>
                <tr>
                  <th>因子</th>
                  <th className="num">IC</th>
                  <th className="num">ICIR</th>
                  <th className="num">收益率</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {candidates.map((factor) => (
                  <tr key={factor.id}>
                    <td>
                      <div className="cc-add-name">
                        <b>{factor.name}</b>
                        <span>{catLabel(factor.category)}{factor.available ? '' : ' · 不可用'}</span>
                      </div>
                    </td>
                    <td className={`num mono ${signCls(factor.summary?.rank_ic_mean)}`}>
                      {factor.summary ? fmt(factor.summary.rank_ic_mean, 4) : '—'}
                    </td>
                    <td className={`num mono ${signCls(factor.summary?.rank_ic_ir)}`}>
                      {factor.summary ? fmt(factor.summary.rank_ic_ir, 4) : '—'}
                    </td>
                    <td className={`num mono ${signCls(factor.summary?.top_excess_cum)}`}>
                      {factor.summary ? pct(factor.summary.top_excess_cum, 2) : '—'}
                    </td>
                    <td className="num">
                      <button
                        className="btn xs"
                        onClick={() => addItem(factor)}
                        disabled={!factor.available}
                        title={factor.available ? '加入组合' : factor.unavailable_reason || '当前数据源不可用'}
                      >
                        加入
                      </button>
                    </td>
                  </tr>
                ))}
                {candidates.length === 0 && (
                  <tr>
                    <td colSpan={5} className="cc-empty-cell">没有匹配的因子</td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </Modal>
      )}

      {/* 运行评价：先选参数（与因子工厂同一套） */}
      {runOpen && (
        <Modal
          title="运行评价"
          subtitle={`对组合「${active.name}」确认分层、持有期、成本、股票池和区间`}
          onClose={() => setRunOpen(false)}
          width={560}
          footer={
            <>
              <span className="subtle">Esc 取消</span>
              <button className="btn primary sm" style={{ marginLeft: 'auto' }} onClick={doEvaluate} disabled={busy}>
                {busy ? '评价中…' : '开始评价'}
              </button>
            </>
          }
        >
          <ParamsPanel
            p={params}
            setP={setParams}
            universes={meta?.universes}
            switchable={meta?.universe_switchable}
            currentPool={health?.stock_pool}
          />
        </Modal>
      )}

      {/* 评价结果：与因子工厂同一套展示 */}
      {resultView && (
        <Modal
          title={<>评价结果 · {resultView.name}</>}
          subtitle={`${fmtTime(resultView.ts)} · ${universeName(resultView.params.universe, meta?.universes) || '当前股票池'} · 持有 ${resultView.params.horizon} 日 · ${resultView.params.groups} 层 · 成本 ${resultView.params.cost}% · ${resultView.params.start || '全区间'} ~ ${resultView.params.end || '今'} · 耗时 ${(resultView.durationMs / 1000).toFixed(1)}s`}
          onClose={() => setResultView(null)}
          footer={
            <>
              <span className="subtle">Esc 关闭</span>
              {active.dirty && (
                <button
                  className="btn sm"
                  style={{ marginLeft: 'auto' }}
                  onClick={() => { setResultView(null); onSave() }}
                >
                  保存组合后摘要入库
                </button>
              )}
            </>
          }
        >
          {resultView.result.warnings?.length > 0 && (
            <div className="note warn mb12">
              <span>⚠</span>
              <div>
                {resultView.result.warnings.map((w, i) => (
                  <div key={i}>{w}</div>
                ))}
              </div>
            </div>
          )}
          <FactoryResult metrics={resultView.result.metrics ?? null} busy={false} />

          {resultView.result.correlation?.length > 1 && (
            <>
              <div className="fw-ref-label mt12">成分因子相关性 <span className="subtle">平均截面秩相关，&gt;0.7 视为同质</span></div>
              <div className="tbl-wrap">
                <table className="tbl">
                  <thead>
                    <tr>
                      <th></th>
                      {resultView.result.names.map((n, i) => (
                        <th key={i} className="num" title={n}>{i + 1}</th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {resultView.result.correlation.map((row, i) => (
                      <tr key={i}>
                        <td style={{ fontSize: 12 }} title={resultView.result.names[i]}>
                          {i + 1}. {resultView.result.names[i]}
                        </td>
                        {row.map((v, j) => (
                          <td key={j} className={`corr-cell ${i === j ? 'diag' : ''}`} style={{ background: i === j ? undefined : corrBg(v) }}>
                            {i === j ? '1' : v.toFixed(2)}
                          </td>
                        ))}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </>
          )}

          <details className="mt12">
            <summary className="subtle" style={{ cursor: 'pointer' }}>合成表达式（{resultView.result.label}）</summary>
            <pre className="expr mt8">{resultView.result.expr}</pre>
          </details>
        </Modal>
      )}
    </div>
  )
}
