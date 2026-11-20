import { useEffect, type ReactNode } from 'react'

/**
 * Modal：居中模态弹窗（评价结果 / 评价记录）。
 *
 * 与 Drawer 相同的纪律：打开即锁背景滚动、Esc 关闭；层级 60，
 * 内部的自定义 Select/DatePicker 弹层（z-index 200）仍能盖在其上。
 */
export function Modal({
  title,
  subtitle,
  onClose,
  footer,
  children,
  width = 1080,
  closeOnEscape = true,
}: {
  title: ReactNode
  subtitle?: ReactNode
  onClose: () => void
  footer?: ReactNode
  children: ReactNode
  width?: number
  closeOnEscape?: boolean
}) {
  useEffect(() => {
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.body.style.overflow = prev
    }
  }, [])

  useEffect(() => {
    if (!closeOnEscape) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose, closeOnEscape])

  return (
    <div className="modal-mask" onClick={onClose}>
      <div className="modal" style={{ width: `min(${width}px, 94vw)` }} onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <div style={{ minWidth: 0 }}>
            <h3>{title}</h3>
            {subtitle && <div className="modal-sub">{subtitle}</div>}
          </div>
          <div style={{ marginLeft: 'auto', display: 'flex', gap: 8, alignItems: 'center', flexShrink: 0 }}>
            <button className="icon-btn" onClick={onClose} title="关闭（Esc）">✕</button>
          </div>
        </div>
        <div className="modal-body">{children}</div>
        {footer && <div className="modal-foot">{footer}</div>}
      </div>
    </div>
  )
}
