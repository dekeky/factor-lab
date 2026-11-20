// 截图：左侧「我的因子」列表（草稿文件 + 已保存因子同列）
import { spawn } from 'node:child_process'
import { writeFileSync, mkdirSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const API = 'http://127.0.0.1:8080'

const f1 = await fetch(`${API}/api/factors`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ name: 'short_reversal_20', expr: 'alpha = -1 * cs_rank(close / ts_delay(close, 20) - 1)', category: 'user', source: 'user' }),
}).then((r) => r.json()).catch(() => null)
const f2 = await fetch(`${API}/api/factors`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ name: 'low_vol_20', expr: 'alpha = -1 * cs_zscore(ts_std_dev(pct_chg, 20))', category: 'user', source: 'user' }),
}).then((r) => r.json()).catch(() => null)

const PORT = 9365
const chrome = spawn('C:\\Program Files\\Google\\Chrome\\application\\chrome.exe', [
  '--headless=new', '--disable-gpu', '--no-sandbox', '--no-first-run',
  `--remote-debugging-port=${PORT}`, `--user-data-dir=${join(tmpdir(), 'shotfiles' + Date.now())}`,
  '--window-size=1600,1000', 'about:blank',
], { stdio: 'ignore' })

try {
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
  await send('Page.navigate', { url: `${API}/#/factory` })
  await sleep(3500)
  // 再开一个草稿文件，制造「未保存 + 已保存」两段
  await js(`document.querySelector('.fw-lib-add')?.click()`)
  await sleep(400)
  await js(`[...document.querySelectorAll('.fw-lib-item')].find(e => e.textContent.includes('short_reversal_20'))?.click()`)
  await sleep(500)
  mkdirSync('shots', { recursive: true })
  const shot = await send('Page.captureScreenshot', { format: 'png' })
  writeFileSync('shots/21-my-files-list.png', Buffer.from(shot.data, 'base64'))
  console.log('saved shots/21-my-files-list.png')
  ws.close()
} finally {
  chrome.kill()
  for (const f of [f1, f2]) {
    if (f?.id) await fetch(`${API}/api/factors/${f.id}`, { method: 'DELETE' }).catch(() => {})
  }
  console.log('temp factors cleaned')
}
