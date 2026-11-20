// 模型设置（左侧「设置」）UI 验证：入口 → 弹窗 → 预设填充 → 测试连接 → 保存热生效 → 持久化 → 恢复未配置
import { spawn } from 'node:child_process'
import { writeFileSync, mkdirSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const API = 'http://127.0.0.1:8080'
const PORT = 9377
let ok = 0
let fail = 0
const assert = (name, cond, extra = '') => {
  if (cond) {
    ok++
    console.log('PASS', name)
  } else {
    fail++
    console.log('FAIL', name, extra)
  }
}

const chrome = spawn('C:\\Program Files\\Google\\Chrome\\application\\chrome.exe', [
  '--headless=new', '--disable-gpu', '--no-sandbox', '--no-first-run',
  `--remote-debugging-port=${PORT}`, `--user-data-dir=${join(tmpdir(), 'set' + Date.now())}`,
  '--window-size=1600,1000', 'about:blank',
], { stdio: 'ignore' })

try {
  for (let i = 0; i < 60; i++) {
    try { if ((await fetch(`http://127.0.0.1:${PORT}/json/version`)).ok) break } catch { /* 等待 */ }
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

  // React 受控输入必须走原生 setter，否则 onChange 收不到
  const setInput = (sel, idx, val) =>
    js(`(() => {
      const el = document.querySelectorAll('${sel}')[${idx}]
      if (!el) return 'no-el'
      const set = Object.getOwnPropertyDescriptor(el.constructor.prototype, 'value').set
      set.call(el, ${JSON.stringify(val)})
      el.dispatchEvent(new Event('input', { bubbles: true }))
      return el.value
    })()`)
  const getInput = (sel, idx) => js(`document.querySelectorAll('${sel}')[${idx}]?.value ?? null`)
  const grid = '.set-model-grid input'
  const clickText = (sel, text) =>
    js(`(() => {
      const b = [...document.querySelectorAll('${sel}')].find(x => x.textContent.trim().includes('${text}'))
      if (!b) return false
      b.click()
      return true
    })()`)

  await new Promise((r) => ws.addEventListener('open', r, { once: true }))
  await send('Emulation.setDeviceMetricsOverride', { width: 1600, height: 1000, deviceScaleFactor: 1, mobile: false })
  await send('Page.navigate', { url: `${API}/#/dashboard` })
  await sleep(3500)

  // 1) 侧栏底部设置入口
  assert('侧栏有「设置」入口', await js(`(() => { const b = document.querySelector('.sidebar-set'); return !!b && b.textContent.includes('设置') })()`))
  await js(`document.querySelector('.sidebar-set').click()`)
  await sleep(900)
  assert('点击后打开设置弹窗', await js(`!!document.querySelector('.modal')`))
  assert('弹窗标题为设置', await js(`document.querySelector('.modal-head h3')?.textContent === '设置'`))
  assert('有模型卡片', (await js(`document.querySelectorAll('.set-model').length`)) >= 1)
  assert('可添加模型', await js(`!![...document.querySelectorAll('.modal .btn')].find(b => b.textContent.trim() === '添加')`))

  // 2) 改为本机不可达端点，验证「测试」失败提示（不向公网发真实请求）
  await setInput(grid, 0, 'http://127.0.0.1:9/v1')
  await setInput(grid, 2, 'ui-test-model')
  await sleep(200)
  await clickText('.set-model .btn', '测试')
  await sleep(4000)
  assert('不可达端点给出错误提示', await js(`!!document.querySelector('.modal .note.err')`), await js(`document.querySelector('.modal .note.err')?.textContent || ''`))
  assert('失败后仍保持弹窗打开', await js(`!!document.querySelector('.modal')`))

  // 3) 保存（本机端点允许无密钥）→ 热生效
  await clickText('.modal-foot .btn', '保存')
  await sleep(2500)
  const footTxt = await js(`document.querySelector('.sidebar .foot')?.textContent || ''`)
  assert('侧栏 LLM 显示模型名（无 eino 前缀）', footTxt.includes('ui-test-model') && !footTxt.includes('eino:'), footTxt)
  const health = await (await fetch(`${API}/api/health`)).json()
  assert('后端 health 热切换生效', health.llm === 'ui-test-model', health.llm)

  // 4) 关闭再打开：配置已持久化
  await js(`document.querySelector('.modal .icon-btn').click()`)
  await sleep(400)
  assert('弹窗可关闭', await js(`!document.querySelector('.modal')`))
  await js(`document.querySelector('.sidebar-set').click()`)
  await sleep(900)
  assert('重开后读到已保存的模型名', (await getInput(grid, 2)) === 'ui-test-model', await getInput(grid, 2))
  assert('重开后 base_url 保持', (await getInput(grid, 0)) === 'http://127.0.0.1:9/v1', await getInput(grid, 0))

  mkdirSync('shots', { recursive: true })
  const shot = await send('Page.captureScreenshot', { format: 'png' })
  writeFileSync('shots/35-model-settings.png', Buffer.from(shot.data, 'base64'))
  console.log('saved shots/35-model-settings.png')

  // 6) 恢复为未配置：删掉模型再保存
  await clickText('.set-model .btn', '删除')
  await sleep(200)
  await clickText('.modal-foot .btn', '保存')
  await sleep(2500)
  const restored = await (await fetch(`${API}/api/settings`)).json()
  assert('恢复后回到未配置状态', restored.online === false && restored.engine === 'unconfigured' && restored.has_key === false, JSON.stringify(restored))
  await js(`document.querySelector('.modal .icon-btn').click()`)
  await sleep(300)
  ws.close()
} finally {
  chrome.kill()
  console.log(`\n${ok} passed, ${fail} failed`)
  process.exit(fail ? 1 : 0)
}
