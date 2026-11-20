import type { Metrics } from '../types'

/**
 * 评价记录：保存在浏览器 localStorage（研究过程日志，按设备保留）。
 *
 * 完整 Metrics 含全部时序（约数百 KB/条），为不撑爆配额：
 *  - 只保留最近 MAX 条；
 *  - 写入超配额时从最旧开始丢弃重试，全部失败则返回空列表（不静默丢当前）。
 */
export interface EvalRecord {
  id: string
  ts: number
  fileName: string
  expr: string
  params: { horizon: number; groups: number; cost: number; start: string; end: string; universe?: string }
  metrics: Metrics
  durationMs: number
}

const KEY = 'fw.evalHistory'
const MAX = 6

export function loadHistory(): EvalRecord[] {
  try {
    const arr = JSON.parse(localStorage.getItem(KEY) ?? '[]')
    return Array.isArray(arr) ? (arr as EvalRecord[]) : []
  } catch {
    return []
  }
}

export function saveRecord(rec: EvalRecord): EvalRecord[] {
  const list = [rec, ...loadHistory()].slice(0, MAX)
  for (let n = list.length; n >= 1; n--) {
    try {
      localStorage.setItem(KEY, JSON.stringify(list.slice(0, n)))
      return list.slice(0, n)
    } catch {
      /* 配额不足，继续丢最旧 */
    }
  }
  return []
}

export function removeRecord(id: string): EvalRecord[] {
  const list = loadHistory().filter((r) => r.id !== id)
  try {
    localStorage.setItem(KEY, JSON.stringify(list))
  } catch {
    /* 忽略 */
  }
  return list
}

export function clearHistory(): EvalRecord[] {
  try {
    localStorage.removeItem(KEY)
  } catch {
    /* 忽略 */
  }
  return []
}

export function fmtTime(ts: number) {
  const d = new Date(ts)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

export function exprBrief(expr: string, max = 64) {
  const first = expr.split('\n').find((l) => l.trim() && !l.trim().startsWith('#')) ?? ''
  return first.length > max ? first.slice(0, max) + '…' : first
}
