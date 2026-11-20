import { spawn } from 'node:child_process'
import { writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
const PORT = 9338
const chrome = spawn(process.env.CHROME_PATH || 'C://Program Files\\Google\\Chrome\\Application\\chrome.exe',
  ['--headless=new', '--disable-gpu', '--no-sandbox', '--no-first-run', `--remote-debugging-port=${PORT}`,
   `--user-data-dir=${join(tmpdir(), 'fl-res-' + Date.now())}`, '--window-size=1440,2400', 'about:blank'], { stdio: 'ignore' })
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
for (let i = 0; i < 60; i++) { try { if ((await fetch(`http://127.0.0.1:${PORT}/json/version`)).ok) break } catch {} await sleep(250) }
const list = await (await fetch(`http://127.0.0.1:${PORT}/json/list`)).json()
const ws = new WebSocket(list.find((t) => t.type === 'page').webSocketDebuggerUrl)
let seq = 0
const pending = new Map()
ws.addEventListener('message', (ev) => { const m = JSON.parse(ev.data); if (m.id && pending.has(m.id)) { pending.get(m.id)(m.result ?? m.error); pending.delete(m.id) } })
const send = (m, p = {}) => new Promise((res) => { const i = ++seq; pending.set(i, res); ws.send(JSON.stringify({ id: i, method: m, params: p })) })
const js = (e) => send('Runtime.evaluate', { expression: e, returnByValue: true }).then((r) => r.result?.value)
await new Promise((r) => ws.addEventListener('open', r, { once: true }))
await send('Page.enable'); await send('Runtime.enable')
await send('Emulation.setDeviceMetricsOverride', { width: 1440, height: 2400, deviceScaleFactor: 1, mobile: false })
await send('Page.navigate', { url: 'http://127.0.0.1:8080/#/factory' })
await sleep(2600)
console.log('点击评价:', await js(`[...document.querySelectorAll('button')].find(b => b.textContent.includes('评价因子'))?.click(); 'ok'`))
await sleep(4000)
console.log('KPI 数:', await js(`document.querySelectorAll('.kpi').length`))
console.log('Tabs:', await js(`[...document.querySelectorAll('.tabs-bar button')].map(b => b.textContent)`))
const r = await send('Page.captureScreenshot', { format: 'png' })
writeFileSync('shots/15-result-tabs.png', Buffer.from(r.data, 'base64'))
console.log('shot saved')
ws.close(); chrome.kill()
