// 会话鉴权回归：登录门、错误令牌提示、HttpOnly Cookie 会话、刷新保持、退出登录
// 用法：node scripts/verify-auth.mjs [baseUrl] [token]
import { spawn } from 'node:child_process'
import { mkdirSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..')
const SHOTS = join(ROOT, 'shots')
mkdirSync(SHOTS, { recursive: true })
const API = process.argv[2] || 'http://127.0.0.1:8080'
const TOKEN = process.argv[3] || process.env.FACTOR_LAB_TOKEN || 'test-token-123'
const PORT = 9381

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
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
  `--remote-debugging-port=${PORT}`, `--user-data-dir=${join(tmpdir(), 'auth' + Date.now())}`,
  '--window-size=1440,900', 'about:blank',
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
  const js = (expression) => send('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true }).then((r) => r.result?.value)

  // React 受控输入：必须用原生 value setter 触发 onChange，直接赋 value 不生效
  const fillToken = (val) => js(`(() => {
    const el = document.querySelector('#fl-token')
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set
    setter.call(el, ${JSON.stringify(val)})
    el.dispatchEvent(new Event('input', { bubbles: true }))
    return true
  })()`)
  await new Promise((r) => ws.addEventListener('open', r, { once: true }))
  await send('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false })

  // 0) 服务端状态
  const sess = await (await fetch(`${API}/api/session`)).json()
  assert('服务端启用了访问令牌', sess.required === true && sess.authed === false, JSON.stringify(sess))
  const anon = await fetch(`${API}/api/factors`)
  assert('未登录访问业务接口返回 401', anon.status === 401, String(anon.status))

  // 1) 登录门
  await send('Page.navigate', { url: `${API}/#/dashboard` })
  await sleep(2200)
  assert('未登录渲染登录门（不渲染主界面）', await js(`!!document.querySelector('.login-card') && !document.querySelector('.sidebar')`))
  assert('登录门含令牌输入框', await js(`!!document.querySelector('#fl-token') && document.querySelector('#fl-token').type === 'password'`))
  assert('登录门提示令牌来源', await js(`document.body.textContent.includes('FACTOR_LAB_TOKEN')`))

  // 2) 错误令牌 → 明确报错，仍停在登录门
  await fillToken('wrong-token')
  await js(`(() => { document.querySelector('.login-form .btn').click(); return true })()`)
  await sleep(1200)
  const errTxt = await js(`document.querySelector('.login-err')?.textContent || ''`)
  assert('错误令牌给出提示', errTxt.includes('不正确'), errTxt)
  assert('错误令牌后仍在登录门', await js(`!!document.querySelector('.login-card')`))

  const shot = await send('Page.captureScreenshot', { format: 'png' })
  writeFileSync(join(SHOTS, '37-login.png'), Buffer.from(shot.data, 'base64'))
  console.log('saved shots/37-login.png')

  // 3) 正确令牌 → 进入主界面
  await fillToken(TOKEN)
  await js(`(() => { document.querySelector('.login-form .btn').click(); return true })()`)
  await sleep(3000)
  assert('正确令牌进入主界面', await js(`!!document.querySelector('.sidebar') && !document.querySelector('.login-card')`))
  assert('登录后能取到因子数据', await js(`document.querySelectorAll('.sidebar .nav-item').length >= 3`))

  // 4) Cookie 属性：HttpOnly + SameSite=Strict（脚本读不到令牌）
  const cookies = (await send('Network.getAllCookies')).cookies || []
  const ck = cookies.find((c) => c.name === 'fl_session')
  assert('会话 Cookie 为 HttpOnly', !!ck && ck.httpOnly === true, JSON.stringify(ck))
  assert('会话 Cookie SameSite=Strict', !!ck && ck.sameSite === 'Strict', ck?.sameSite)
  assert('令牌不落在 localStorage', await js(`Object.keys(localStorage).length === 0`))

  // 5) 刷新后仍保持登录
  await send('Page.reload', { ignoreCache: false })
  await sleep(2500)
  assert('刷新后仍保持登录（Cookie 会话）', await js(`!!document.querySelector('.sidebar') && !document.querySelector('.login-card')`))

  // 6) 退出登录 → 回到登录门
  const out = await js(`fetch('/api/session', { method: 'DELETE' }).then(r => r.json())`)
  assert('退出登录接口返回未授权态', out?.authed === false, JSON.stringify(out))
  await send('Page.reload', { ignoreCache: false })
  await sleep(2200)
  assert('退出后回到登录门', await js(`!!document.querySelector('.login-card')`))

  ws.close()
} finally {
  chrome.kill()
  console.log(`\n${ok} passed, ${fail} failed`)
  if (fail > 0) process.exitCode = 1
}
