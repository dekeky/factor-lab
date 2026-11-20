// 截图：因子工厂的 AI 助手对话流程
import { spawn } from 'node:child_process'
import { writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const BASE = process.argv[2] || 'http://127.0.0.1:8080'
const PORT = 9337
const CHROME = process.env.CHROME_PATH || 'C:\\Program Files\\Google\\Chrome\\application\\chrome.exe'
const chrome = spawn(
  CHROME,
  ['--headless=new', '--disable-gpu', '--no-sandbox', '--no-first-run', `--remote-debugging-port=${PORT}`,
    `--user-data-dir=${join(tmpdir(), 'fl-assist-' + Date.now())}`, '--window-size=1440,950', 'about:blank'],
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
await send('Page.navigate', { url: `${BASE}/#/factory` })
await sleep(2800)

// 点击建议按钮「写一个短期反转因子」
console.log('点击建议:', await js(`[...document.querySelectorAll('.assist-empty .btn')][0]?.click(); 'ok'`))
await sleep(2500)
console.log('消息数:', await js(`document.querySelectorAll('.assist-msgs .msg').length`))
console.log('表达式卡片:', await js(`document.querySelectorAll('.expr-card').length`))
console.log('校验标签:', await js(`document.querySelector('.expr-card .tag')?.textContent`))

const r = await send('Page.captureScreenshot', { format: 'png' })
writeFileSync('shots/13-assist.png', Buffer.from(r.data, 'base64'))
console.log('shot saved')

// 再点「应用到编辑器」验证联动
await js(`document.querySelector('.expr-card .btn.primary')?.click()`)
await sleep(600)
console.log('编辑器值已更新:', await js(`document.querySelector('textarea.editor')?.value?.includes('mom20') ?? false`))
const r2 = await send('Page.captureScreenshot', { format: 'png' })
writeFileSync('shots/14-assist-applied.png', Buffer.from(r2.data, 'base64'))
console.log('shot2 saved')
ws.close()
chrome.kill()
