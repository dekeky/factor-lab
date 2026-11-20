import { useEffect, useMemo, useRef, useState } from 'react'
import { api } from '../api'
import type { Factor, Health, Meta, Metrics } from '../types'
import { AssistPanel } from '../components/AssistPanel'
import { AiIcon } from '../components/AiIcon'
import { CodeEditor, type CodeEditorHandle } from '../components/CodeEditor'
import { FactoryResult } from '../components/FactoryResult'
import { defaultEvalRange, ParamsPanel, type Params, type SaveState } from '../components/FactoryPanels'
import { Modal } from '../components/Modal'
import { EvalRecordList } from '../components/EvalRecordList'
import { fmt, signCls } from '../components/Layout'
import { clearHistory, fmtTime, loadHistory, saveRecord, type EvalRecord } from '../lib/evalHistory'
import { samePool, universeName } from '../lib/universes'

const DEFAULT_EXPR = 'mom20 = close / ts_delay(close, 20) - 1\nalpha = -1 * cs_rank(mom20)'

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

/** Tabler trash outline，与 finclaw 列表删除按钮同形。 */
function TrashIcon({ size = 14 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="M4 7h16" />
      <path d="M10 11v6" />
      <path d="M14 11v6" />
      <path d="M5 7l1 12a2 2 0 0 0 2 2h8a2 2 0 0 0 2-2l1-12" />
      <path d="M9 7V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v3" />
    </svg>
  )
}

/** 入库：数据库 + 加号（Tabler database-plus） */
function DbPlusIcon({ size = 14 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="M4 6c0 1.657 3.582 3 8 3s8 -1.343 8 -3s-3.582 -3 -8 -3s-8 1.343 -8 3" />
      <path d="M4 6v6c0 1.657 3.582 3 8 3c1.075 0 2.1 -.08 3.037 -.224" />
      <path d="M20 12v-6" />
      <path d="M4 12v6c0 1.657 3.582 3 8 3c.166 0 .331 -.002 .495 -.006" />
      <path d="M16 19h6" />
      <path d="M19 16v6" />
    </svg>
  )
}

/** 编辑器标签页：每个打开的因子一份独立上下文 */
interface FileTab {
  key: string
  name: string
  expr: string
  factorId: number | null
  dirty: boolean
  metrics: Metrics | null
  save: SaveState
}

function newTab(name: string, expr: string, factorId: number | null): FileTab {
  return {
    key: `t${Date.now()}${Math.random().toString(36).slice(2, 6)}`,
    name,
    expr,
    factorId,
    dirty: false,
    metrics: null,
    save: factorId
      ? { name, cat: 'user', mode: 'update', updateId: factorId }
      : { name: '', cat: 'user', mode: 'new', updateId: null },
  }
}

/**
 * 因子工厂：Cursor 式 IDE 工作区。
 *
 * 左列 = 因子列表 + 多标签编辑器
 * 右列 = 全高 AI 对话助手，宽度可拖拽。
 * 评价参数在点击「运行评价」时以弹窗选择（分层、持有、成本、股票池、区间），入库与结果用 Modal。
 */
export function FactoryPage({
  meta,
  llm,
  health,
  onSaved,
}: {
  meta: Meta | null
  llm: string
  health: Health | null
  onSaved: () => void
}) {
  const [tabs, setTabs] = useState<FileTab[]>(() => [newTab('未命名', DEFAULT_EXPR, null)])
  const [activeKey, setActiveKey] = useState<string>(() => '')
  const [params, setParams] = useState<Params>({
    horizon: 1, groups: 10, cost: 0.3, start: '', end: '', universe: storedStr('fw.evalUniverse'),
  })
  const [library, setLibrary] = useState<Factor[]>([])
  const [libFilter, setLibFilter] = useState('')
  const [check, setCheck] = useState<{ ok: boolean; errors: string[] } | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [msg, setMsg] = useState('')
  const [chatWidth, setChatWidth] = useState(() => stored('fw.chatWidth', 400))
  const [chatOpen, setChatOpen] = useState(true)
  const [runOpen, setRunOpen] = useState(false)
  const editorRef = useRef<CodeEditorHandle>(null)
  const dragRef = useRef<{ start: number; base: number } | null>(null)
  const [records, setRecords] = useState<EvalRecord[]>(() => loadHistory())
  const [resultView, setResultView] = useState<EvalRecord | null>(null)
  const [recordsOpen, setRecordsOpen] = useState(false)
  /** 评价记录弹窗内的详情；不拆弹窗，避免切页闪一下 */
  const [recordDetail, setRecordDetail] = useState<EvalRecord | null>(null)
  /** 列表内联重命名：kind=draft 用标签页 key，kind=saved 用因子 id */
  const [renaming, setRenaming] = useState<{ kind: 'draft' | 'saved'; id: string; value: string } | null>(null)

  const active = tabs.find((t) => t.key === activeKey) ?? tabs[0]

  useEffect(() => {
    if (!tabs.find((t) => t.key === activeKey)) setActiveKey(tabs[0].key)
  }, [tabs, activeKey])

  const loadLibrary = () => api.factors().then(setLibrary).catch(() => {})
  useEffect(() => { loadLibrary() }, [])

  // 评价区间默认最近五年（夹在数据面板范围内）；股票池默认取当前面板（用户改过则不覆盖）
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

  // 实时静态校验（防抖 500ms），跟随当前标签页表达式
  useEffect(() => {
    if (!active.expr.trim()) {
      setCheck(null)
      return
    }
    const t = setTimeout(() => {
      api
        .check(active.expr)
        .then((r) => setCheck({ ok: r.ok, errors: r.errors ?? [] }))
        .catch(() => setCheck(null))
    }, 500)
    return () => clearTimeout(t)
  }, [active.expr])

  // 分隔条拖拽：x 调整右侧对话宽度
  useEffect(() => {
    const move = (e: PointerEvent) => {
      const d = dragRef.current
      if (!d) return
      const v = clamp(d.base + (d.start - e.clientX), 300, 680)
      setChatWidth(v)
      try { localStorage.setItem('fw.chatWidth', String(v)) } catch { /* 忽略隐私模式 */ }
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

  const fields = useMemo(() => meta?.fields ?? [], [meta])
  const ops = useMemo(() => meta?.operators ?? [], [meta])

  /** 左侧列表：只展示用户自建因子（内置 / 因子库因子不出现），搜索过滤，按更新时间倒序 */
  const listedFactors = useMemo(() => {
    const kw = libFilter.trim().toLowerCase()
    return library
      .filter((f) => f.source === 'user' && (!kw || f.name.toLowerCase().includes(kw)))
      .sort((a, b) => (b.updated_at > a.updated_at ? 1 : -1))
  }, [library, libFilter])

  /** 列表呈现两类行：未保存的草稿文件 + 已保存的自建因子 */
  const draftRows = useMemo(() => {
    const kw = libFilter.trim().toLowerCase()
    return tabs
      .filter((t) => t.factorId === null && (!kw || t.name.toLowerCase().includes(kw)))
      .map((t) => ({ key: t.key, name: t.name, dirty: t.dirty, active: t.key === active.key }))
  }, [tabs, libFilter, active.key])

  const savedRows = useMemo(
    () =>
      listedFactors.map((f) => {
        const t = tabs.find((x) => x.factorId === f.id)
        return { factor: f, active: t?.key === active.key, dirty: !!t?.dirty }
      }),
    [listedFactors, tabs, active.key],
  )

  /** 当前文件的入库状态：放在编辑器顶栏，而不是左侧列表分组 */
  const fileStatus = active.factorId == null
    ? { kind: 'draft' as const, label: '未保存', hint: '尚未入库，关闭后内容会丢失' }
    : active.dirty
      ? { kind: 'dirty' as const, label: '待保存', hint: '表达式已改，尚未写回因子库' }
      : { kind: 'saved' as const, label: '已保存', hint: '当前表达式已入库' }

  function startDrag(e: React.PointerEvent<HTMLDivElement>) {
    e.preventDefault()
    dragRef.current = { start: e.clientX, base: chatWidth }
    document.body.classList.add('fw-dragging-x')
  }

  function patchActive(patch: Partial<FileTab>) {
    setTabs((ts) => ts.map((t) => (t.key === active.key ? { ...t, ...patch } : t)))
  }

  function switchTab(key: string) {
    if (key === active.key) return
    setActiveKey(key)
    setCheck(null)
    setError('')
    setMsg('')
  }

  function closeTab(key: string) {
    setTabs((ts) => {
      const idx = ts.findIndex((t) => t.key === key)
      const next = ts.filter((t) => t.key !== key)
      if (!next.length) {
        const fresh = newTab('未命名', DEFAULT_EXPR, null)
        setActiveKey(fresh.key)
        return [fresh]
      }
      if (key === activeKey) setActiveKey(next[Math.min(idx, next.length - 1)].key)
      return next
    })
    setCheck(null)
    setError('')
    setMsg('')
  }

  function addTab() {
    const used = new Set(tabs.map((t) => t.name))
    let n = 1
    let name = '未命名'
    while (used.has(name)) {
      n += 1
      name = `未命名 ${n}`
    }
    const t = newTab(name, DEFAULT_EXPR, null)
    setTabs((ts) => [...ts, t])
    setActiveKey(t.key)
    setCheck(null)
    setError('')
    setMsg('')
    editorRef.current?.focus()
  }

  function updateExpr(v: string) {
    patchActive({ expr: v, dirty: true })
  }

  /** 从左侧列表打开一个因子作为标签页（已打开则直接切换） */
  function openFactor(f: Factor) {
    const exist = tabs.find((t) => t.factorId === f.id)
    if (exist) {
      switchTab(exist.key)
      return
    }
    const t = newTab(f.name, f.expr, f.id)
    setTabs((ts) => [...ts, t])
    setActiveKey(t.key)
    setCheck(null)
    setMsg('')
    setError(f.available ? '' : `「${f.name}」在当前数据源下不可用：${f.unavailable_reason ?? '缺少字段或算子'}`)
  }

  /** 双击名称进入内联重命名 */
  function startRename(kind: 'draft' | 'saved', id: string, current: string) {
    setError('')
    setMsg('')
    setRenaming({ kind, id, value: current })
  }

  /** 提交重命名：草稿只改本地标签页；已保存因子调更新接口 */
  async function commitRename() {
    const r = renaming
    if (!r) return
    setRenaming(null)
    const name = r.value.trim()
    if (!name) return
    if (r.kind === 'draft') {
      setTabs((ts) => ts.map((t) => (t.key === r.id
        ? { ...t, name, save: { ...t.save, name } }
        : t)))
      return
    }
    const fid = Number(r.id)
    const f = library.find((x) => x.id === fid)
    if (!f || name === f.name) return
    try {
      await api.updateFactor(fid, { name, expr: f.expr, category: f.category, desc: f.desc })
      setMsg(`已重命名为「${name}」`)
      setTabs((ts) => ts.map((t) => (t.factorId === fid ? { ...t, name, save: { ...t.save, name } } : t)))
      loadLibrary()
      onSaved()
    } catch (e) {
      setError((e as Error).message)
    }
  }

  /** 点「运行评价」/ Ctrl+Enter：先弹参数选择框，确认后再计算 */
  function openRun() {
    if (!active.expr.trim() || busy) return
    setError('')
    setRunOpen(true)
  }

  async function doEvaluate() {
    if (!active.expr.trim() || busy) return
    setRunOpen(false)
    setBusy(true)
    setError('')
    setMsg('')
    const t0 = Date.now()
    try {
      const m = await api.evaluate(active.expr, {
        horizon: params.horizon,
        groups: params.groups,
        cost: params.cost / 100,
        start: params.start,
        end: params.end,
        universe: meta?.universe_switchable ? params.universe : (health?.stock_pool || ''),
      })
      patchActive({ metrics: m, dirty: false })
      const rec: EvalRecord = {
        id: `e${Date.now()}`,
        ts: Date.now(),
        fileName: `${active.name}.dsl`,
        expr: active.expr,
        params: {
          horizon: params.horizon, groups: params.groups, cost: params.cost,
          start: params.start, end: params.end,
          universe: meta?.universe_switchable ? params.universe : (health?.stock_pool || ''),
        },
        metrics: m,
        durationMs: Date.now() - t0,
      }
      setRecords(saveRecord(rec))
      setResultView(rec) // 运行完成 → 弹窗展示结果
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  /** 把某条评价记录的表达式与参数载到一个新标签页 */
  function loadRecordExpr(r: EvalRecord) {
    const exist = tabs.find((t) => t.expr === r.expr && t.name === r.fileName.replace(/\.dsl$/, ''))
    if (exist) {
      switchTab(exist.key)
    } else {
      const t = newTab(r.fileName.replace(/\.dsl$/, ''), r.expr, null)
      t.save = { name: t.name, cat: 'user', mode: 'new', updateId: null }
      t.dirty = true // 从记录载回：尚未入库，视为未保存修改
      setTabs((ts) => [...ts, t])
      setActiveKey(t.key)
    }
    setParams({
      horizon: r.params.horizon,
      groups: r.params.groups,
      cost: r.params.cost,
      start: r.params.start,
      end: r.params.end,
      universe: r.params.universe || params.universe,
    })
    setCheck(null)
    setMsg('')
    setError('')
  }

  function resultSubtitle(r: EvalRecord) {
    return `${fmtTime(r.ts)} · ${universeName(r.params.universe, meta?.universes) || '当前股票池'} · 持有 ${r.params.horizon} 日 · ${r.params.groups} 层 · 成本 ${r.params.cost}% · ${r.params.start || '全区间'} ~ ${r.params.end || '今'} · 耗时 ${(r.durationMs / 1000).toFixed(1)}s`
  }

  function closeRecords() {
    setRecordDetail(null)
    setRecordsOpen(false)
  }

  /** 把指定标签页写入因子库：草稿新建，已入库则按当前表达式更新。名称用左侧列表名。 */
  async function saveTab(tab: FileTab) {
    const name = tab.name.trim()
    if (!name) {
      setError('请先填写因子名称（双击列表名称可重命名）')
      return
    }
    try {
      if (tab.factorId != null) {
        await api.updateFactor(tab.factorId, {
          name, expr: tab.expr, category: tab.save.cat,
          desc: tab.metrics
            ? `评价于 ${tab.metrics.dates[0]} ~ ${tab.metrics.dates[tab.metrics.dates.length - 1]}`
            : '从因子工厂更新表达式',
        })
        setMsg(`已更新因子「${name}」（原指标已作废，需重新评价）`)
        setTabs((ts) => ts.map((t) => (
          t.key === tab.key ? { ...t, name, save: { ...t.save, name }, dirty: false } : t
        )))
      } else {
        const created = await api.createFactor({ name, expr: tab.expr, category: tab.save.cat, source: 'user' })
        setMsg(`已入库「${name}」`)
        setTabs((ts) => ts.map((t) => (
          t.key === tab.key
            ? { ...t, name, save: { ...t.save, mode: 'update', updateId: created?.id ?? null, name }, factorId: created?.id ?? null, dirty: false }
            : t
        )))
      }
      loadLibrary()
      onSaved()
    } catch (e) {
      setError((e as Error).message)
    }
  }

  function discardDraft(key: string, name: string, dirty: boolean) {
    if (dirty && !confirm(`「${name}」尚未入库，删除后内容将丢失。确定删除？`)) return
    closeTab(key)
  }

  async function deleteSaved(f: Factor) {
    if (!confirm(`确认删除因子「${f.name}」？此操作不可撤销。`)) return
    try {
      await api.deleteFactor(f.id)
      const t = tabs.find((x) => x.factorId === f.id)
      if (t) closeTab(t.key)
      setMsg('因子已删除')
      loadLibrary()
      onSaved()
    } catch (e) {
      setError((e as Error).message)
    }
  }

  return (
    <div className="fw">
      <div className="fw-shell">
        {/* 左侧：因子列表；名称右侧为保存 / 删除 */}
        <aside className="fw-lib">
          <div className="fw-lib-head">
            <span>因子</span>
            <span className="fw-lib-count">{draftRows.length + savedRows.length}</span>
            <button className="fw-lib-add" onClick={addTab} title="新建因子文件">＋</button>
          </div>
          <div className="fw-lib-search">
            <input
              type="text"
              value={libFilter}
              onChange={(e) => setLibFilter(e.target.value)}
              placeholder="搜索因子…"
            />
          </div>
          <div className="fw-lib-list">
            {!draftRows.length && !savedRows.length && (
              <p className="subtle" style={{ padding: '10px 10px', lineHeight: 1.7 }}>
                {libFilter ? '无匹配因子' : '还没有因子文件。点右上「＋」新建，或编写表达式后点名称旁的入库图标。'}
              </p>
            )}
            {draftRows.map((r) => (
              <div
                key={r.key}
                className={`fw-lib-item${r.active ? ' on' : ''}`}
                onClick={() => switchTab(r.key)}
                title={r.dirty ? `${r.name}.dsl · 有未写入库的更改` : `${r.name}.dsl`}
                role="button"
                tabIndex={0}
                onKeyDown={(e) => { if (e.key === 'Enter') switchTab(r.key) }}
              >
                <span className={`fw-dot${r.dirty ? ' dirty' : ' idle'}`} />
                {renaming?.kind === 'draft' && renaming.id === r.key ? (
                  <input
                    className="fw-lib-rename"
                    autoFocus
                    value={renaming.value}
                    onChange={(e) => setRenaming({ ...renaming, value: e.target.value })}
                    onClick={(e) => e.stopPropagation()}
                    onBlur={commitRename}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') commitRename()
                      if (e.key === 'Escape') setRenaming(null)
                    }}
                    title="回车确认，Esc 取消"
                  />
                ) : (
                  <span
                    className="fw-lib-name"
                    onDoubleClick={(e) => { e.stopPropagation(); startRename('draft', r.key, r.name) }}
                    title="双击重命名"
                  >{r.name}.dsl</span>
                )}
                <span className="fw-lib-actions">
                  <button
                    type="button"
                    className="fw-lib-save"
                    title="入库"
                    aria-label={`入库 ${r.name}`}
                    onClick={(e) => {
                      e.stopPropagation()
                      const t = tabs.find((x) => x.key === r.key)
                      if (t) void saveTab(t)
                    }}
                  >
                    <DbPlusIcon />
                  </button>
                  <button
                    type="button"
                    className="fw-lib-del"
                    title="删除"
                    aria-label={`删除 ${r.name}`}
                    onClick={(e) => { e.stopPropagation(); discardDraft(r.key, r.name, r.dirty) }}
                  >
                    <TrashIcon />
                  </button>
                </span>
              </div>
            ))}
            {savedRows.map(({ factor: f, active: isActive, dirty: isDirty }) => (
              <div
                key={f.id}
                className={`fw-lib-item${isActive ? ' on' : ''}${f.available ? '' : ' unavail'}`}
                onClick={() => openFactor(f)}
                title={f.available ? f.expr : `不可用：${f.unavailable_reason ?? '缺少字段或算子'}`}
                role="button"
                tabIndex={0}
                onKeyDown={(e) => { if (e.key === 'Enter') openFactor(f) }}
              >
                <span className={`fw-dot${isDirty ? ' dirty' : ' idle'}`} />
                {renaming?.kind === 'saved' && renaming.id === String(f.id) ? (
                  <input
                    className="fw-lib-rename"
                    autoFocus
                    value={renaming.value}
                    onChange={(e) => setRenaming({ ...renaming, value: e.target.value })}
                    onClick={(e) => e.stopPropagation()}
                    onBlur={commitRename}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') commitRename()
                      if (e.key === 'Escape') setRenaming(null)
                    }}
                    title="回车确认，Esc 取消"
                  />
                ) : (
                  <span
                    className="fw-lib-name"
                    onDoubleClick={(e) => { e.stopPropagation(); startRename('saved', String(f.id), f.name) }}
                    title={f.available ? `双击重命名 · ${f.expr}` : `不可用：${f.unavailable_reason ?? '缺少字段或算子'}`}
                  >{f.name}</span>
                )}
                {f.summary && <span className="ic">{f.summary.rank_ic_mean.toFixed(3)}</span>}
                <span className="fw-lib-actions">
                  {isDirty && (
                    <button
                      type="button"
                      className="fw-lib-save"
                      title="入库（更新当前表达式）"
                      aria-label={`入库 ${f.name}`}
                      onClick={(e) => {
                        e.stopPropagation()
                        const t = tabs.find((x) => x.factorId === f.id)
                        if (t) void saveTab(t)
                      }}
                    >
                      <DbPlusIcon />
                    </button>
                  )}
                  <button
                    type="button"
                    className="fw-lib-del"
                    title="删除"
                    aria-label={`删除 ${f.name}`}
                    onClick={(e) => { e.stopPropagation(); void deleteSaved(f) }}
                  >
                    <TrashIcon />
                  </button>
                </span>
              </div>
            ))}
          </div>
        </aside>

        <section className="fw-editor-col">
          {/* 操作条只属于编辑器列，不横跨右侧 AI 助手 */}
          <header className="fw-top">
            <div className="fw-file" title={fileStatus.hint}>
              <span className={`fw-file-status ${fileStatus.kind}`}>{fileStatus.label}</span>
            </div>
            <div className="fw-top-actions">
              {active.metrics && active.dirty && <span className="tag amber">结果待更新</span>}
              {active.metrics && !active.dirty && (
                <span className="fw-ic">RankIC <b className={signCls(active.metrics.rank_ic_mean)}>{fmt(active.metrics.rank_ic_mean)}</b></span>
              )}
              <button className="btn sm" onClick={() => { setRecordDetail(null); setRecordsOpen(true) }} title="历史评价记录">
                评价记录{records.length ? ` (${records.length})` : ''}
              </button>
              <button className="btn primary sm" onClick={openRun} disabled={busy || !active.expr.trim()}>
                {busy ? '评价中…' : '▶ 运行评价'}
              </button>
            </div>
          </header>

          {error && <div className="note err fw-flash">{error}</div>}
          {msg && <div className="note ok fw-flash">{msg}</div>}
          {busy && (
            <div className="fw-busy">
              <span className="spinner" />
              {samePool(params.universe, health?.stock_pool)
                ? '正在计算因子值并评价…'
                : `正在加载「${universeName(params.universe, meta?.universes)}」成分与行情，随后评价…`}
            </div>
          )}

          {check && !check.ok && <div className="fw-diag">⚠ {check.errors.join(' · ')}</div>}

          <div className="fw-editor">
            <CodeEditor
              key={active.key}
              ref={editorRef}
              value={active.expr}
              onChange={updateExpr}
              fields={fields}
              operators={ops}
              onRun={openRun}
              status={check ? (check.ok ? 'ok' : 'error') : 'idle'}
              placeholder={'例：mom20 = close / ts_delay(close, 20) - 1\nalpha = -1 * cs_rank(mom20)'}
              fill
            />
          </div>
        </section>

        {chatOpen && <div className="fw-resizer-x" onPointerDown={startDrag} title="拖拽调整 AI 面板宽度" />}

        <aside className="fw-chat" style={{ width: chatOpen ? chatWidth : 40 }}>
          {chatOpen ? (
            <AssistPanel
              expr={active.expr}
              llm={llm}
              fileName={`${active.name}.dsl`}
              onCollapse={() => setChatOpen(false)}
              onApply={(e) => {
                patchActive({ expr: e, dirty: true, metrics: null, save: { ...active.save, mode: 'new', updateId: null } })
                setCheck(null)
                setError('')
                setMsg('')
              }}
            />
          ) : (
            <button className="fw-chat-collapsed" onClick={() => setChatOpen(true)} title="展开 AI 助手">
              <span><AiIcon /></span><span>AI 助手</span>
            </button>
          )}
        </aside>
      </div>

      {resultView && (
        <Modal
          title={<>评价结果 · {resultView.fileName}</>}
          subtitle={resultSubtitle(resultView)}
          onClose={() => setResultView(null)}
          footer={
            <>
              <span className="subtle">Esc 关闭 · 可从「评价记录」随时回看</span>
              <button className="btn sm" style={{ marginLeft: 'auto' }} onClick={() => { setResultView(null); void saveTab(active) }}>
                保存
              </button>
            </>
          }
        >
          <FactoryResult metrics={resultView.metrics} busy={false} />
        </Modal>
      )}

      {runOpen && (
        <Modal
          title="运行评价"
          subtitle={`对「${active.name}.dsl」确认分层、持有期、成本、股票池和区间`}
          onClose={() => setRunOpen(false)}
          width={560}
          footer={
            <>
              <span className="subtle">Esc 取消</span>
              <button className="btn primary sm" style={{ marginLeft: 'auto' }} onClick={doEvaluate}>
                ▶ 开始评价
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

      {recordsOpen && (
        <Modal
          title={recordDetail ? <>评价结果 · {recordDetail.fileName}</> : '评价记录'}
          subtitle={recordDetail ? resultSubtitle(recordDetail) : '每次运行评价自动保存；点「详情」查看完整结果'}
          onClose={() => (recordDetail ? setRecordDetail(null) : closeRecords())}
          width={recordDetail ? 1080 : 680}
          footer={
            recordDetail ? (
              <>
                <button className="btn sm" onClick={() => setRecordDetail(null)}>返回</button>
                <button
                  className="btn sm"
                  style={{ marginLeft: 'auto' }}
                  onClick={() => {
                    loadRecordExpr(recordDetail)
                    closeRecords()
                  }}
                >
                  载入
                </button>
              </>
            ) : undefined
          }
        >
          <div key={recordDetail ? recordDetail.id : 'list'} className="modal-pane">
            {recordDetail ? (
              <FactoryResult metrics={recordDetail.metrics} busy={false} />
            ) : (
              <EvalRecordList
                records={records}
                onView={setRecordDetail}
                onClear={() => setRecords(clearHistory())}
              />
            )}
          </div>
        </Modal>
      )}
    </div>
  )
}
