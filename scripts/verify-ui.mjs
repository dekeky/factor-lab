// 交互功能验证：下拉选中后回填表达式、日历选择后回填日期
import { spawn } from 'node:child_process'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const BASE = process.argv[2] || 'http://127.0.0.1:8080'
const PORT = 9335
const CHROME = process.env.CHROME_PATH || 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe'
const chrome = spawn(
  CHROME,
  ['--headless=new', '--disable-gpu', '--no-sandbox', '--no-first-run', `--remote-debugging-port=${PORT}`,
    `--user-data-dir=${join(tmpdir(), 'fl-verify-' + Date.now())}`, '--window-size=1440,950', 'about:blank'],
  { stdio: 'ignore' },
)
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
for (let i = 0; i < 60; i++) {
  try { if ((await fetch(`http://127.0.0.1:${PORT}/json/version`)).ok) break } catch {}
  await sleep(250)
}
const list = await (await fetch(`http://127.0.0.1:${PORT}/json/list`)).json()
const ws = new WebSocket(list.find((t) => t.type === 'page').webSocketDebuggerUrl)
let seq = 0
const pending = new Map()
ws.addEventListener('message', (ev) => {
  const m = JSON.parse(ev.data)
  if (m.id && pending.has(m.id)) { pending.get(m.id)(m.result ?? m.error); pending.delete(m.id) }
})
const send = (method, params = {}) =>
  new Promise((res) => { const i = ++seq; pending.set(i, res); ws.send(JSON.stringify({ id: i, method, params })) })
const evalJS = (expression) => send('Runtime.evaluate', { expression, returnByValue: true }).then((r) => r.result?.value)

await new Promise((r) => ws.addEventListener('open', r, { once: true }))
await send('Page.enable')
await send('Runtime.enable')
await send('Emulation.setDeviceMetricsOverride', { width: 1440, height: 950, deviceScaleFactor: 1, mobile: false })
await send('Page.navigate', { url: `${BASE}/#/factory` })
await sleep(2600)

console.log('初始表达式:', await evalJS(`document.querySelector('textarea.editor').value.split('\\n').slice(-1)[0]`))

// 打开下拉并选第 4 项（alpha101_004）
await evalJS(`document.querySelector('.card .sel').click()`)
await sleep(400)
console.log('弹层项数:', await evalJS(`document.querySelectorAll('.popover .pop-item').length`))
console.log('第4项:', await evalJS(`document.querySelectorAll('.popover .pop-item')[3].textContent`))
await evalJS(`document.querySelectorAll('.popover .pop-item')[3].click()`)
await sleep(400)
console.log('选中后表达式:', await evalJS(`document.querySelector('textarea.editor').value.split('\\n').slice(-1)[0]`))
console.log('弹层是否关闭:', (await evalJS(`document.querySelector('.popover') === null`)) ? '是' : '否')
console.log('按钮显示:', await evalJS(`document.querySelector('.card .sel').textContent`).then((t) => t.replace('▾', '')))

// 日期选择：点开起始日期，选 10 号
await evalJS(`[...document.querySelectorAll('.sel')].find(x => x.textContent.includes('2023'))?.click()`)
await sleep(400)
console.log('日历是否打开:', (await evalJS(`!!document.querySelector('.popover .cal')`)) ? '是' : '否')
await evalJS(`[...document.querySelectorAll('.cal-day')].find(d => d.textContent.trim()==='10' && !d.className.includes('other'))?.click()`)
await sleep(400)
console.log('选择后起始日期:', await evalJS(`[...document.querySelectorAll('.sel')].find(x => x.textContent.includes('2023'))?.textContent`).then((t) => t?.replace('▦', '')))

// 评价一次，确认区间参数真的传到了后端
await evalJS(`[...document.querySelectorAll('button')].find(b => b.textContent.includes('评价因子'))?.click()`)
await sleep(4000)
console.log('评价结果 n_dates:', await evalJS(`document.querySelector('.kpi .num')?.textContent`))

ws.close()
chrome.kill()
