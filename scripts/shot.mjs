// 开发用：启动无头 Chrome（CDP）→ 页面交互 → 截图 → 关闭。无需额外依赖（Node 18+）。
// 用法：node scripts/shot.mjs [baseUrl]
import { writeFileSync, mkdirSync } from 'node:fs'
import { spawn } from 'node:child_process'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const BASE = process.argv[2] || process.env.BASE_URL || 'http://127.0.0.1:8080'
const PORT = 9333
const CHROME =
  process.env.CHROME_PATH ||
  'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe'
const PROFILE = join(tmpdir(), 'fl-cdp-' + Date.now())

mkdirSync('shots', { recursive: true })

const chrome = spawn(
  CHROME,
  [
    '--headless=new',
    '--disable-gpu',
    '--no-sandbox',
    '--no-first-run',
    '--disable-extensions',
    `--remote-debugging-port=${PORT}`,
    `--user-data-dir=${PROFILE}`,
    '--window-size=1440,950',
    'about:blank',
  ],
  { stdio: 'ignore', detached: false },
)

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

async function waitPort() {
  for (let i = 0; i < 60; i++) {
    try {
      const r = await fetch(`http://127.0.0.1:${PORT}/json/version`)
      if (r.ok) return true
    } catch {}
    await sleep(250)
  }
  return false
}

if (!(await waitPort())) {
  console.error('chrome 调试端口未就绪')
  chrome.kill()
  process.exit(1)
}

const list = await (await fetch(`http://127.0.0.1:${PORT}/json/list`)).json()
const page = list.find((t) => t.type === 'page')
const ws = new WebSocket(page.webSocketDebuggerUrl)
let seq = 0
const pending = new Map()
ws.addEventListener('message', (ev) => {
  const msg = JSON.parse(ev.data)
  if (msg.id && pending.has(msg.id)) {
    pending.get(msg.id)(msg.result ?? msg.error)
    pending.delete(msg.id)
  }
})
const send = (method, params = {}) =>
  new Promise((res) => {
    const i = ++seq
    pending.set(i, res)
    ws.send(JSON.stringify({ id: i, method, params }))
  })

await new Promise((r) => ws.addEventListener('open', r, { once: true }))
await send('Page.enable')
await send('Runtime.enable')
const setViewport = (w, h) =>
  send('Emulation.setDeviceMetricsOverride', { width: w, height: h, deviceScaleFactor: 1, mobile: false })
await setViewport(1440, 950)

async function goto(hash, wait = 2400) {
  await send('Page.navigate', { url: `${BASE}/#/${hash}` })
  await sleep(wait)
}
async function shot(name) {
  const r = await send('Page.captureScreenshot', { format: 'png', captureBeyondViewport: false })
  writeFileSync(`shots/${name}.png`, Buffer.from(r.data, 'base64'))
  console.log('shot:', name)
}
async function clickText(text) {
  const r = await send('Runtime.evaluate', {
    expression: `(() => {
      const b = [...document.querySelectorAll('button')].find(x => x.textContent.trim() === ${JSON.stringify(text)});
      if (!b) return 'NOT_FOUND';
      b.click(); return 'OK';
    })()`,
  })
  return r.result?.value
}
async function clickSel(sel) {
  const r = await send('Runtime.evaluate', {
    expression: `(() => { const b = document.querySelector(${JSON.stringify(sel)}); if(!b) return 'NOT_FOUND'; b.click(); return 'OK'; })()`,
  })
  return r.result?.value
}
async function metrics() {
  const r = await send('Runtime.evaluate', {
    expression: `JSON.stringify({
      docH: document.documentElement.scrollHeight,
      viewH: window.innerHeight,
      drawer: (() => { const d = document.querySelector('.drawer'); if (!d) return null; const b = d.getBoundingClientRect(); return {x:b.x,y:b.y,w:b.width,h:b.height}; })(),
      mask: (() => { const m = document.querySelector('.drawer-mask'); if (!m) return null; const b = m.getBoundingClientRect(); return {x:b.x,y:b.y,w:b.width,h:b.height}; })(),
      overflowX: document.documentElement.scrollWidth > window.innerWidth
    })`,
  })
  return r.result?.value
}

await goto('library')
console.log('详情:', await clickText('详情'))
await sleep(3200)
console.log('layout:', await metrics())
await shot('02-library-drawer')

console.log('close:', await clickSel('.drawer-head .icon-btn'))
await sleep(400)
console.log('编辑:', await clickText('编辑'))
await sleep(1200)
await shot('03-library-edit')
await clickSel('.drawer-head .icon-btn')
await sleep(400)

await goto('factory')
await shot('04-factory')

// 自定义下拉弹层：点开「从因子库载入」
console.log('open select:', await clickSel('.card .sel'))
await sleep(500)
console.log(
  'popover:',
  await send('Runtime.evaluate', {
    expression: `(() => { const p = document.querySelector('.popover'); if(!p) return 'NONE'; const b = p.getBoundingClientRect(); const a = document.querySelector('.card .sel').getBoundingClientRect(); return JSON.stringify({pop:{x:Math.round(b.x),y:Math.round(b.y),w:Math.round(b.width),h:Math.round(b.height)}, anchor:{x:Math.round(a.x),y:Math.round(a.y),w:Math.round(a.width),h:Math.round(a.height)}}); })()`,
  }).then((r) => r.result?.value),
)
await shot('07-select-open')
await send('Runtime.evaluate', { expression: `window.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape'}))` })
await sleep(300)

// 日期选择器
const dateBtn = await send('Runtime.evaluate', {
  expression: `(() => { const b = [...document.querySelectorAll('.sel')].find(x => x.textContent.includes('202')); if(!b) return 'NOT_FOUND'; b.click(); return 'OK'; })()`,
})
console.log('open date:', dateBtn.result?.value)
await sleep(500)
console.log(
  'calendar:',
  await send('Runtime.evaluate', {
    expression: `(() => { const p = document.querySelector('.popover .cal'); if(!p) return 'NONE'; const b = p.getBoundingClientRect(); return JSON.stringify({x:Math.round(b.x),y:Math.round(b.y),w:Math.round(b.width),h:Math.round(b.height), inView: b.right <= window.innerWidth && b.bottom <= window.innerHeight}); })()`,
  }).then((r) => r.result?.value),
)
await shot('08-date-open')
await send('Runtime.evaluate', { expression: `window.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape'}))` })
await sleep(300)

await goto('compose')
await shot('05-compose')

await setViewport(1024, 900)
await goto('factory', 2000)
await shot('06-factory-1024')

ws.close()
chrome.kill()
console.log('done')
