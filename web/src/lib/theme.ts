import { useEffect, useState } from 'react'

export type Theme = 'dark' | 'light'

const KEY = 'fl.theme'

export function readTheme(): Theme {
  try {
    const v = localStorage.getItem(KEY)
    if (v === 'light' || v === 'dark') return v
  } catch {
    /* ignore */
  }
  return 'dark'
}

export function applyTheme(theme: Theme) {
  const root = document.documentElement
  root.dataset.theme = theme
  root.style.colorScheme = theme
  try {
    localStorage.setItem(KEY, theme)
  } catch {
    /* ignore */
  }
  window.dispatchEvent(new Event('fl-theme'))
}

export function cssVar(name: string, fallback = '') {
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return v || fallback
}

/** hex / rgb → rgba，给图表和热力格子用。解析失败则原样返回。 */
export function colorAlpha(color: string, a: number) {
  const raw = color.trim()
  const hex = raw.startsWith('#') ? raw.slice(1) : ''
  const full = hex.length === 3 ? hex.split('').map((c) => c + c).join('') : hex
  if (full.length >= 6) {
    const r = parseInt(full.slice(0, 2), 16)
    const g = parseInt(full.slice(2, 4), 16)
    const b = parseInt(full.slice(4, 6), 16)
    if ([r, g, b].every((n) => Number.isFinite(n))) return `rgba(${r},${g},${b},${a})`
  }
  const rgb = raw.match(/^rgba?\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)/i)
  if (rgb) return `rgba(${rgb[1]},${rgb[2]},${rgb[3]},${a})`
  return raw
}

export function useTheme(): Theme {
  const [theme, setTheme] = useState<Theme>(readTheme)
  useEffect(() => {
    const on = () => setTheme(readTheme())
    window.addEventListener('fl-theme', on)
    return () => window.removeEventListener('fl-theme', on)
  }, [])
  return theme
}
