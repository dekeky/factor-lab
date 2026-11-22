import { useEffect, useRef } from 'react'
import * as echarts from 'echarts'
import { cssVar } from '../lib/theme'

function paint(inst: echarts.ECharts, option: echarts.EChartsOption) {
  const ink = cssVar('--ink', '#f5f5f5')
  const sub = cssVar('--sub', '#a3a3a3')
  const line = cssVar('--line', '#2a2a2a')
  const sheet = cssVar('--sheet', '#161616')
  inst.setOption(option)
  inst.setOption({
    backgroundColor: 'transparent',
    textStyle: { color: ink },
    legend: { textStyle: { color: sub } },
    tooltip: {
      backgroundColor: sheet,
      borderColor: line,
      textStyle: { color: ink },
    },
  })
}

// useChart: 挂载 ECharts 实例并在 option 变化时更新。
//
// 健壮性要点（历史问题：图表空白）：
//  1. 容器尺寸为 0 时（后台标签页 / 隐藏容器 / 首帧未布局）不能 init，
//     否则 ECharts 会记下 0×0，之后即使容器可见也不再自行重绘 → 永久空白。
//     这里用 requestAnimationFrame 轮询等待尺寸就绪再初始化。
//  2. 页面重新可见（visibilitychange）或窗口尺寸变化时强制 resize。
//  3. option 可能早于实例创建到达，用 ref 暂存，实例创建后立即应用。
function ymdFromAxis(x: unknown): string {
  if (typeof x === 'string') return x.slice(0, 10)
  if (typeof x === 'number' && Number.isFinite(x)) {
    const d = new Date(x)
    if (!Number.isNaN(d.getTime())) {
      return `${d.getUTCFullYear()}-${String(d.getUTCMonth() + 1).padStart(2, '0')}-${String(d.getUTCDate()).padStart(2, '0')}`
    }
  }
  return ''
}

export function useChart(option: echarts.EChartsOption | null, onAxisClick?: (day: string) => void) {
  const ref = useRef<HTMLDivElement>(null)
  const instRef = useRef<echarts.ECharts | null>(null)
  const optionRef = useRef<echarts.EChartsOption | null>(option)
  const clickRef = useRef(onAxisClick)

  optionRef.current = option
  clickRef.current = onAxisClick

  useEffect(() => {
    const el = ref.current
    if (!el) return
    let raf = 0
    let disposed = false
    let ro: ResizeObserver | null = null
    let zrClick: ((e: { offsetX: number; offsetY: number }) => void) | null = null

    const create = () => {
      if (disposed) return
      const rect = el.getBoundingClientRect()
      if (rect.width < 2 || rect.height < 2) {
        // 容器还没有尺寸，下一帧再试（最多持续到有尺寸为止）
        raf = requestAnimationFrame(create)
        return
      }
      const inst = echarts.init(el, undefined, { renderer: 'canvas' })
      instRef.current = inst
      if (optionRef.current) paint(inst, optionRef.current)
      zrClick = (e: { offsetX: number; offsetY: number }) => {
        if (!clickRef.current) return
        const point: [number, number] = [e.offsetX, e.offsetY]
        if (!inst.containPixel('grid', point)) return
        const data = inst.convertFromPixel({ seriesIndex: 0 }, point)
        if (!Array.isArray(data) || data.length < 1) return
        const day = ymdFromAxis(data[0])
        if (day) clickRef.current(day)
      }
      inst.getZr().on('click', zrClick)
      ro = new ResizeObserver(() => inst.resize())
      ro.observe(el)
    }
    create()

    const onVisible = () => {
      if (document.visibilityState === 'visible') instRef.current?.resize()
    }
    const onResize = () => instRef.current?.resize()
    const onTheme = () => {
      const inst = instRef.current
      if (inst && optionRef.current) paint(inst, optionRef.current)
    }
    document.addEventListener('visibilitychange', onVisible)
    window.addEventListener('resize', onResize)
    window.addEventListener('fl-theme', onTheme)

    return () => {
      disposed = true
      cancelAnimationFrame(raf)
      document.removeEventListener('visibilitychange', onVisible)
      window.removeEventListener('resize', onResize)
      window.removeEventListener('fl-theme', onTheme)
      ro?.disconnect()
      if (zrClick) instRef.current?.getZr().off('click', zrClick)
      instRef.current?.dispose()
      instRef.current = null
    }
  }, [])

  useEffect(() => {
    if (!option) return
    const inst = instRef.current
    if (!inst) return // 实例尚未创建（等待容器尺寸），创建时会用 optionRef 应用
    inst.clear()
    paint(inst, option)
  }, [option])

  return ref
}

export function Chart({
  option,
  small,
  clickable,
  onAxisClick,
}: {
  option: echarts.EChartsOption | null
  small?: boolean
  clickable?: boolean
  onAxisClick?: (day: string) => void
}) {
  const ref = useChart(option, onAxisClick)
  const cls = ['chart', small ? 'small' : '', clickable ? 'clickable' : ''].filter(Boolean).join(' ')
  return <div ref={ref} className={cls} />
}
