import { useMemo, useRef, useState } from 'react'
import { Popover } from './Popover'

const pad = (n: number) => String(n).padStart(2, '0')
const fmt = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
function parse(s: string): Date | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(s.trim())
  if (!m) return null
  const d = new Date(+m[1], +m[2] - 1, +m[3])
  return Number.isNaN(d.getTime()) ? null : d
}
const DOW = ['一', '二', '三', '四', '五', '六', '日']

/** 自定义日期选择：替代原生 date 输入，弹层位置可控。 */
export function DatePicker({
  value,
  onChange,
  placeholder = '不限',
  clearable = true,
  minWidth = 150,
}: {
  value: string
  onChange: (v: string) => void
  placeholder?: string
  clearable?: boolean
  minWidth?: number
}) {
  const ref = useRef<HTMLButtonElement>(null)
  const [open, setOpen] = useState(false)
  const sel = parse(value)
  const [view, setView] = useState(() => sel ?? new Date())

  const days = useMemo(() => {
    const y = view.getFullYear()
    const m = view.getMonth()
    const first = new Date(y, m, 1)
    const offset = (first.getDay() + 6) % 7 // 周一为第一列
    const start = new Date(y, m, 1 - offset)
    return Array.from({ length: 42 }, (_, i) => {
      const d = new Date(start.getFullYear(), start.getMonth(), start.getDate() + i)
      return { d, label: d.getDate(), inMonth: d.getMonth() === m }
    })
  }, [view])

  const today = fmt(new Date())

  function pick(v: string) {
    onChange(v)
    setOpen(false)
  }

  return (
    <>
      <button
        ref={ref}
        type="button"
        className="sel"
        onClick={() => {
          setView(parse(value) ?? new Date())
          setOpen((v) => !v)
        }}
        title={value || placeholder}
      >
        <span className={`sel-label ${value ? '' : 'ph'}`}>{value || placeholder}</span>
        <span className="sel-caret">▦</span>
      </button>
      <Popover anchorRef={ref} open={open} onClose={() => setOpen(false)} minWidth={Math.max(248, minWidth)} maxHeight={420}>
        <div className="cal">
          <div className="cal-head">
            <button type="button" className="cal-nav" onClick={() => setView(new Date(view.getFullYear(), view.getMonth() - 1, 1))}>
              ‹
            </button>
            <div className="cal-title">
              {view.getFullYear()} 年 {view.getMonth() + 1} 月
            </div>
            <button type="button" className="cal-nav" onClick={() => setView(new Date(view.getFullYear(), view.getMonth() + 1, 1))}>
              ›
            </button>
          </div>
          <div className="cal-grid cal-dow">
            {DOW.map((d) => (
              <div key={d} className="cal-dow-cell">
                {d}
              </div>
            ))}
          </div>
          <div className="cal-grid">
            {days.map(({ d, label, inMonth }) => {
              const v = fmt(d)
              const cls = [
                'cal-day',
                inMonth ? '' : 'other',
                v === value ? 'on' : '',
                v === today ? 'today' : '',
              ]
                .filter(Boolean)
                .join(' ')
              return (
                <button key={v} type="button" className={cls} onClick={() => pick(v)}>
                  {label}
                </button>
              )
            })}
          </div>
          <div className="cal-foot">
            <button type="button" className="btn xs" onClick={() => pick(today)}>
              今天
            </button>
            {clearable && (
              <button type="button" className="btn xs ghost" onClick={() => pick('')}>
                清空（不限）
              </button>
            )}
            <span className="subtle" style={{ marginLeft: 'auto' }}>
              {value || '未选'}
            </span>
          </div>
        </div>
      </Popover>
    </>
  )
}
