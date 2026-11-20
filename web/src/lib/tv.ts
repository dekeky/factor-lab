/** 把 Tushare 代码转成 TradingView 符号：600426.SH → SSE:600426 */
export function tvSymbol(code: string): string {
  const c = code.trim().toUpperCase()
  const [num, mkt] = c.split('.')
  const id = num || c
  if (mkt === 'SH' || mkt === 'SS') return `SSE:${id}`
  if (mkt === 'SZ') return `SZSE:${id}`
  if (mkt === 'BJ') return `BSE:${id}`
  if (/^6\d{5}$/.test(id) || /^9\d{5}$/.test(id)) return `SSE:${id}`
  if (/^[03]\d{5}$/.test(id)) return `SZSE:${id}`
  if (/^[48]\d{5}$/.test(id)) return `BSE:${id}`
  return id.includes(':') ? c : `SSE:${id}`
}

export function tvEmbedSrc(symbol: string): string {
  const q = new URLSearchParams({
    symbol,
    interval: 'D',
    timezone: 'Asia/Shanghai',
    theme: 'light',
    style: '1',
    locale: 'zh_CN',
    withdateranges: '1',
    hidesidetoolbar: '0',
    hidetoptoolbar: '0',
    symboledit: '1',
    saveimage: '1',
    hideideas: '1',
    toolbarbg: 'f1f3f6',
  })
  return `https://s.tradingview.com/widgetembed/?${q.toString()}`
}

export function tvPageUrl(symbol: string): string {
  return `https://cn.tradingview.com/chart/?symbol=${encodeURIComponent(symbol)}`
}
