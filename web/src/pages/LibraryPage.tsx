import { useEffect, useMemo, useRef, useState } from 'react'
import { api } from '../api'
import type { Factor, Meta, Metrics } from '../types'
import { Drawer } from '../components/Drawer'
import { Select } from '../components/Select'
import { ICChart, LayerChart, NavChart } from '../components/Charts'
import { Busy, CATEGORY_LABEL, Empty, catLabel, dt, fmt, pct, signCls, srcTag } from '../components/Layout'

type SortKey = 'id' | 'updated' | 'rank_ic' | 'rank_ic_ir' | 'top_excess'

/** 取排序用的指标值；未评价返回 null（始终排在末尾） */
function metricOf(f: Factor, k: SortKey): number | null {
  const s = f.summary
  if (!s) return null
  switch (k) {
    case 'rank_ic':
      return s.rank_ic_mean
    case 'rank_ic_ir':
      return s.rank_ic_ir
    case 'top_excess':
      return s.top_excess_cum
    default:
      return null
  }
}

export function LibraryPage({
  factors,
  meta,
  onChanged,
  onNav,
}: {
  factors: Factor[]
  meta: Meta | null
  onChanged: () => void
  onNav: (p: 'factory' | 'compose') => void
}) {
  const [q, setQ] = useState('')
  const [fCat, setFCat] = useState('')
  const [fSrc, setFSrc] = useState('')
  const [sortKey, setSortKey] = useState<SortKey>('id')
  const [sortDir, setSortDir] = useState<1 | -1>(1)
  const [selected, setSelected] = useState<Set<number>>(new Set())

  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState('')
  const [err, setErr] = useState('')

  const [viewing, setViewing] = useState<Factor | null>(null)
  const [editing, setEditing] = useState<Factor | 'new' | null>(null)
  const fileRef = useRef<HTMLInputElement>(null)

  const cats = useMemo(() => {
    const m = new Map<string, number>()
    for (const f of factors) m.set(f.category, (m.get(f.category) ?? 0) + 1)
    return [...m.entries()].sort((a, b) => b[1] - a[1])
  }, [factors])

  const filtered = useMemo(() => {
    const kw = q.trim().toLowerCase()
    let out = factors.filter((f) => {
      if (fCat && f.category !== fCat) return false
      if (fSrc && f.source !== fSrc) return false
      if (!kw) return true
      return f.name.toLowerCase().includes(kw) || f.expr.toLowerCase().includes(kw) || (f.desc ?? '').toLowerCase().includes(kw)
    })
    out = [...out].sort((a, b) => {
      if (sortKey === 'id') return (a.id - b.id) * sortDir
      if (sortKey === 'updated') {
        return (new Date(a.updated_at).getTime() - new Date(b.updated_at).getTime()) * sortDir
      }
      // 指标类：未评价（null）恒定沉底，不随升降序翻转
      const va = metricOf(a, sortKey)
      const vb = metricOf(b, sortKey)
      if (va === null && vb === null) return a.id - b.id
      if (va === null) return 1
      if (vb === null) return -1
      return (va - vb) * sortDir
    })
    return out
  }, [factors, q, fCat, fSrc, sortKey, sortDir])

  const allChecked = filtered.length > 0 && filtered.every((f) => selected.has(f.id))

  function toggle(id: number) {
    setSelected((prev) => {
      const next = new Set(prev)
      next.has(id) ? next.delete(id) : next.add(id)
      return next
    })
  }
  function toggleAll() {
    setSelected(allChecked ? new Set() : new Set(filtered.map((f) => f.id)))
  }

  async function batchEval(ids: number[]) {
    setBusy(true)
    setErr('')
    setMsg('')
    try {
      const r = await api.batchEvaluate(ids)
      setMsg(`已评价 ${r.evaluated} 个${r.failed ? `，失败 ${r.failed} 个` : ''}`)
      if (r.errors?.length) setErr(r.errors.slice(0, 6).join('\n'))
      onChanged()
      setSelected(new Set())
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  async function onDelete(f: Factor): Promise<boolean> {
    if (!confirm(`确认删除因子「${f.name}」？此操作不可撤销。`)) return false
    try {
      await api.deleteFactor(f.id)
      onChanged()
      setMsg(`已删除「${f.name}」`)
      return true
    } catch (e) {
      setErr((e as Error).message)
      return false
    }
  }

  async function onCopy(f: Factor) {
    try {
      const c = await api.copyFactor(f.id)
      onChanged()
      setMsg(`已复制为「${c.name}」`)
    } catch (e) {
      setErr((e as Error).message)
    }
  }

  async function onImport(file: File) {
    setBusy(true)
    setErr('')
    setMsg('')
    try {
      const text = await file.text()
      const payload = JSON.parse(text)
      const r = await api.importFactors(payload)
      setMsg(`导入完成：新增 ${r.added}、更新 ${r.updated}、跳过 ${r.skipped}`)
      if (r.errors?.length) setErr(r.errors.slice(0, 6).join('\n'))
      onChanged()
    } catch (e) {
      setErr('导入失败：' + (e as Error).message)
    } finally {
      setBusy(false)
      if (fileRef.current) fileRef.current.value = ''
    }
  }

  function sortBy(k: SortKey) {
    if (sortKey === k) setSortDir(sortDir === 1 ? -1 : 1)
    else {
      setSortKey(k)
      // 指标列与更新时间默认「大到小」，便于一眼看到最强的因子
      setSortDir(k === 'id' ? 1 : -1)
    }
  }

  const arrow = (k: SortKey) => (sortKey === k ? (sortDir === 1 ? ' ↑' : ' ↓') : '')

  return (
    <div className="lib-page">
      <div className="lib-head">
        <p>因子全部落库（含内置经典因子），可编辑、复制、导入导出；指标由同一套裁判批量计算，口径一致才可横向比较。</p>
        <div className="actions">
          <button className="btn" onClick={() => onNav('factory')}>
            写新因子
          </button>
          <button className="btn" onClick={() => fileRef.current?.click()}>
            导入
          </button>
          <a className="btn" href={api.exportUrl()} download>
            导出
          </a>
          <button className="btn primary" onClick={() => setEditing('new')}>
            + 新建因子
          </button>
        </div>
      </div>

      <input
        ref={fileRef}
        type="file"
        accept="application/json,.json"
        style={{ display: 'none' }}
        onChange={(e) => {
          const f = e.target.files?.[0]
          if (f) onImport(f)
        }}
      />

      {(msg || err) && (
        <div className={`note ${err ? 'err' : 'ok'} mb12`}>
          <span>{err ? '⚠' : '✓'}</span>
          <div>{err || msg}</div>
        </div>
      )}

      <div className="toolbar">
        <input type="text" placeholder="搜索名称 / 表达式…" value={q} onChange={(e) => setQ(e.target.value)} style={{ width: 230 }} />
        <Select
          value={fCat}
          onChange={setFCat}
          minWidth={150}
          items={[
            { value: '', label: '全部分类' },
            ...cats.map(([c, n]) => ({ value: c, label: `${catLabel(c)}（${n}）` })),
          ]}
        />
        <Select
          value={fSrc}
          onChange={setFSrc}
          minWidth={120}
          items={[
            { value: '', label: '全部来源' },
            { value: 'classic', label: '内置' },
            { value: 'user', label: '自建' },
            { value: 'llm', label: 'AI' },
            { value: 'composed', label: '组合' },
          ]}
        />
        <span className="grow" />
        <span className="subtle">
          {filtered.length} / {factors.length} 条
          {selected.size > 0 && ` · 选中 ${selected.size}`}
        </span>
        <button className="btn" onClick={() => batchEval([...selected])} disabled={busy || factors.length === 0}>
          {busy ? '评价中…' : selected.size > 0 ? `评价选中（${selected.size}）` : '评价全部'}
        </button>
      </div>

      {busy && <Busy text="批量评价进行中：每个因子都会跑一遍完整回测，请稍候…" />}

      {filtered.length === 0 ? (
        <div className="card">
          <Empty text="没有匹配的因子" hint="试试清空筛选条件，或到「因子工厂」新建一个" />
        </div>
      ) : (
        <div className="card tight" style={{ padding: 0 }}>
          <div className="tbl-wrap" style={{ border: 'none', maxHeight: 'calc(100vh - 320px)' }}>
            <table className="tbl">
              <thead>
                <tr>
                  <th style={{ width: 34 }}>
                    <input type="checkbox" checked={allChecked} onChange={toggleAll} />
                  </th>
                  <th>因子名称</th>
                  <th>分类</th>
                  <th>来源</th>
                  <th className="num sortable" onClick={() => sortBy('rank_ic')}>
                    RankIC{arrow('rank_ic')}
                  </th>
                  <th className="num sortable" onClick={() => sortBy('rank_ic_ir')}>
                    ICIR{arrow('rank_ic_ir')}
                  </th>
                  <th className="num sortable" onClick={() => sortBy('top_excess')}>
                    超额收益（多头）{arrow('top_excess')}
                  </th>
                  <th className="num sortable" style={{ width: 172 }} onClick={() => sortBy('updated')}>
                    更新时间{arrow('updated')}
                  </th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((f) => (
                  <tr key={f.id} className="clickable">
                    <td>
                      <input type="checkbox" checked={selected.has(f.id)} onChange={() => toggle(f.id)} />
                    </td>
                    <td className="name" style={{ cursor: 'pointer' }} onClick={() => setViewing(f)}>
                      <div className="nameline">
                        {f.name}
                        {!f.available && (
                          <span className="tag red" style={{ marginLeft: 6 }} title={f.unavailable_reason}>
                            当前数据源不支持
                          </span>
                        )}
                      </div>
                    </td>
                    <td>
                      <span className="tag gray">{catLabel(f.category)}</span>
                    </td>
                    <td>{srcTag(f.source)}</td>
                    <td className={`num ${signCls(f.summary?.rank_ic_mean)}`}>{f.summary ? fmt(f.summary.rank_ic_mean) : '—'}</td>
                    <td className={`num ${signCls(f.summary?.rank_ic_ir)}`}>{f.summary ? fmt(f.summary.rank_ic_ir, 2) : '—'}</td>
                    <td className={`num ${signCls(f.summary?.top_excess_cum)}`}>{f.summary ? pct(f.summary.top_excess_cum) : '—'}</td>
                    <td className="num subtle nowrap" style={{ fontSize: 11.5 }}>
                      {dt(f.updated_at)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {viewing && (
        <ViewDrawer
          factor={viewing}
          onClose={() => setViewing(null)}
          onEdit={() => {
            setEditing(viewing)
            setViewing(null)
          }}
          onCopy={async () => {
            await onCopy(viewing)
            setViewing(null)
          }}
          onDelete={async () => {
            if (await onDelete(viewing)) setViewing(null)
          }}
        />
      )}

      {editing && (
        <EditDrawer
          meta={meta}
          factor={editing === 'new' ? null : editing}
          onClose={() => setEditing(null)}
          onSaved={(m) => {
            setEditing(null)
            setMsg(m)
            onChanged()
          }}
        />
      )}
    </div>
  )
}

function ViewDrawer({
  factor,
  onClose,
  onEdit,
  onCopy,
  onDelete,
}: {
  factor: Factor
  onClose: () => void
  onEdit: () => void
  onCopy: () => void
  onDelete: () => void
}) {
  const [m, setM] = useState<Metrics | null>(null)
  const [busy, setBusy] = useState(true)
  const [err, setErr] = useState('')

  useEffect(() => {
    setBusy(true)
    setErr('')
    setM(null)
    api
      .evaluate(factor.expr)
      .then(setM)
      .catch((e) => setErr((e as Error).message))
      .finally(() => setBusy(false))
  }, [factor.id, factor.expr])

  return (
    <Drawer
      title={factor.name}
      subtitle={
        <>
          {catLabel(factor.category)} · {factor.source}
          {factor.desc ? ` · ${factor.desc}` : ''}
        </>
      }
      onClose={onClose}
      actions={
        <>
          <button className="btn sm" onClick={onCopy} title="复制为新因子">
            ⧉ 复制
          </button>
          <button className="btn sm" onClick={onDelete} title="删除因子">
            ✕ 删除
          </button>
          <button className="btn sm primary" onClick={onEdit}>
            编辑
          </button>
        </>
      }
    >
      <div className="card tight">
        <h3>表达式</h3>
        <pre className="expr">{factor.expr}</pre>
      </div>

      {busy && <Busy text="正在用当前面板重算指标…" />}
      {err && <div className="note err">{err}</div>}

      {m && (
        <>
          <div className="grid c2 mb12">
            <div className="kpi">
              <div className="lbl">RankIC 均值</div>
              <div className={`num ${signCls(m.rank_ic_mean)}`}>{fmt(m.rank_ic_mean)}</div>
              <div className="foot">t = {fmt(m.rank_ic_t, 1)} · P(IC&gt;0.02) = {pct(m.pic_002, 0)}</div>
            </div>
            <div className="kpi">
              <div className="lbl">多头累计超额</div>
              <div className={`num ${signCls(m.top_excess_cum)}`}>{pct(m.top_excess_cum)}</div>
              <div className="foot">换手 {pct(m.turnover_daily, 0)}/日 · 成本 {pct(m.cost_annual)}/年</div>
            </div>
          </div>
          <div className="card">
            <h3>
              分层回测 <span className="hint">Q1 最小 → Q{fmt(m.layer_mean.length, 0)} 最大</span>
            </h3>
            <LayerChart m={m} />
          </div>
          <div className="card">
            <h3>IC 时序</h3>
            <ICChart m={m} />
          </div>
          <div className="card">
            <h3>多头组合 vs 全市场</h3>
            <NavChart m={m} />
          </div>
        </>
      )}
    </Drawer>
  )
}

function EditDrawer({
  meta,
  factor,
  onClose,
  onSaved,
}: {
  meta: Meta | null
  factor: Factor | null
  onClose: () => void
  onSaved: (msg: string) => void
}) {
  const [name, setName] = useState(factor?.name ?? '')
  const [category, setCategory] = useState(factor?.category ?? 'user')
  const [desc, setDesc] = useState(factor?.desc ?? '')
  const [expr, setExpr] = useState(factor?.expr ?? 'mom20 = close / ts_delay(close, 20) - 1\nalpha = -1 * cs_rank(mom20)')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [check, setCheck] = useState<{ ok: boolean; errors: string[] } | null>(null)

  const taRef = useRef<HTMLTextAreaElement>(null)
  const fields = meta?.fields ?? []

  async function validate() {
    try {
      const r = await api.check(expr)
      setCheck({ ok: r.ok, errors: r.errors ?? [] })
      return r.ok
    } catch {
      return false
    }
  }

  async function save() {
    if (!name.trim()) {
      setErr('请填写名称')
      return
    }
    setBusy(true)
    setErr('')
    try {
      if (factor) {
        await api.updateFactor(factor.id, { name: name.trim(), expr, category, desc })
        onSaved(`已更新「${name.trim()}」`)
      } else {
        await api.createFactor({ name: name.trim(), expr, category, source: 'user', desc })
        onSaved(`已创建「${name.trim()}」`)
      }
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  function insert(text: string) {
    const ta = taRef.current
    if (!ta) return setExpr(expr + text)
    const s = ta.selectionStart
    const next = expr.slice(0, s) + text + expr.slice(ta.selectionEnd)
    setExpr(next)
    requestAnimationFrame(() => {
      ta.focus()
      ta.setSelectionRange(s + text.length, s + text.length)
    })
  }

  return (
    <Drawer
      title={factor ? `编辑 ${factor.name}` : '新建因子'}
      subtitle="表达式会先经过静态校验（字段存在性、算子白名单、窗口常数范围），通过后才允许保存"
      onClose={onClose}
      actions={
        <>
          <button className="btn sm" onClick={validate}>
            校验
          </button>
          <button className="btn sm primary" onClick={save} disabled={busy}>
            {busy ? '保存中…' : '保存'}
          </button>
        </>
      }
    >
      <div className="card tight">
        <div className="grid c2" style={{ gap: 10 }}>
          <div className="field">
            <label>名称</label>
            <input type="text" value={name} onChange={(e) => setName(e.target.value)} placeholder="如：短期反转_20日" />
          </div>
          <div className="field">
            <label>分类</label>
            <Select
              value={category}
              onChange={setCategory}
              items={Object.entries(CATEGORY_LABEL).map(([k, v]) => ({ value: k, label: v }))}
            />
          </div>
        </div>
        <div className="field mt8">
          <label>说明（可选）</label>
          <input type="text" value={desc} onChange={(e) => setDesc(e.target.value)} placeholder="一句话描述经济含义与假设" />
        </div>
      </div>

      <div className="card tight">
        <h3>
          表达式
          {check && (
            <span className="right">
              {check.ok ? <span className="tag green">✓ 通过</span> : <span className="tag red">✗ {check.errors.length} 个问题</span>}
            </span>
          )}
        </h3>
        <textarea ref={taRef} className="editor" value={expr} spellCheck={false} onChange={(e) => setExpr(e.target.value)} />
        <div className="row tight mt8">
          {fields.map((f) => (
            <button key={f} className="tag gray" style={{ cursor: 'pointer', border: '1px solid #e5eaf2' }} onClick={() => insert(f)}>
              {f}
            </button>
          ))}
        </div>
      </div>

      {check && !check.ok && <div className="note err">{check.errors.join('\n')}</div>}
      {err && <div className="note err">{err}</div>}
    </Drawer>
  )
}
