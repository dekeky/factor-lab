// 诊断：组合构建页图表是否渲染
import { spawn } from 'node:child_process'
import { writeFileSync, mkdirSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const BASE = process.argv[2] || 'http://127.0.0.1:8080'
const PORT = 9336
const CHROME = process.env.CHROME_PATH || 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe'
mkdirSync('shots', { recursive: true })
const chrome = spawn(
  CHROME,
  ['--headless=new', '--disable-gpu', '--no-sandbox', '--no-first-run', `--remote-debugging-port=${PORT}`,
    `--user-data-dir=${join(tmpdir(), 'fl-compose-' + Date.now())}`, '--window-size=1440,950', 'about:blank'],
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
const js = (expression) => send('Runtime.evaluate', { expression, returnByValue: true }).then((r) => r.result?.value)

await new Promise((r) => ws.addEventListener('open', r, { once: true }))
await send('Page.enable')
await send('Runtime.enable')
await send('Emulation.setDeviceMetricsOverride', { width: 1440, height: 950, deviceScaleFactor: 1, mobile: false })
await send('Page.navigate', { url: `${BASE}/#/compose` })
await sleep(2600)

// 选 6 个因子
for (let i = 0; i < 6; i++) {
  await js(`document.querySelectorAll('.card .flex.between.center')[${i}]?.click()`)
  await sleep(120)
}
console.log('已选:', await js(`document.querySelectorAll('.picked-item').length`))

// 运行组合评价
await js(`[...document.querySelectorAll('button')].find(b => b.textContent.includes('运行组合评价'))?.click()`)
await sleep(5000)

console.log('结果卡片数:', await js(`document.querySelectorAll('.card').length`))
console.log('图表容器:', await js(`JSON.stringify([...document.querySelectorAll('.chart')].map((c) => { const b = c.getBoundingClientRect(); return {w: Math.round(b.width), h: Math.round(b.height), canvas: !!c.querySelector('canvas')}; }))`))

const r = await send('Page.captureScreenshot', { format: 'png', captureBeyondViewport: true })
writeFileSync('shots/12-compose-charts.png', Buffer.from(r.data, 'base64'))
console.log('shot saved')
ws.close()
chrome.kill()
