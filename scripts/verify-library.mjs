// 因子库页面验证：侧栏顺序、列精简、去评价状态筛选、指标排序、行点击详情
import { spawn } from 'node:child_process'
import { writeFileSync, mkdirSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const API = 'http://127.0.0.1:8080'
const PORT = 9373
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
  `--remote-debugging-port=${PORT}`, `--user-data-dir=${join(tmpdir(), 'lib' + Date.now())}`,
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

  await new Promise((r) => ws.addEventListener('open', r, { once: true }))
  await send('Emulation.setDeviceMetricsOverride', { width: 1600, height: 1000, deviceScaleFactor: 1, mobile: false })
  await send('Page.navigate', { url: `${API}/#/library` })
  await sleep(3500)

  // 1) 侧栏顺序：因子组合 在 因子库 之前
  const nav = await js(`[...document.querySelectorAll('.sidebar .nav-item')].map(b => b.textContent.replace(/\\d+$/, '').trim())`)
  const iCompose = nav.findIndex((t) => t.includes('因子组合'))
  const iLibrary = nav.findIndex((t) => t.includes('因子库'))
  assert('侧栏含「因子组合」与「因子库」', iCompose >= 0 && iLibrary >= 0, JSON.stringify(nav))
  assert('因子库排在因子组合下面', iLibrary === iCompose + 1, `compose=${iCompose} library=${iLibrary}`)

  // 2) 表头列结构
  const heads = await js(`[...document.querySelectorAll('table.tbl thead th')].map(t => t.textContent.replace(/[↑↓]/g, '').trim())`)
  const cols = heads.filter((h) => h !== '')
  assert('列 = 因子名称/分类/来源/RankIC/ICIR/超额收益（多头）/更新时间',
    cols.length === 7 &&
    cols[0] === '因子名称' && cols[1] === '分类' && cols[2] === '来源' &&
    cols[3] === 'RankIC' && cols[4] === 'ICIR' &&
    cols[5] === '超额收益（多头）' && cols[6] === '更新时间',
    JSON.stringify(cols))
  assert('已无 t 值列', !heads.some((h) => h === 't 值'))
  assert('已无操作列', !heads.some((h) => h.includes('详情') || h.includes('编辑')))

  // 3) 筛选条件中不再有评价状态
  const toolbar = await js(`document.querySelector('.toolbar')?.textContent || ''`)
  assert('筛选无「评价状态」', !toolbar.includes('评价状态') && !toolbar.includes('已评价') && !toolbar.includes('待评价'), toolbar)
  assert('分类/来源筛选仍在', toolbar.includes('全部分类') && toolbar.includes('全部来源'))

  // 4) 名称列只显示名称，无描述副行
  assert('名称列无描述副行', await js(`document.querySelectorAll('table.tbl tbody td.name .subtle').length === 0`))
  const nameTxt = await js(`document.querySelector('table.tbl tbody td.name')?.textContent || ''`)
  assert('名称列有内容', nameTxt.trim().length > 0, nameTxt)

  // 4.1) 更新时间格式：2026-08-01 11:30:00
  const timeTxt = await js(`document.querySelector('table.tbl tbody tr td:last-child')?.textContent.trim() || ''`)
  assert('更新时间格式为 YYYY-MM-DD HH:mm:ss', /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/.test(timeTxt), timeTxt)

  // 5) 三列指标可排序：降序 → 升序
  const colIdx = { RankIC: 3, ICIR: 4, '超额收益（多头）': 5 }
  const readCol = (i) =>
    js(`[...document.querySelectorAll('table.tbl tbody tr')].map(tr => {
      const t = tr.children[${i + 1}]?.textContent.trim() || ''
      if (!t || t === '—') return null
      return /%$/.test(t) ? parseFloat(t) : parseFloat(t)
    })`)

  for (const [label, idx] of Object.entries(colIdx)) {
    const thSel = `[...document.querySelectorAll('table.tbl thead th')].find(th => th.textContent.includes('${label}'))`
    // 首次点击 → 降序
    await js(`(() => { const th = ${thSel}; th.click(); return true })()`)
    await sleep(350)
    const desc = (await readCol(idx)).filter((v) => v !== null)
    const descOk = desc.every((v, i) => i === 0 || desc[i - 1] >= v)
    const arrowDown = await js(`(() => { const th = ${thSel}; return th.textContent.includes('↓') })()`)
    assert(`${label} 首次点击降序`, descOk && arrowDown, `len=${desc.length} head=${desc.slice(0, 4)}`)

    // 再次点击 → 升序
    await js(`(() => { const th = ${thSel}; th.click(); return true })()`)
    await sleep(350)
    const asc = (await readCol(idx)).filter((v) => v !== null)
    const ascOk = asc.every((v, i) => i === 0 || asc[i - 1] <= v)
    const arrowUp = await js(`(() => { const th = ${thSel}; return th.textContent.includes('↑') })()`)
    assert(`${label} 再点击升序`, ascOk && arrowUp, `head=${asc.slice(0, 4)}`)
  }

  // 6) 点行打开详情抽屉（编辑/复制/删除已收进抽屉）
  await js(`(() => { const n = document.querySelector('table.tbl tbody td.name'); n.dispatchEvent(new MouseEvent('click', { bubbles: true })); return true })()`)
  await sleep(500)
  assert('点行打开详情抽屉', await js(`!!document.querySelector('.drawer')`))
  const drawerActions = await js(`[...document.querySelectorAll('.drawer .btn')].map(b => b.textContent.trim()).join('|')`)
  assert('抽屉含 复制/删除/编辑', /复制/.test(drawerActions) && /删除/.test(drawerActions) && /编辑/.test(drawerActions), drawerActions)
  await js(`(() => { const b = document.querySelector('.drawer .icon-btn'); if (b) b.click(); return true })()`)
  await sleep(400)
  assert('抽屉可关闭', await js(`!document.querySelector('.drawer')`))

  // 截图：按 RankIC 降序排列的列表
  await js(`(() => { const th = [...document.querySelectorAll('table.tbl thead th')].find(t => t.textContent.includes('RankIC')); th.click(); return true })()`)
  await sleep(400)

  mkdirSync('shots', { recursive: true })
  const shot = await send('Page.captureScreenshot', { format: 'png' })
  writeFileSync('shots/34-library-new.png', Buffer.from(shot.data, 'base64'))
  console.log('saved shots/34-library-new.png')
  ws.close()
} finally {
  chrome.kill()
  console.log(`\n${ok} passed, ${fail} failed`)
  process.exit(fail ? 1 : 0)
}
