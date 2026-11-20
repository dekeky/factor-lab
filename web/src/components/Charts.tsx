import { useMemo } from 'react'
import type { EChartsOption, SeriesOption } from 'echarts'
import { Chart } from './Chart'
import type { Metrics, PaperDay, PaperTrade } from '../types'

// 转成百分数数值：null/NaN 一律返回 null（ECharts 认作断点）。
// 注意不能复用 pct()——它返回 "—" 字符串，会让整个 series 画不出来。
const pctNum = (v: number | null | undefined): number | null => {
  if (v === null || v === undefined) return null
  const n = Number(v)
  if (!Number.isFinite(n)) return null
  return Math.round(n * 10000) / 100
}

// 把 [日期数组, 数值数组] 配成 [[date, value], ...]，供 time 轴使用。
// 用 time 轴而非 category 轴：上千个交易日时标签由 ECharts 按时间刻度自动稀疏，
// category 轴的 axisLabel.interval 在 dataZoom 下并不可靠（实测仍会挤成一团）。
const pairs = (dates: string[] | undefined, vals: Array<number | null | undefined> | undefined) => {
  const ds = dates ?? []
  const vs = vals ?? []
  return ds.map((d, i) => [d, numOrNull(vs[i])])
}

const numOrNull = (v: number | null | undefined): number | null => {
  if (v === null || v === undefined) return null
  const n = Number(v)
  return Number.isFinite(n) ? n : null
}

// 分层图：各层平均单期收益柱状 + 累计收益折线（双轴）
export function LayerChart({ m }: { m: Metrics }) {
  const G = m.layer_mean?.length ?? 0
  const labels = Array.from({ length: G }, (_, i) => `Q${i + 1}`)
  const option: EChartsOption = {
    tooltip: { trigger: 'axis' },
    legend: { data: ['平均单期收益(%)', '累计收益(%)'] },
    grid: { left: 56, right: 56, bottom: 30, top: 40 },
    xAxis: { type: 'category', data: labels, name: '因子升序', axisLabel: { hideOverlap: true } },
    yAxis: [
      { type: 'value', name: '单期(%)' },
      { type: 'value', name: '累计(%)' },
    ],
    series: [
      {
        name: '平均单期收益(%)',
        type: 'bar',
        data: (m.layer_mean ?? []).map(pctNum),
        itemStyle: { color: '#2563eb' },
      },
      {
        name: '累计收益(%)',
        type: 'line',
        yAxisIndex: 1,
        data: (m.layer_cum ?? []).map(pctNum),
        itemStyle: { color: '#b7791f' },
      },
    ],
  }
  return <Chart option={option} />
}

// IC 时序：IC 与 RankIC 按日折线 + 0.02 参考线
export function ICChart({ m }: { m: Metrics }) {
  const option: EChartsOption = {
    tooltip: { trigger: 'axis' },
    legend: { data: ['IC', 'RankIC'] },
    grid: { left: 56, right: 20, bottom: 30, top: 40 },
    xAxis: { type: 'time', axisLabel: { hideOverlap: true } },
    yAxis: { type: 'value', name: 'IC' },
    dataZoom: [{ type: 'inside' }, { type: 'slider', height: 16, bottom: 4 }],
    series: [
      {
        name: 'IC',
        type: 'line',
        showSymbol: false,
        data: pairs(m.dates, m.ic_by_date),
        lineStyle: { color: '#9db3cf', width: 1 },
      },
      {
        name: 'RankIC',
        type: 'line',
        showSymbol: false,
        data: pairs(m.dates, m.rank_ic_by_date),
        lineStyle: { color: '#2563eb', width: 1.5 },
      },
    ],
  }
  return <Chart option={option} />
}

// 净值曲线：多头毛/净 vs 全市场
export function NavChart({ m }: { m: Metrics }) {
  const option: EChartsOption = {
    tooltip: { trigger: 'axis' },
    legend: { data: ['多头(毛)', '多头(净)', '全市场'] },
    grid: { left: 56, right: 20, bottom: 30, top: 40 },
    xAxis: { type: 'time', axisLabel: { hideOverlap: true } },
    yAxis: { type: 'value', name: '净值', scale: true },
    dataZoom: [{ type: 'inside' }, { type: 'slider', height: 16, bottom: 4 }],
    series: [
      { name: '多头(毛)', type: 'line', showSymbol: false, data: pairs(m.dates, m.top_nav), lineStyle: { color: '#c0392b' } },
      { name: '多头(净)', type: 'line', showSymbol: false, data: pairs(m.dates, m.top_nav_net), lineStyle: { color: '#b7791f' } },
      { name: '全市场', type: 'line', showSymbol: false, data: pairs(m.dates, m.universe_nav), lineStyle: { color: '#5b6b7c' } },
    ],
  }
  return <Chart option={option} />
}

// 分层净值
export function LayerNavChart({ m }: { m: Metrics }) {
  const G = m.layer_nav?.length ?? 0
  const palette = ['#94b8e3', '#7ea6e8', '#5f8fe0', '#4a7ede', '#346cd6', '#2563eb', '#1d4fae', '#17408c', '#123268', '#0c2447']
  const option: EChartsOption = {
    tooltip: { trigger: 'axis' },
    legend: { data: Array.from({ length: G }, (_, i) => `Q${i + 1}`), top: 0 },
    grid: { left: 56, right: 20, bottom: 30, top: 56 },
    xAxis: { type: 'time', axisLabel: { hideOverlap: true } },
    yAxis: { type: 'value', name: '净值', scale: true },
    dataZoom: [{ type: 'inside' }, { type: 'slider', height: 16, bottom: 4 }],
    series: (m.layer_nav ?? []).map((nav, i) => ({
      name: `Q${i + 1}`,
      type: 'line' as const,
      showSymbol: false,
      data: pairs(m.dates, nav),
      lineStyle: { color: palette[i % palette.length], width: i === 0 || i === G - 1 ? 2 : 1 },
    })),
  }
  return <Chart option={option} />
}

type TradeMark = 'buy' | 'sell' | 'both'

const TRADE_MARK = {
  buy: {
    name: '买入',
    color: '#c0392b',
    symbol: 'triangle' as const,
    rotate: 0,
    size: 12,
    offset: [0, 10] as [number, number],
  },
  sell: {
    name: '卖出',
    color: '#1e8449',
    symbol: 'triangle' as const,
    rotate: 180,
    size: 12,
    offset: [0, -10] as [number, number],
  },
}

function filledTrades(day: PaperDay): PaperTrade[] {
  return (day.trades ?? []).filter((t) => t.status === 'filled' && t.shares > 0)
}

function holdingCodes(day: PaperDay | undefined): Set<string> {
  return new Set((day?.holdings ?? []).filter((h) => h.shares > 0).map((h) => h.code))
}

/** 只标换股日（持仓名单变化），避免每日再平衡把净值线盖满。 */
function tradeMarkKind(day: PaperDay, prev?: PaperDay): TradeMark | null {
  const trades = filledTrades(day)
  if (trades.length === 0) return null
  const before = holdingCodes(prev)
  const after = holdingCodes(day)
  let entered = false
  let exited = false
  for (const code of after) if (!before.has(code)) entered = true
  for (const code of before) if (!after.has(code)) exited = true
  if (!entered && !exited) return null
  if (entered && exited) return 'both'
  if (entered) return 'buy'
  return 'sell'
}

function nearestDay(dates: string[], day: string): string {
  if (dates.includes(day)) return day
  const t = Date.parse(day)
  if (!Number.isFinite(t) || dates.length === 0) return day
  let best = dates[0]
  let bestDiff = Math.abs(Date.parse(dates[0]) - t)
  for (const d of dates) {
    const diff = Math.abs(Date.parse(d) - t)
    if (diff < bestDiff) {
      bestDiff = diff
      best = d
    }
  }
  return best
}

function moneyAxis(v: number): string {
  return v.toLocaleString('zh-CN', { maximumFractionDigits: 0 })
}

export function PaperNavChart({
  days,
  capital,
  onInspectDay,
}: {
  days: PaperDay[]
  capital: number
  onInspectDay?: (signalDate: string) => void
}) {
  const clickable = Boolean(onInspectDay)
  const { option, dates, byFill } = useMemo(() => {
    const dates = days.map((d) => d.fill_date)
    const nav = days.map((d) => d.nav)
    const byFill = new Map<string, { signal: string; kind: TradeMark | null; buys: number; sells: number }>()
    const marks: Record<'buy' | 'sell', Array<[string, number]>> = { buy: [], sell: [] }
    days.forEach((d, i) => {
      const trades = filledTrades(d)
      const kind = tradeMarkKind(d, days[i - 1])
      byFill.set(d.fill_date, {
        signal: d.date,
        kind,
        buys: trades.filter((t) => t.side === 'buy').length,
        sells: trades.filter((t) => t.side === 'sell').length,
      })
      // 换股日同时画出买卖三角：贴在净值线上下，避免再被折线盖住。
      if (kind === 'buy' || kind === 'both') marks.buy.push([d.fill_date, d.nav])
      if (kind === 'sell' || kind === 'both') marks.sell.push([d.fill_date, d.nav])
    })
    const legend = ['净值', '本金']
    const series: SeriesOption[] = [
      { name: '净值', type: 'line', showSymbol: false, data: pairs(dates, nav), lineStyle: { color: '#c0392b', width: 1.6 }, z: 3 },
      {
        name: '本金',
        type: 'line',
        showSymbol: false,
        data: dates.map((d) => [d, capital]),
        lineStyle: { color: '#9aa8bd', type: 'dashed', width: 1 },
        z: 2,
      },
    ]
    for (const kind of ['buy', 'sell'] as const) {
      const spec = TRADE_MARK[kind]
      if (marks[kind].length === 0) continue
      legend.push(spec.name)
      series.push({
        name: spec.name,
        type: 'scatter',
        clip: false,
        data: marks[kind],
        symbol: spec.symbol,
        symbolRotate: spec.rotate,
        symbolSize: spec.size,
        symbolOffset: spec.offset,
        itemStyle: {
          color: spec.color,
          borderColor: '#fff',
          borderWidth: 1,
        },
        tooltip: { show: false },
        z: 6,
      })
    }
    const option: EChartsOption = {
      tooltip: {
        trigger: 'axis',
        formatter: (raw) => {
          const list = Array.isArray(raw) ? raw : [raw]
          const first = list[0] as { axisValue?: string | number; value?: unknown }
          const fill = nearestDay(dates, ymdHint(first?.axisValue ?? first?.value))
          const row = byFill.get(fill)
          const navPt = list.find((p) => (p as { seriesName?: string }).seriesName === '净值') as { value?: unknown } | undefined
          const navVal = Array.isArray(navPt?.value) ? Number(navPt.value[1]) : Number.NaN
          const lines = [`<b>${fill}</b>`]
          if (Number.isFinite(navVal)) lines.push(`净值 ${moneyAxis(navVal)}`)
          if (row && (row.buys || row.sells)) {
            const bits = []
            if (row.buys) bits.push(`买 ${row.buys}`)
            if (row.sells) bits.push(`卖 ${row.sells}`)
            lines.push(bits.join(' · '))
          }
          if (clickable) lines.push('<span style="color:#6b7a91">点击查看当日明细</span>')
          return lines.join('<br/>')
        },
      },
      legend: { data: legend },
      grid: { left: 64, right: 20, bottom: 30, top: 40 },
      xAxis: { type: 'time', axisLabel: { hideOverlap: true } },
      yAxis: { type: 'value', name: '净值', scale: true },
      dataZoom: [{ type: 'inside' }, { type: 'slider', height: 16, bottom: 4 }],
      series,
    }
    return { option, dates, byFill }
  }, [days, capital, clickable])
  return (
    <Chart
      option={option}
      clickable={clickable}
      onAxisClick={
        onInspectDay
          ? (day) => {
              const fill = nearestDay(dates, day)
              onInspectDay(byFill.get(fill)?.signal ?? fill)
            }
          : undefined
      }
    />
  )
}

function ymdHint(v: unknown): string {
  if (typeof v === 'string') return v.slice(0, 10)
  if (Array.isArray(v) && v.length) return ymdHint(v[0])
  if (typeof v === 'number' && Number.isFinite(v)) {
    const d = new Date(v)
    if (!Number.isNaN(d.getTime())) {
      return `${d.getUTCFullYear()}-${String(d.getUTCMonth() + 1).padStart(2, '0')}-${String(d.getUTCDate()).padStart(2, '0')}`
    }
  }
  return ''
}
