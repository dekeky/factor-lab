import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode, type RefObject } from 'react'

/**
 * Popover：锚定在触发器下方的浮层。
 *
 * 原生 <select> / <input type="date"> 的弹层由浏览器绘制，位置不受页面控制，
 * 在滚动容器、sticky 祖先、页面缩放下容易错位（"自己飘"）。这里用 fixed 定位
 * 自行计算位置，并在滚动/缩放时关闭，行为可预期。
 */
export function Popover({
  anchorRef,
  open,
  onClose,
  children,
  minWidth = 180,
  maxHeight = 320,
}: {
  anchorRef: RefObject<HTMLElement | null>
  open: boolean
  onClose: () => void
  children: ReactNode
  minWidth?: number
  maxHeight?: number
}) {
  const boxRef = useRef<HTMLDivElement>(null)
  // 初始就必须脱离文档流：否则弹层插入 flex 容器会撑高整行，
  // 锚点按钮被垂直居中推走，随后测算出的位置就是错的（"弹层自己飘"）。
  const [style, setStyle] = useState<CSSProperties>({
    position: 'fixed',
    top: 0,
    left: 0,
    visibility: 'hidden',
  })

  useLayoutEffect(() => {
    if (!open) return
    const el = anchorRef.current
    if (!el) return
    const r = el.getBoundingClientRect()
    const vh = window.innerHeight
    const vw = window.innerWidth
    const spaceBelow = vh - r.bottom - 8
    const spaceAbove = r.top - 8
    const openUp = spaceBelow < 200 && spaceAbove > spaceBelow
    const w = Math.max(r.width, minWidth)
    const left = Math.max(8, Math.min(r.left, vw - w - 8))
    const maxH = Math.max(140, Math.min(maxHeight, openUp ? spaceAbove : spaceBelow))
    const top = openUp ? Math.max(8, r.top - 4 - maxH) : r.bottom + 4
    setStyle({
      position: 'fixed',
      left,
      top,
      minWidth: w,
      maxWidth: Math.min(w + 160, vw - 16),
      maxHeight: maxH,
      visibility: 'visible',
    })
  }, [open, anchorRef, minWidth, maxHeight])

  // 点击外部 / Esc / 滚动 / 尺寸变化 → 关闭（比跟随重算更可靠）
  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      const t = e.target as Node
      if (boxRef.current?.contains(t)) return
      if (anchorRef.current?.contains(t)) return
      onClose()
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('mousedown', onDown, true)
    window.addEventListener('keydown', onKey)
    window.addEventListener('scroll', onClose, true)
    window.addEventListener('resize', onClose)
    return () => {
      window.removeEventListener('mousedown', onDown, true)
      window.removeEventListener('keydown', onKey)
      window.removeEventListener('scroll', onClose, true)
      window.removeEventListener('resize', onClose)
    }
  }, [open, onClose, anchorRef])

  if (!open) return null
  return (
    <div ref={boxRef} className="popover" style={style} role="listbox">
      {children}
    </div>
  )
}
