// 验证：因子组合 IDE 布局（左列表 / 中组合编辑 / 右 AI 助手）
import { spawn } from 'node:child_process'
import { writeFileSync, mkdirSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const API = 'http://127.0.0.1:8080'
let ok = 0, fail = 0
const assert = (name, cond) => { if (cond) { ok++; console.log('PASS', name) } else { fail++; console.log('FAIL', name) } }
const setInput = (sel, value) => `(() => { const i = document.querySelector('${sel}'); if (!i) return false;
  const set = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set;
  set.call(i, ${JSON.stringify(value)}); i.dispatchEvent(new Event('input', { bubbles: true })); return true })()`

const PORT = 9369
const chrome = spawn('C:\\Program Files\\Google\\Chrome\\application\\chrome.exe', [
  '--headless=new', '--disable-gpu', '--no-sandbox', '--no-first-run',
  `--remote-debugging-port=${PORT}`, `--user-data-dir=${join(tmpdir(), 'shotccide' + Date.now())}`,
  '--window-size=1600,1000', 'about:blank',
], { stdio: 'ignore' })

let createdId = null
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
  await send('Page.navigate', { url: `${API}/#/compose` })
  await sleep(3500)

  // 1) IDE 三栏骨架
  assert('左侧「我的组合」列表', await js(`!![...document.querySelectorAll('.fw-lib-head')].find(h => h.textContent.includes('我的组合'))`))
  assert('中间组合编辑器（名称/已选/候选）', await js(`(() => { const e = document.querySelector('.cc-body'); return e && e.textContent.includes('组合名称') && e.textContent.includes('已选因子') && e.textContent.includes('候选因子') })()`))
  assert('右侧 AI 助手面板', await js(`!!document.querySelector('.fw-chat .ac')`))
  assert('AI 助手含组合建议', await js(`document.querySelector('.fw-chat .ac').textContent.includes('互补')`))
  assert('顶栏含保存与评价按钮', await js(`(() => { const t = document.querySelector('.fw-top'); return t && t.textContent.includes('保存组合') && t.textContent.includes('运行评价') })()`))
  assert('页面无卡片网格残留', await js(`document.querySelectorAll('.combo-grid, .combo-card').length === 0`))

  mkdirSync('shots', { recursive: true })
  let shot = await send('Page.captureScreenshot', { format: 'png' })
  writeFileSync('shots/29-combo-ide.png', Buffer.from(shot.data, 'base64'))

  // 2) 从候选加入两个因子 + 改名 + 保存
  await js(`document.querySelectorAll('.cc-cand')[0].click()`)
  await sleep(250)
  await js(`document.querySelectorAll('.cc-cand')[0].click()`)
  await sleep(250)
  assert('候选可点击加入', await js(`document.querySelectorAll('.picked-item').length === 2`))
  assert('填组合名称', await js(setInput('.cc-head input[type=text]', 'ide_combo_test')))
  await js(`[...document.querySelectorAll('.fw-top .btn')].find(b => b.textContent.includes('保存组合')).click()`)
  await sleep(800)
  assert('保存成功提示', await js(`(() => { const n = document.querySelector('.note.ok'); return n && n.textContent.includes('ide_combo_test') })()`))
  assert('已保存标记出现', await js(`document.querySelector('.cc-head').textContent.includes('已保存')`))

  const combos = await fetch(`${API}/api/combos`).then((r) => r.json())
  const created = combos.find((c) => c.name === 'ide_combo_test')
  createdId = created?.id ?? null
  assert('后端已创建组合', createdId != null)

  shot = await send('Page.captureScreenshot', { format: 'png' })
  writeFileSync('shots/30-combo-saved.png', Buffer.from(shot.data, 'base64'))

  // 3) 运行评价 → 参数弹窗 → 结果弹窗
  await js(`[...document.querySelectorAll('.fw-top .btn.primary')].find(b => b.textContent.includes('运行评价')).click()`)
  await sleep(500)
  assert('参数弹窗（股票池 + 参数）', await js(`(() => { const m = document.querySelector('.modal'); return m && m.textContent.includes('运行评价') && m.textContent.includes('股票池') && m.textContent.includes('持有期') })()`))
  await js(`[...document.querySelectorAll('.modal .btn.primary')].find(b => b.textContent.includes('开始评价')).click()`)
  await sleep(200)
  let done = false
  for (let i = 0; i < 120; i++) {
    await sleep(2000)
    const state = await js(`(() => { const m = document.querySelector('.modal'); return m ? m.textContent.slice(0, 400) : '' })()`)
    if (state.includes('评价结果')) { done = true; break }
    if (!state) break
  }
  assert('评价完成弹结果弹窗', done)
  assert('结果含工厂式 KPI 与图表', await js(`(() => { const m = document.querySelector('.modal'); return m && m.textContent.includes('RankIC 均值') && m.textContent.includes('分层回测') })()`))
  assert('结果含相关性表', await js(`(() => { const m = document.querySelector('.modal'); return m && m.querySelector('.corr-cell') !== null })()`))

  shot = await send('Page.captureScreenshot', { format: 'png' })
  writeFileSync('shots/31-combo-ide-result.png', Buffer.from(shot.data, 'base64'))
  await js(`[...document.querySelectorAll('.modal [class*=close], .modal-close')].click?.()`)
  await js(`(() => { const esc = new KeyboardEvent('mousedown', { bubbles: true }); const m = document.querySelector('.modal'); const btn = m && [...m.querySelectorAll('button')].find(b => b.className.includes('close')); if (btn) { btn.click(); return true } return false })()`)
  await sleep(500)

  // 4) 左列表：已保存组合行（草稿入库后移入已保存段并高亮）
  assert('列表高亮当前组合', await js(`(() => { const r = [...document.querySelectorAll('.fw-lib-item')].find(e => e.textContent.includes('ide_combo_test')); return r && r.classList.contains('on') })()`))
  assert('列表含已保存组合行', await js(`!![...document.querySelectorAll('.fw-lib-item')].find(e => e.textContent.includes('ide_combo_test'))`))
  assert('保存后评价写回摘要（列表 IC）', await js(`(() => { const r = [...document.querySelectorAll('.fw-lib-item')].find(e => e.textContent.includes('ide_combo_test')); return r && r.textContent.includes('—') === false && /-?\\d\\.\\d{3}/.test(r.textContent) })()`))

  shot = await send('Page.captureScreenshot', { format: 'png' })
  writeFileSync('shots/32-combo-ide-list.png', Buffer.from(shot.data, 'base64'))

  // 5) 删除（两段确认）
  await js(`[...document.querySelectorAll('.fw-top .btn')].find(b => b.textContent.trim() === '删除').click()`)
  await sleep(300)
  assert('删除出现确认', await js(`document.querySelector('.fw-top').textContent.includes('确认删除')`))
  await js(`[...document.querySelectorAll('.fw-top .btn')].find(b => b.textContent.includes('确认删除')).click()`)
  await sleep(700)
  const after = await fetch(`${API}/api/combos`).then((r) => r.json())
  assert('组合已删除', !after.find((c) => c.id === createdId))
  createdId = null
  ws.close()
} finally {
  chrome.kill()
  if (createdId != null) await fetch(`${API}/api/combos/${createdId}`, { method: 'DELETE' }).catch(() => {})
  console.log('cleaned')
  console.log(`\n${ok} passed, ${fail} failed`)
  process.exit(fail ? 1 : 0)
}
