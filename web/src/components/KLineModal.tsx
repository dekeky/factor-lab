import { useEffect, useRef, useState } from 'react'
import { createChart, ColorType, CrosshairMode, type IChartApi, type Time } from 'lightweight-charts'
import { api } from '../api'
import { Modal } from './Modal'
import { tvPageUrl, tvSymbol } from '../lib/tv'
import { colorAlpha, cssVar } from '../lib/theme'

function klineChrome() {
  const sheet = cssVar('--sheet', '#161616')
  const ink = cssVar('--ink-2', '#d4d4d4')
  const line = cssVar('--line', '#2a2a2a')
  const up = cssVar('--up', '#e54545')
  const down = cssVar('--down', '#12a36e')
  return { sheet, ink, line, up, down }
}

export type KLineTarget = {
  code: string
  name?: string
  label?: string
}

function ymdSlash(time: Time): string {
  if (typeof time === 'string') {
    return time.slice(0, 10).replace(/-/g, '/')
  }
  if (typeof time === 'object' && time && 'year' in time) {
    return `${time.year}/${pad2(time.month)}/${pad2(time.day)}`
  }
  const d = new Date(Number(time) * 1000)
  return `${d.getUTCFullYear()}/${pad2(d.getUTCMonth() + 1)}/${pad2(d.getUTCDate())}`
}

function pad2(n: number) {
  return String(n).padStart(2, '0')
}

export function stockTitle(row: KLineTarget) {
  if (row.label) return row.label
  if (row.name) return `${row.name}（${row.code}）`
  return row.code
}

export function StockName({
  row,
  onOpen,
}: {
  row: KLineTarget
  onOpen: (row: KLineTarget) => void
}) {
  return (
    <button type="button" className="stock-link" title="查看 K 线" onClick={() => onOpen(row)}>
      {stockTitle(row)}
    </button>
  )
}

export function KLineModal({ target, onClose }: { target: KLineTarget; onClose: () => void }) {
  const symbol = tvSymbol(target.code)
  const wrapRef = useRef<HTMLDivElement>(null)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(true)

  useEffect(() => {
    const el = wrapRef.current
    if (!el) return
    let disposed = false
    let chart: IChartApi | null = null
    let ro: ResizeObserver | null = null
    let onTheme: (() => void) | null = null
    setErr('')
    setBusy(true)

    api
      .kline(target.code)
      .then((kl) => {
        if (disposed) return
        if (!kl.bars.length) {
          setBusy(false)
          setErr('这只股票没有可用 K 线')
          return
        }
        const create = () => {
          if (disposed || !el) return
          const rect = el.getBoundingClientRect()
          if (rect.width < 2 || rect.height < 2) {
            requestAnimationFrame(create)
            return
          }
          const c = klineChrome()
          chart = createChart(el, {
            layout: {
              background: { type: ColorType.Solid, color: c.sheet },
              textColor: c.ink,
              fontFamily: 'Manrope, PingFang SC, Microsoft YaHei, sans-serif',
            },
            grid: {
              vertLines: { color: c.line },
              horzLines: { color: c.line },
            },
            crosshair: { mode: CrosshairMode.Normal },
            rightPriceScale: { borderColor: c.line },
            localization: {
              locale: 'zh-CN',
              dateFormat: 'yyyy/MM/dd',
              timeFormatter: ymdSlash,
            },
            timeScale: {
              borderColor: c.line,
              timeVisible: false,
              tickMarkMaxCharacterLength: 10,
              tickMarkFormatter: (time: Time) => ymdSlash(time),
            },
            width: Math.floor(rect.width),
            height: Math.floor(rect.height),
          })
          const candle = chart.addCandlestickSeries({
            upColor: c.up,
            downColor: c.down,
            borderUpColor: c.up,
            borderDownColor: c.down,
            wickUpColor: c.up,
            wickDownColor: c.down,
          })
          candle.setData(
            kl.bars.map((b) => ({
              time: b.time as Time,
              open: b.open,
              high: b.high,
              low: b.low,
              close: b.close,
            })),
          )
          const vol = chart.addHistogramSeries({
            priceFormat: { type: 'volume' },
            priceScaleId: 'vol',
          })
          chart.priceScale('vol').applyOptions({ scaleMargins: { top: 0.78, bottom: 0 } })
          vol.setData(
            kl.bars.map((b) => ({
              time: b.time as Time,
              value: b.volume,
              color: b.close >= b.open ? colorAlpha(c.up, 0.45) : colorAlpha(c.down, 0.45),
            })),
          )
          chart.timeScale().fitContent()
          const vis = chart.timeScale().getVisibleLogicalRange()
          if (vis) {
            chart.timeScale().setVisibleLogicalRange({ from: vis.from - 10, to: vis.to })
          }
          onTheme = () => {
            if (!chart) return
            const t = klineChrome()
            chart.applyOptions({
              layout: {
                background: { type: ColorType.Solid, color: t.sheet },
                textColor: t.ink,
              },
              grid: {
                vertLines: { color: t.line },
                horzLines: { color: t.line },
              },
              rightPriceScale: { borderColor: t.line },
              timeScale: { borderColor: t.line },
            })
            candle.applyOptions({
              upColor: t.up,
              downColor: t.down,
              borderUpColor: t.up,
              borderDownColor: t.down,
              wickUpColor: t.up,
              wickDownColor: t.down,
            })
          }
          window.addEventListener('fl-theme', onTheme)
          ro = new ResizeObserver(() => {
            if (!chart || !el) return
            const r = el.getBoundingClientRect()
            chart.applyOptions({ width: Math.floor(r.width), height: Math.floor(r.height) })
          })
          ro.observe(el)
          setBusy(false)
        }
        create()
      })
      .catch((e) => {
        if (!disposed) {
          setBusy(false)
          setErr((e as Error).message)
        }
      })

    return () => {
      disposed = true
      if (onTheme) window.removeEventListener('fl-theme', onTheme)
      ro?.disconnect()
      chart?.remove()
    }
  }, [target.code])

  return (
    <Modal
      title={stockTitle(target)}
      subtitle={`TradingView · ${symbol} · 前复权日K`}
      width={1120}
      onClose={onClose}
      footer={
        <a className="btn" href={tvPageUrl(symbol)} target="_blank" rel="noreferrer">
          在 TradingView 打开
        </a>
      }
    >
      {err && (
        <div className="note err mb12">
          <span>⚠</span>
          <div>{err}</div>
        </div>
      )}
      <div className="tv-frame">
        {busy && !err && <div className="tv-loading">加载 K 线…</div>}
        <div className="tv-chart" ref={wrapRef} />
      </div>
    </Modal>
  )
}
