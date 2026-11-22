import type { Factor, Meta, Metrics } from '../types'
import { canonicalIndex, universeList, universeName, type Universe } from '../lib/universes'
import { CATEGORY_LABEL } from './Layout'
import { DatePicker } from './DatePicker'
import { Select, type OptGroup } from './Select'

type Operator = Meta['operators'][number]

export interface Params {
  horizon: number
  groups: number
  cost: number
  start: string
  end: string
  universe: string
}

const EVAL_YEARS = 5

function shiftYears(iso: string, years: number): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso.trim())
  if (!m) return iso
  const d = new Date(+m[1] + years, +m[2] - 1, +m[3])
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

/** 评价区间默认最近五年，且不超出数据面板起止。 */
export function defaultEvalRange(range?: { from: string; to: string }): { start: string; end: string } {
  const from = range?.from?.trim() ?? ''
  const to = range?.to?.trim() ?? ''
  if (!to) return { start: '', end: '' }
  const start5 = shiftYears(to, -EVAL_YEARS)
  const start = from && from > start5 ? from : start5
  return { start, end: to }
}

function poolGroups(list: Universe[], current?: string): OptGroup[] {
  const items = [...list]
  const cur = canonicalIndex(current)
  if (cur && !items.some((u) => u.code === cur)) {
    items.unshift({ code: cur, name: universeName(cur, list) || cur, group: '当前' })
  }
  const groups: OptGroup[] = []
  for (const u of items) {
    let g = groups.find((x) => x.label === u.group)
    if (!g) {
      g = { label: u.group, items: [] }
      groups.push(g)
    }
    g.items.push({ value: u.code, label: u.name, hint: u.code })
  }
  return groups
}

export function ParamsPanel({
  p,
  setP,
  universes,
  switchable,
  currentPool,
}: {
  p: Params
  setP: (v: Params) => void
  universes?: Universe[]
  switchable?: boolean
  currentPool?: string
}) {
  const set = (k: keyof Params, v: number | string) => setP({ ...p, [k]: v })
  const list = universeList(universes)
  const selected = canonicalIndex(p.universe) || canonicalIndex(currentPool)
  const groups = poolGroups(list, currentPool)
  const canSwitch = switchable !== false

  return (
    <div className="eval-form">
      <div className="field">
        <label>股票池</label>
        <div className="eval-select">
          <Select
            value={selected}
            onChange={(v) => set('universe', v)}
            groups={groups}
            minWidth={420}
            maxHeight={380}
            disabled={!canSwitch}
          />
        </div>
        <p className="field-hint">
          {canSwitch
            ? '按所选指数最近一期成分评价。第一次选新指数需要拉取该池行情。'
            : '当前数据源只有一份股票池，无法按指数切换。'}
        </p>
      </div>

      <div className="eval-form-row">
        <div className="field">
          <label>分层数</label>
          <input type="number" min={2} max={20} value={p.groups} onChange={(e) => set('groups', +e.target.value || 10)} />
          <p className="field-hint">默认十分位分组。</p>
        </div>
        <div className="field">
          <label>持有期</label>
          <div className="eval-input">
            <input type="number" min={1} max={20} value={p.horizon} onChange={(e) => set('horizon', +e.target.value || 1)} />
            <span>日</span>
          </div>
          <p className="field-hint">T+1 收盘后持有。</p>
        </div>
        <div className="field">
          <label>单边成本</label>
          <div className="eval-input">
            <input type="number" min={0} step={0.05} value={p.cost} onChange={(e) => set('cost', +e.target.value || 0)} />
            <span>%</span>
          </div>
          <p className="field-hint">含佣金与滑点。</p>
        </div>
      </div>

      <div className="field">
        <label>评价区间</label>
        <div className="eval-range">
          <DatePicker value={p.start} onChange={(v) => set('start', v)} placeholder="起始日期" />
          <span className="eval-range-sep">至</span>
          <DatePicker value={p.end} onChange={(v) => set('end', v)} placeholder="结束日期" />
        </div>
        <p className="field-hint">默认最近五年；留空则使用当前数据面板的完整区间。</p>
      </div>
    </div>
  )
}

export function RefPanel({
  fields,
  ops,
  onInsert,
}: {
  fields: string[]
  ops: Operator[]
  onInsert: (text: string) => void
}) {
  return (
    <div className="fw-panel">
      <div className="flex between center mb8">
        <div className="subtle">点击插入到光标处 · 字段来自当前数据面板，算子来自引擎白名单</div>
        <a className="help-inline-link" href="#/help">查看完整 DSL 文档</a>
      </div>
      <div className="fw-ref-label">数据字段</div>
      <div className="row tight mb8">
        {fields.map((f) => (
          <button key={f} className="tag gray fw-chip" onClick={() => onInsert(f)}>{f}</button>
        ))}
      </div>
      <div className="fw-ref-label">算子（ts_* 沿时间轴，cs_* 在横截面）</div>
      <div className="row tight">
        {ops.map((o) => (
          <button
            key={o.name}
            className="tag blue fw-chip"
            title={`参数 ${o.min_args}~${o.max_args}`}
            onClick={() => onInsert(`${o.name}(`)}
          >
            {o.name}
          </button>
        ))}
      </div>
    </div>
  )
}

export interface SaveState {
  name: string
  cat: string
  mode: 'new' | 'update'
  updateId: number | null
}

export function SavePanel({
  metrics,
  save,
  setSave,
  onSave,
  msg,
  library,
  onPickUpdate,
}: {
  metrics: Metrics | null
  save: SaveState
  setSave: (v: SaveState) => void
  onSave: () => void
  msg: string
  library: Factor[]
  onPickUpdate: (f: Factor) => void
}) {
  const grouped = new Map<string, Factor[]>()
  for (const f of library) {
    const k = f.category || 'other'
    if (!grouped.has(k)) grouped.set(k, [])
    grouped.get(k)!.push(f)
  }
  return (
    <div className="fw-panel">
      <div className="fw-panel-grid">
        <div className="field">
          <label>因子名称</label>
          <input type="text" value={save.name} onChange={(e) => setSave({ ...save, name: e.target.value })} placeholder="如：短期反转_20日" />
        </div>
        <div className="field">
          <label>分类</label>
          <Select
            value={save.cat}
            onChange={(v) => setSave({ ...save, cat: v })}
            items={[
              ...Object.entries(CATEGORY_LABEL)
                .filter(([k]) => !['classic', 'combo', 'user'].includes(k))
                .map(([k, v]) => ({ value: k, label: v })),
              { value: 'user', label: '自建' },
            ]}
          />
        </div>
        <div className="field fw-span2">
          <label>更新已有因子（选择后按当前表达式覆盖）</label>
          <Select
            value=""
            onChange={(v) => {
              const f = library.find((x) => String(x.id) === v)
              if (f) onPickUpdate(f)
            }}
            placeholder={`从因子库选择…（${library.length}）`}
            minWidth={240}
            groups={[...grouped.entries()].map(([cat, fs]) => ({
              label: `${cat}（${fs.length}）`,
              items: fs.map((f) => ({
                value: String(f.id),
                label: f.name,
                hint: f.summary ? f.summary.rank_ic_mean.toFixed(3) : undefined,
              })),
            }))}
          />
        </div>
      </div>
      <div className="row mt8">
        <button className="btn primary sm" onClick={onSave} disabled={!save.name.trim()}>
          {save.mode === 'update' ? '保存更新' : '新建并入库'}
        </button>
        <button className="btn sm" onClick={() => setSave({ ...save, mode: 'new', updateId: null })}>作为新因子</button>
        {save.mode === 'update' && <span className="tag amber">正在编辑因子库中的 #{save.updateId}</span>}
      </div>
      {msg && <div className="note ok mt8">{msg}</div>}
      <p className="subtle mt8">
        <b>口径提示</b>：A 股个人无空头腿，结论以「多头净超额」为主；多空收益仅作研究参考。表达式可随时入库，无需先评价。
      </p>
    </div>
  )
}
