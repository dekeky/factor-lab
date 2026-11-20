// 截图：运行评价的参数弹窗（含股票池）
import { spawn } from 'node:child_process'
import { writeFileSync, mkdirSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const PORT = 9363
const chrome = spawn('C:\\Program Files\\Google\\Chrome\\application\\chrome.exe', [
  '--headless=new', '--disable-gpu', '--no-sandbox', '--no-first-run',
  `--remote-debugging-port=${PORT}`, `--user-data-dir=${join(tmpdir(), 'shotrun' + Date.now())}`,
  '--window-size=1600,1000', 'about:blank',
], { stdio: 'ignore' })

for (let i = 0; i < 60; i++) {
  try { if ((await fetch(`http://127.0.0.1:${PORT}/json/version`)).ok) break } catch { /* 等待启动 */ }
  await sleep(250)
}
const list = await (await fetch(`http://127.0.0.1:${PORT}/json/list`)).json()
const ws = new WebSocket(list.find((t) => t.type === 'page').webSocketDebuggerUrl)
let seq = 0
const pend = new Map()
ws.addEventListener('message', (e) => {
  const m = JSON.parse(e.data)
  if (m.id && pend.has(m.id)) { pend.get(m.id)(m.result); pend.delete(m.id) }
})
const send = (method, params = {}) => new Promise((r) => { const i = ++seq; pend.set(i, r); ws.send(JSON.stringify({ id: i, method, params })) })
const js = (expression) => send('Runtime.evaluate', { expression, returnByValue: true }).then((r) => r.result?.value)
await new Promise((r) => ws.addEventListener('open', r, { once: true }))
await send('Emulation.setDeviceMetricsOverride', { width: 1600, height: 1000, deviceScaleFactor: 1, mobile: false })
await send('Page.navigate', { url: 'http://127.0.0.1:8080/#/factory' })
await sleep(3500)
await js(`[...document.querySelectorAll('.fw-top .btn')].find(b => b.textContent.includes('运行评价'))?.click()`)
await sleep(600)
mkdirSync('shots', { recursive: true })
const shot = await send('Page.captureScreenshot', { format: 'png' })
writeFileSync('shots/20-run-params-modal.png', Buffer.from(shot.data, 'base64'))
console.log('saved shots/20-run-params-modal.png')
ws.close()
chrome.kill()
