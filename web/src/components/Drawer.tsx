import { useEffect, type ReactNode } from 'react'

export function Drawer({
  title,
  subtitle,
  onClose,
  actions,
  children,
  width,
}: {
  title: string
  subtitle?: ReactNode
  onClose: () => void
  actions?: ReactNode
  children: ReactNode
  width?: number
}) {
  // 打开抽屉时锁定背景滚动：否则滚到抽屉底部继续滚会带动背后页面，视觉上像弹窗在飘
  useEffect(() => {
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.body.style.overflow = prev
    }
  }, [])

  // Esc 关闭
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    <div className="drawer-mask" onClick={onClose}>
      <div
        className="drawer"
        style={width ? { width: `min(${width}px, 96vw)` } : undefined}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="drawer-head">
          <div style={{ minWidth: 0 }}>
            <h3>{title}</h3>
            {subtitle && <div className="sub">{subtitle}</div>}
          </div>
          <div style={{ marginLeft: 'auto', display: 'flex', gap: 8, alignItems: 'center', flexShrink: 0 }}>
            {actions}
            <button className="icon-btn" onClick={onClose} title="关闭（Esc）">
              ✕
            </button>
          </div>
        </div>
        <div className="drawer-body">{children}</div>
      </div>
    </div>
  )
}
