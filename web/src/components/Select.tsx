import { useRef, useState } from 'react'
import { Popover } from './Popover'

export interface OptItem {
  value: string
  label: string
  hint?: string
}
export interface OptGroup {
  label: string
  items: OptItem[]
}

/** 自定义下拉：替代原生 select，弹层位置精确可控、样式统一。 */
export function Select({
  value,
  onChange,
  groups,
  items,
  placeholder = '请选择',
  minWidth,
  maxHeight,
  disabled,
  size,
}: {
  value: string
  onChange: (v: string) => void
  groups?: OptGroup[]
  items?: OptItem[]
  placeholder?: string
  minWidth?: number
  maxHeight?: number
  disabled?: boolean
  size?: 'sm'
}) {
  const ref = useRef<HTMLButtonElement>(null)
  const [open, setOpen] = useState(false)

  const all: OptItem[] = items ?? (groups ?? []).flatMap((g) => g.items)
  const current = all.find((o) => o.value === value)

  return (
    <>
      <button
        ref={ref}
        type="button"
        className={`sel ${size === 'sm' ? 'sm' : ''}`}
        disabled={disabled}
        onClick={() => setOpen((v) => !v)}
        title={current?.label ?? placeholder}
      >
        <span className={`sel-label ${current ? '' : 'ph'}`}>{current?.label ?? placeholder}</span>
        <span className="sel-caret">▾</span>
      </button>
      <Popover anchorRef={ref} open={open} onClose={() => setOpen(false)} minWidth={minWidth} maxHeight={maxHeight}>
        <div className="pop-list">
          {items &&
            items.map((o) => (
              <button
                key={o.value}
                type="button"
                className={`pop-item ${o.value === value ? 'on' : ''}`}
                onClick={() => {
                  onChange(o.value)
                  setOpen(false)
                }}
              >
                <span className="pop-item-label">{o.label}</span>
                {o.hint && <span className="pop-item-hint">{o.hint}</span>}
              </button>
            ))}
          {groups &&
            groups.map((g) => (
              <div key={g.label}>
                <div className="pop-group">{g.label}</div>
                {g.items.map((o) => (
                  <button
                    key={g.label + o.value}
                    type="button"
                    className={`pop-item ${o.value === value ? 'on' : ''}`}
                    onClick={() => {
                      onChange(o.value)
                      setOpen(false)
                    }}
                  >
                    <span className="pop-item-label">{o.label}</span>
                    {o.hint && <span className="pop-item-hint">{o.hint}</span>}
                  </button>
                ))}
              </div>
            ))}
          {all.length === 0 && <div className="pop-empty">无可选项</div>}
        </div>
      </Popover>
    </>
  )
}
