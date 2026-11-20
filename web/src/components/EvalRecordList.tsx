import { fmt, pct, signCls } from './Layout'
import { type EvalRecord } from '../lib/evalHistory'

function evalDate(ts: number) {
  const d = new Date(ts)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

function factorName(fileName: string) {
  return fileName.replace(/\.dsl$/i, '')
}

/** 评价记录列表：每条一行，名称 / RankIC / ICIR / 超额收益 / 评价日期 / 详情。 */
export function EvalRecordList({
  records,
  onView,
  onClear,
}: {
  records: EvalRecord[]
  onView: (r: EvalRecord) => void
  onClear: () => void
}) {
  if (records.length === 0) {
    return (
      <div className="fw-empty">
        <div className="fw-empty-mark">◷</div>
        <div>
          <b>还没有评价记录</b>
          <p className="subtle">每次「运行评价」都会把表达式、参数与关键指标存成一条记录（保存在本机浏览器，最多 6 条）。</p>
        </div>
      </div>
    )
  }

  return (
    <div className="rec-list">
      <div className="rec-head">
        <span className="subtle">共 {records.length} 条 · 保存在本机浏览器</span>
        <button className="btn xs ghost" onClick={onClear}>清空</button>
      </div>
      <div className="rec-row rec-row-head">
        <span>名称</span>
        <span>RankIC</span>
        <span>ICIR</span>
        <span>超额收益</span>
        <span>评价日期</span>
        <span />
      </div>
      {records.map((r) => (
        <div key={r.id} className="rec-row">
          <span className="rec-file" title={factorName(r.fileName)}>{factorName(r.fileName)}</span>
          <b className={signCls(r.metrics.rank_ic_mean)}>{fmt(r.metrics.rank_ic_mean)}</b>
          <b>{fmt(r.metrics.rank_ic_ir, 2)}</b>
          <b className={signCls(r.metrics.top_excess_cum)}>{pct(r.metrics.top_excess_cum)}</b>
          <span className="rec-dates">{evalDate(r.ts)}</span>
          <button className="btn xs" onClick={() => onView(r)}>详情</button>
        </div>
      ))}
    </div>
  )
}
