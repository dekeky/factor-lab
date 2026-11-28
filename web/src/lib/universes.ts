export interface Universe {
  code: string
  name: string
  group: string
}

/** 与后端 internal/data/universe.go 对齐；meta 未返回时作下拉兜底。 */
export const FALLBACK_UNIVERSES: Universe[] = [
  { code: 'ALL', name: '全市场', group: '沪深核心' },
  { code: '000300.SH', name: '沪深300', group: '沪深核心' },
  { code: '000905.SH', name: '中证500', group: '沪深核心' },
  { code: '000906.SH', name: '中证800', group: '沪深核心' },
  { code: '000852.SH', name: '中证1000', group: '沪深核心' },
  { code: '000510.SH', name: '中证A500', group: '沪深核心' },
  { code: '000985.SH', name: '中证全指', group: '沪深核心' },
  { code: '000016.SH', name: '上证50', group: '上证 / 科创' },
  { code: '000010.SH', name: '上证180', group: '上证 / 科创' },
  { code: '000688.SH', name: '科创50', group: '上证 / 科创' },
  { code: '000698.SH', name: '科创100', group: '上证 / 科创' },
  { code: '399001.SZ', name: '深证成指', group: '深证 / 创业板' },
  { code: '399330.SZ', name: '深证100', group: '深证 / 创业板' },
  { code: '399006.SZ', name: '创业板指', group: '深证 / 创业板' },
  { code: '399673.SZ', name: '创业板50', group: '深证 / 创业板' },
]

export function canonicalIndex(code?: string) {
  const v = (code ?? '').trim().toUpperCase()
  if (v === '399300.SZ') return '000300.SH'
  return v
}

export function samePool(a?: string, b?: string) {
  const x = canonicalIndex(a)
  const y = canonicalIndex(b)
  return !!x && x === y
}

export function universeName(code?: string, list?: Universe[]) {
  const v = canonicalIndex(code)
  if (!v) return ''
  const hit = (list?.length ? list : FALLBACK_UNIVERSES).find((u) => u.code === v)
  return hit?.name ?? v
}

export function universeList(list?: Universe[]) {
  return list?.length ? list : FALLBACK_UNIVERSES
}
