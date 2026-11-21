import { useMemo } from 'react'
import type { EChartsOption, SeriesOption } from 'echarts'
import { Chart } from './Chart'
import { cssVar, useTheme } from '../lib/theme'
import type { Metrics, PaperFill, PaperSnapshot } from '../types'

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
  useTheme()
  const up = cssVar('--up', '#e54545')
  const down = cssVar('--down', '#12a36e')
  const sub = cssVar('--sub', '#a3a3a3')
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
        data: (m.layer_mean ?? []).map((v) => {
          const n = pctNum(v)
          const color = n == null || n === 0 ? sub : n > 0 ? up : down
          return { value: n, itemStyle: { color } }
        }),
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
  useTheme()
  const ink = cssVar('--ink', '#f5f5f5')
  const sub = cssVar('--sub', '#a3a3a3')
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
        lineStyle: { color: sub, width: 1 },
      },
      {
        name: 'RankIC',
        type: 'line',
        showSymbol: false,
        data: pairs(m.dates, m.rank_ic_by_date),
        lineStyle: { color: ink, width: 1.5 },
      },
    ],
  }
  return <Chart option={option} />
}

// 净值曲线：多头毛/净 vs 全市场
export function NavChart({ m }: { m: Metrics }) {
  useTheme()
  const sub = cssVar('--sub', '#a3a3a3')
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
      { name: '全市场', type: 'line', showSymbol: false, data: pairs(m.dates, m.universe_nav), lineStyle: { color: sub } },
    ],
  }
  return <Chart option={option} />
}

// 分层净值
export function LayerNavChart({ m }: { m: Metrics }) {
  useTheme()
  const G = m.layer_nav?.length ?? 0
  const palette = ['#8a9a94', '#7d8a86', '#8c7a74', '#a06c64', '#b45c52', '#c44536', '#a3382c', '#8a2e24', '#6f241c', cssVar('--ink', '#f5f5f5')]
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

function fillDay(f: PaperFill): string {
  return (f.date || '').slice(0, 10)
}

function filledOn(fills: PaperFill[] | undefined, day: string): PaperFill[] {
  return (fills ?? []).filter((f) => f.status === 'filled' && f.shares > 0 && fillDay(f) === day)
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
  snapshots,
  fills,
  capital,
  onInspectDay,
}: {
  snapshots: PaperSnapshot[]
  fills?: PaperFill[]
  capital: number
  onInspectDay?: (date: string) => void
}) {
  const theme = useTheme()
  const clickable = Boolean(onInspectDay)
  const { option, dates } = useMemo(() => {
    const dates = snapshots.map((d) => d.date)
    const nav = snapshots.map((d) => d.nav)
    const byFill = new Map<string, { buys: number; sells: number }>()
    const marks: Record<'buy' | 'sell', Array<[string, number]>> = { buy: [], sell: [] }
    snapshots.forEach((d) => {
      const trades = filledOn(fills, d.date)
      const buys = trades.filter((t) => t.side === 'buy').length
      const sells = trades.filter((t) => t.side === 'sell').length
      byFill.set(d.date, { buys, sells })
      if (buys) marks.buy.push([d.date, d.nav])
      if (sells) marks.sell.push([d.date, d.nav])
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
          borderColor: cssVar('--sheet', '#161616'),
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
    return { option, dates }
  }, [snapshots, fills, capital, clickable, theme])
  return (
    <Chart
      option={option}
      clickable={clickable}
      onAxisClick={
        onInspectDay
          ? (day) => {
              onInspectDay(nearestDay(dates, day))
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
