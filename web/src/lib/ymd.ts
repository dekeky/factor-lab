/** A 股日期按上海日历日处理。 */

const shanghaiYmd = new Intl.DateTimeFormat('en-CA', {
  timeZone: 'Asia/Shanghai',
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
})

export function ymdShanghai(v: Date | number): string {
  return shanghaiYmd.format(v instanceof Date ? v : new Date(v))
}

// ECharts time 轴把 'YYYY-MM-DD' 当成本地零点（东八区即当天 00:00）。
// 若再用 getUTC* 取日历日，会退一天，曲线最后一点会被标成前一交易日。
export function ymdFromAxis(x: unknown): string {
  if (typeof x === 'string') {
    const m = /^(\d{4}-\d{2}-\d{2})/.exec(x)
    return m ? m[1] : x.slice(0, 10)
  }
  if (Array.isArray(x) && x.length) return ymdFromAxis(x[0])
  if (typeof x === 'number' && Number.isFinite(x)) {
    const d = new Date(x)
    if (!Number.isNaN(d.getTime())) return ymdShanghai(d)
  }
  return ''
}
