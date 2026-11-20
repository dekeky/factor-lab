// 验证：双击「我的因子」列表项名称可内联重命名（回车确认 / Esc 取消 / 重名报错）
import { spawn } from 'node:child_process'
import { writeFileSync, mkdirSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const API = 'http://127.0.0.1:8080'
let ok = 0, fail = 0
const assert = (name, cond) => { if (cond) { ok++; console.log('PASS', name) } else { fail++; console.log('FAIL', name) } }

// 造两个临时因子：一个被重命名，一个用于重名冲突
const f1 = await fetch(`${API}/api/factors`, {
  method: 'POST', headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ name: 'rename_ui_tmp', expr: 'alpha = -1 * cs_rank(close / ts_delay(close, 10) - 1)', source: 'user' }),
}).then((r) => r.json()).catch(() => null)
const f2 = await fetch(`${API}/api/factors`, {
  method: 'POST', headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ name: 'rename_keep_tmp', expr: 'alpha = -1 * cs_rank(ts_mean(close, 5))', source: 'user' }),
}).then((r) => r.json()).catch(() => null)

const PORT = 9366
const chrome = spawn('C:\\Program Files\\Google\\Chrome\\application\\chrome.exe', [
  '--headless=new', '--disable-gpu', '--no-sandbox', '--no-first-run',
  `--remote-debugging-port=${PORT}`, `--user-data-dir=${join(tmpdir(), 'shotrename' + Date.now())}`,
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

  // 1) 双击已保存因子名 → 出现内联输入框
  const itemSel = `[...document.querySelectorAll('.fw-lib-item')].find(e => e.textContent.includes('rename_ui_tmp'))`
  await js(`${itemSel}.querySelector('.fw-lib-name').dispatchEvent(new MouseEvent('dblclick', { bubbles: true }))`)
  await sleep(300)
  assert('双击出现内联输入框', await js(`!!document.querySelector('.fw-lib-rename')`))

  // 2) Esc 取消 → 输入框消失，名称不变
  await js(`(() => { const i = document.querySelector('.fw-lib-rename'); i.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })) })()`)
  await sleep(200)
  assert('Esc 取消重命名', await js(`!document.querySelector('.fw-lib-rename') && !!${itemSel}`))

  // 3) 再双击 → 改名 → 回车提交
  await js(`${itemSel}.querySelector('.fw-lib-name').dispatchEvent(new MouseEvent('dblclick', { bubbles: true }))`)
  await sleep(300)
  await js(`(() => { const i = document.querySelector('.fw-lib-rename');
    const set = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set;
    set.call(i, 'rename_ui_done'); i.dispatchEvent(new Event('input', { bubbles: true }));
    i.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })) })()`)
  await sleep(900)
  assert('回车后列表显示新名称', await js(`!![...document.querySelectorAll('.fw-lib-item')].find(e => e.textContent.includes('rename_ui_done'))`))
  const libNow = await fetch(`${API}/api/factors`).then((r) => r.json())
  const renamed = libNow.find((f) => f.id === f1.id)
  assert('后端名称已更新', renamed && renamed.name === 'rename_ui_done')

  // 4) 改回原名时与另一因子重名 → 报冲突错误
  await js(`[...document.querySelectorAll('.fw-lib-item')].find(e => e.textContent.includes('rename_ui_done')).querySelector('.fw-lib-name').dispatchEvent(new MouseEvent('dblclick', { bubbles: true }))`)
  await sleep(300)
  await js(`(() => { const i = document.querySelector('.fw-lib-rename');
    const set = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set;
    set.call(i, 'rename_keep_tmp'); i.dispatchEvent(new Event('input', { bubbles: true }));
    i.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })) })()`)
  await sleep(900)
  assert('重名冲突显示错误提示', await js(`!!document.querySelector('.note.err') && document.querySelector('.note.err').textContent.includes('同名因子已存在')`))
  const libAfter = await fetch(`${API}/api/factors`).then((r) => r.json())
  assert('冲突时后端名称未被改动', libAfter.find((f) => f.id === f1.id)?.name === 'rename_ui_done')

  // 5) 双击草稿文件名 → 本地重命名（不调接口）
  await js(`document.querySelector('.fw-lib-add')?.click()`)
  await sleep(400)
  const draftSel = `[...document.querySelectorAll('.fw-lib-item')].find(e => e.textContent.includes('未命名'))`
  await js(`${draftSel}.querySelector('.fw-lib-name').dispatchEvent(new MouseEvent('dblclick', { bubbles: true }))`)
  await sleep(300)
  assert('草稿也可双击重命名', await js(`!!document.querySelector('.fw-lib-rename')`))
  await js(`(() => { const i = document.querySelector('.fw-lib-rename');
    const set = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set;
    set.call(i, 'my_draft'); i.dispatchEvent(new Event('input', { bubbles: true }));
    i.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })) })()`)
  await sleep(400)
  assert('草稿改名生效', await js(`!![...document.querySelectorAll('.fw-lib-item')].find(e => e.textContent.includes('my_draft.dsl'))`))

  mkdirSync('shots', { recursive: true })
  const shot = await send('Page.captureScreenshot', { format: 'png' })
  writeFileSync('shots/23-rename-inline.png', Buffer.from(shot.data, 'base64'))
  console.log(`saved shots/23-rename-inline.png`)
  ws.close()
} finally {
  chrome.kill()
  for (const f of [f1, f2]) {
    if (f?.id) await fetch(`${API}/api/factors/${f.id}`, { method: 'DELETE' }).catch(() => {})
  }
  console.log('temp factors cleaned')
  console.log(`\n${ok} passed, ${fail} failed`)
  process.exit(fail ? 1 : 0)
}
