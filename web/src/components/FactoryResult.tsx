import { useState } from 'react'
import type { Metrics } from '../types'
import { ICChart, LayerChart, LayerNavChart, NavChart } from './Charts'
import { Busy, fmt, pct, signCls } from './Layout'

type Tab = 'layer' | 'layerNav' | 'ic' | 'nav'

/** 底部面板：评价结果（KPI + 四个图表视图） */
export function FactoryResult({ metrics, busy }: { metrics: Metrics | null; busy: boolean }) {
  const [tab, setTab] = useState<Tab>('layer')

  if (busy) return <Busy text="正在计算因子值并评价…" />

  if (!metrics) {
    return (
      <div className="fw-empty">
        <div className="fw-empty-mark">▤</div>
        <div>
          <b>还没有评价结果</b>
          <p className="subtle">点击上方「▶ 运行评价」或按 Ctrl+Enter，结果会以弹窗展示，也可在此回看。</p>
        </div>
      </div>
    )
  }

  return (
    <>
      <div className="fw-kpis">
        <div className="kpi">
          <div className="lbl">RankIC 均值</div>
          <div className={`num ${signCls(metrics.rank_ic_mean)}`}>{fmt(metrics.rank_ic_mean)}</div>
          <div className="foot">IC 均值 {fmt(metrics.ic_mean)}</div>
        </div>
        <div className="kpi">
          <div className="lbl">RankICIR</div>
          <div className="num">{fmt(metrics.rank_ic_ir, 2)}</div>
          <div className="foot">t 值 {fmt(metrics.rank_ic_t, 1)}</div>
        </div>
        <div className="kpi">
          <div className="lbl">多头累计超额</div>
          <div className={`num ${signCls(metrics.top_excess_cum)}`}>{pct(metrics.top_excess_cum)}</div>
          <div className="foot">年化成本 {pct(metrics.cost_annual)}</div>
        </div>
        <div className="kpi">
          <div className="lbl">P(IC&gt;0.02)</div>
          <div className="num">{pct(metrics.pic_002, 0)}</div>
          <div className="foot">覆盖度 {pct(metrics.coverage, 0)} · {metrics.n_dates} 个截面</div>
        </div>
      </div>

      <div className="fw-subtabs">
        <button className={tab === 'layer' ? 'on' : ''} onClick={() => setTab('layer')}>分层回测</button>
        <button className={tab === 'layerNav' ? 'on' : ''} onClick={() => setTab('layerNav')}>分层净值</button>
        <button className={tab === 'ic' ? 'on' : ''} onClick={() => setTab('ic')}>IC 时序</button>
        <button className={tab === 'nav' ? 'on' : ''} onClick={() => setTab('nav')}>多头 vs 全市场</button>
        <span className="right subtle">Q1 = 因子最小 · Q{metrics.layer_mean.length} = 因子最大 · {metrics.n_dates} 个截面</span>
      </div>

      {tab === 'layer' && (
        <>
          <LayerChart m={metrics} />
          <p className="subtle mt8">单调性看柱状是否逐层抬升；分层交叉（如两端都高）通常意味着因子与收益非线性或分组方向被反转。</p>
        </>
      )}
      {tab === 'layerNav' && <LayerNavChart m={metrics} />}
      {tab === 'ic' && <ICChart m={metrics} />}
      {tab === 'nav' && (
        <>
          <NavChart m={metrics} />
          <p className="subtle mt8">多头 = 因子最高分位等权；净曲线已扣单边成本。A 股无个人空头腿，以多头净超额为主结论口径。</p>
        </>
      )}
    </>
  )
}
