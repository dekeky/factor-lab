// 因子工厂 IDE 布局回归：左右分栏、顶栏参数/速查弹层、入库弹窗、弹层定位、AI 对话联动、评价结果图表。
// 用法：node scripts/verify-factory-ide.mjs [baseUrl]
import { spawn } from 'node:child_process'
import { mkdirSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..')
const SHOTS = join(ROOT, 'shots')
mkdirSync(SHOTS, { recursive: true })
const BASE = process.argv[2] || 'http://127.0.0.1:8080'
const PORT = 9341
const CHROME = process.env.CHROME_PATH || 'C:\\Program Files\\Google\\Chrome\\application\\chrome.exe'
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

const chrome = spawn(CHROME, [
  '--headless=new', '--disable-gpu', '--no-sandbox', '--no-first-run',
  `--remote-debugging-port=${PORT}`,
  `--user-data-dir=${join(tmpdir(), 'fl-ide-' + Date.now())}`,
  '--window-size=1600,1000', 'about:blank',
], { stdio: 'ignore' })

for (let i = 0; i < 60; i++) {
  try { if ((await fetch(`http://127.0.0.1:${PORT}/json/version`)).ok) break } catch {}
  await sleep(250)
}
const list = await (await fetch(`http://127.0.0.1:${PORT}/json/list`)).json()
const ws = new WebSocket(list.find((t) => t.type === 'page').webSocketDebuggerUrl)
let seq = 0
const pending = new Map()
const runtimeErrors = []
ws.addEventListener('message', (ev) => {
  const m = JSON.parse(ev.data)
  if (m.id && pending.has(m.id)) { pending.get(m.id)(m.result ?? m.error); pending.delete(m.id) }
  if (m.method === 'Runtime.exceptionThrown') {
    runtimeErrors.push(m.params.exceptionDetails.exception?.description || m.params.exceptionDetails.text)
  }
  if (m.method === 'Runtime.consoleAPICalled' && m.params.type === 'error') {
    runtimeErrors.push(m.params.args.map((a) => a.value ?? a.description ?? '').join(' '))
  }
})
const send = (method, params = {}) =>
  new Promise((res) => { const i = ++seq; pending.set(i, res); ws.send(JSON.stringify({ id: i, method, params })) })
const js = (expression) => send('Runtime.evaluate', { expression, returnByValue: true }).then((r) => r.result?.value)
const jsAwait = (expression) => send('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true }).then((r) => r.result?.value)

const results = []
function check(name, ok, detail = '') {
  results.push({ name, ok: !!ok })
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${detail ? ' — ' + detail : ''}`)
}
async function waitFor(expression, timeout = 12000) {
  const t0 = Date.now()
  while (Date.now() - t0 < timeout) {
    if (await js(expression)) return true
    await sleep(250)
  }
  return false
}
async function shot(name) {
  const r = await send('Page.captureScreenshot', { format: 'png' })
  writeFileSync(join(SHOTS, name), Buffer.from(r.data, 'base64'))
}

await new Promise((r) => ws.addEventListener('open', r, { once: true }))
await send('Page.enable')
await send('Runtime.enable')
await send('Emulation.setDeviceMetricsOverride', { width: 1600, height: 1000, deviceScaleFactor: 1, mobile: false })
await send('Page.navigate', { url: `${BASE}/#/factory` })
await sleep(3200)

// 1) 左右分栏与全高布局
const L = await js(`(() => {
  const r = s => { const e = document.querySelector(s); if (!e) return null; const b = e.getBoundingClientRect();
    return { x: Math.round(b.x), y: Math.round(b.y), w: Math.round(b.width), h: Math.round(b.height) } }
  return {
    shell: r('.fw-shell'), editor: r('.fw-editor-col'), chat: r('.fw-chat'),
    cmBody: r('.fw-editor .cm-body'), top: r('.fw-top'),
    hasEditor: !!document.querySelector('.fw-editor .cm-src'),
    hasChat: !!document.querySelector('.ac-input-wrap textarea'),
    hasResizerX: !!document.querySelector('.fw-resizer-x'),
    noBottom: !document.querySelector('.fw-bottom') && !document.querySelector('.fw-btab'),
    overflowX: document.body.scrollWidth - window.innerWidth,
  }
})()`)
console.log('布局:', JSON.stringify(L))
check('IDE 骨架渲染', !!(L?.shell && L?.editor && L?.chat))
check('AI 助手位于编辑器右侧', L?.chat.x >= L?.editor.x + L?.editor.w - 10, `chat.x=${L?.chat.x}, editor右缘=${L?.editor.x + L?.editor.w}`)
check('左右两列等高铺满工作区', Math.abs(L?.chat.h - L?.shell.h) < 4 && Math.abs(L?.editor.h - L?.shell.h) < 4, `shell.h=${L?.shell.h}, chat.h=${L?.chat.h}`)
check('代码编辑区有实际高度', L?.cmBody.h > 150, `cm-body.h=${L?.cmBody.h}`)
check('编辑器 / 对话输入框 / 分隔条齐全', L?.hasEditor && L?.hasChat && L?.hasResizerX)
check('底部常驻面板已删除', L?.noBottom === true)
check('页面无横向溢出', L?.overflowX <= 2, `overflow=${L?.overflowX}px`)

// 2) 运行评价 → 参数弹窗（含股票池）→ 日期默认值；速查已删除
const clickTop = async (label) => {
  await js(`[...document.querySelectorAll('.fw-top .btn')].find(b => b.textContent.includes(${JSON.stringify(label)}))?.click()`)
  await sleep(300)
}
await clickTop('运行评价')
const runInfo = await js(`(() => {
  const modal = document.querySelector('.modal');
  if (!modal) return null;
  const dates = [...modal.querySelectorAll('.sel-label')].map(e => e.textContent.trim());
  const pool = modal.querySelector('.fw-pool');
  return {
    numbers: modal.querySelectorAll('input[type=number]').length,
    dates,
    dateOK: dates.length === 2 && dates.every(d => d.length === 10 && d[4] === '-' && d[7] === '-'),
    poolText: pool?.textContent?.replace(/\\s+/g, ' ').trim() ?? '',
    hasStartBtn: [...(modal.querySelectorAll('.modal-foot .btn') ?? [])].some(b => b.textContent.includes('开始评价')),
  }
})()`)
console.log('运行参数弹窗:', JSON.stringify(runInfo))
check('运行评价打开参数弹窗', runInfo?.numbers === 3 && runInfo?.hasStartBtn, JSON.stringify(runInfo))
check('评价区间日期自动回填', runInfo?.dateOK, JSON.stringify(runInfo?.dates))
// 股票池/天数取当前面板真实值，避免脚本硬编码某一数据源（finclaw 1655 日 vs 合成 500 日）
const hp = await (await fetch(`${BASE}/api/health`)).json()
check('弹窗内展示股票池信息', /000300\.SH/.test(runInfo?.poolText ?? '')
  && runInfo.poolText.includes(`${hp.panel.stocks} 只`)
  && runInfo.poolText.includes(`${hp.panel.dates} 日`), runInfo?.poolText)
check('速查入口已删除', await js(`![...document.querySelectorAll('.fw-top .btn')].some(b => b.textContent.includes('速查')) && ![...document.querySelectorAll('.fw-top .btn')].some(b => b.textContent.includes('参数'))`))
await js(`document.querySelector('.modal-head .icon-btn')?.click()`)
await sleep(280)
check('参数弹窗可取消（不触发评价）', await js(`!document.querySelector('.modal-mask')`))

// 3) 保存入库弹窗 + 自定义弹层定位稳定（历史问题：弹框乱飘）
await clickTop('保存入库')
check('入库弹窗无需先评价即可使用', await js(`!!document.querySelector('.modal .fw-panel input[type=text]') && !!document.querySelector('.modal .fw-panel .sel-label')`))
await js(`[...document.querySelectorAll('.modal .sel')].pop()?.click()`)
await sleep(180)
const popPos = `(() => {
  const e = document.querySelector('.popover'); const a = [...document.querySelectorAll('.modal .sel')].pop();
  if (!e || !a) return null; const b = e.getBoundingClientRect(), r = a.getBoundingClientRect();
  return { x: Math.round(b.x), y: Math.round(b.y), anchorBottom: Math.round(r.bottom), h: Math.round(b.height) }
})()`
const p1 = await js(popPos)
await sleep(800)
const p2 = await js(popPos)
check('下拉弹层出现且位置稳定', !!p1 && !!p2 && Math.abs(p1.y - p2.y) < 3 && Math.abs(p1.x - p2.x) < 3,
  `t1=${JSON.stringify(p1)} t2=${JSON.stringify(p2)}`)
check('弹层紧贴锚点下方', !!p1 && Math.abs(p1.y - p1.anchorBottom) < 30, `gap=${p1 ? p1.y - p1.anchorBottom : 'n/a'}px`)
await js(`document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))`)
await sleep(250)
await js(`document.querySelector('.modal-head .icon-btn')?.click()`)
await sleep(280)
check('入库弹窗可关闭', await js(`!document.querySelector('.modal-mask')`))

// 3b) 顶部操作条只属于编辑器列，不横跨 AI 助手列
const topScope = await js(`(() => {
  const top = document.querySelector('.fw-top'); const shell = document.querySelector('.fw-shell');
  const chat = document.querySelector('.fw-chat');
  if (!top || !shell || !chat) return null;
  const t = top.getBoundingClientRect(), c = chat.getBoundingClientRect();
  return { topRight: Math.round(t.right), chatLeft: Math.round(c.left), overChat: t.right > c.left + 2 };
})()`)
check('操作条不覆盖 AI 助手列', topScope && !topScope.overChat, JSON.stringify(topScope))

// 3c) 左侧「我的因子」列表（仅 source=user）+ 多标签编辑
// 先通过 Node 侧 fetch 造一个临时自建因子，再刷新页面让列表加载它
const created = await fetch('http://127.0.0.1:8080/api/factors', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ name: 'verify_user_factor', expr: 'alpha = -1 * cs_rank(close / ts_delay(close, 5) - 1)', category: 'user', source: 'user' }),
}).then((r) => r.json()).catch((e) => ({ error: String(e) }))
check('临时自建因子创建成功', !!created?.id, JSON.stringify(created))
await js(`location.reload()`)
await sleep(2600)
const libInfo = await js(`(() => {
  const lib = document.querySelector('.fw-lib'); const ed = document.querySelector('.fw-editor-col');
  if (!lib || !ed) return null;
  const l = lib.getBoundingClientRect(), e = ed.getBoundingClientRect();
  return {
    leftOK: l.right <= e.left + 2,
    items: lib.querySelectorAll('.fw-lib-item').length,
    headText: lib.querySelector('.fw-lib-head span')?.textContent,
    cats: [...lib.querySelectorAll('.fw-lib-cat')].map(x => x.textContent),
    names: [...lib.querySelectorAll('.fw-lib-item .fw-lib-name')].map(x => x.textContent),
  };
})()`)
console.log('我的因子列表:', JSON.stringify(libInfo))
check('左侧为「我的因子」列表（仅用户自建）', libInfo && libInfo.leftOK && libInfo.headText === '我的因子' && libInfo.items >= 2, JSON.stringify(libInfo))
check('草稿文件与已保存因子同列展示', (libInfo?.names ?? []).some(n => n.includes('未命名')) && (libInfo?.names ?? []).includes('verify_user_factor'), JSON.stringify(libInfo?.names))
check('文件标签栏已并入列表（编辑器上方无标签条）', await js(`!document.querySelector('.fw-tabbar') && !document.querySelector('.fw-filetab')`))
await js(`(() => {
  const i = document.querySelector('.fw-lib-search input');
  const set = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set;
  set.call(i, 'verify');
  i.dispatchEvent(new Event('input', { bubbles: true }));
})()`)
await sleep(300)
const filteredNames = await js(`[...document.querySelectorAll('.fw-lib-item .fw-lib-name')].map(x => x.textContent)`)
check('因子列表搜索过滤（草稿同样过滤）', filteredNames.length === 1 && filteredNames[0] === 'verify_user_factor', JSON.stringify(filteredNames))
await js(`(() => {
  const i = document.querySelector('.fw-lib-search input');
  const set = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set;
  set.call(i, '');
  i.dispatchEvent(new Event('input', { bubbles: true }));
})()`)
await sleep(250)
// 点已保存因子：打开并聚焦到该文件
await js(`[...document.querySelectorAll('.fw-lib-item')].find(e => e.textContent.includes('verify_user_factor'))?.click()`)
await sleep(340)
const tabInfo = await js(`(() => ({
  activeName: document.querySelector('.fw-lib-item.on .fw-lib-name')?.textContent,
  editorLen: document.querySelector('.fw-editor .cm-src')?.value.length,
  items: document.querySelectorAll('.fw-lib-item').length,
}))()`)
check('点击因子文件在编辑器打开', tabInfo?.activeName === 'verify_user_factor' && tabInfo.editorLen > 30, JSON.stringify(tabInfo))
// 点草稿：切回未保存的编辑中文件
await js(`[...document.querySelectorAll('.fw-lib-item')].find(e => e.textContent.includes('未命名'))?.click()`)
await sleep(300)
check('列表可在多个因子文件间切换', await js(`document.querySelector('.fw-lib-item.on .fw-lib-name')?.textContent?.includes('未命名') && document.querySelector('.fw-editor .cm-src')?.value.includes('cs_rank(mom20)')`))
// 关闭已保存因子的文件：行仍在（已保存），仅关闭编辑
await js(`(() => {
  const row = [...document.querySelectorAll('.fw-lib-item')].find(e => e.textContent.includes('verify_user_factor'));
  row?.querySelector('.fw-lib-x')?.click();
})()`)
await sleep(300)
check('关闭文件不删除已保存因子', await js(`(() => {
  const names = [...document.querySelectorAll('.fw-lib-item .fw-lib-name')].map(x => x.textContent);
  return names.includes('verify_user_factor') && document.querySelectorAll('.fw-lib-item').length >= 2;
})()`))

// 4) AI 对话 → 表达式卡片 → 应用到编辑器
// 先给编辑器注入一段不同表达式，确保 AI 应用后的内容与当前不同，能真正验证写入联动。
await js(`(() => {
  const t = document.querySelector('.fw-editor .cm-src');
  const set = Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype, 'value').set;
  set.call(t, 'rev = -1 * cs_rank(close / ts_delay(close, 5) - 1)');
  t.dispatchEvent(new Event('input', { bubbles: true }));
})()`)
await sleep(400)
const before = await js(`document.querySelector('.fw-editor .cm-src').value`)
check('编辑器预置内容成功', before?.includes('cs_rank(close'), `${before?.length} 字符`)

await js(`document.querySelector('.ac-suggest button')?.click()`)
const gotCard = await waitFor(`!!document.querySelector('.ac-code-card')`, 12000)
const aiInfo = await js(`(() => ({
  msgs: document.querySelectorAll('.ac-msg').length,
  cards: document.querySelectorAll('.ac-code-card').length,
  valid: document.querySelector('.ac-code-head .tag')?.textContent,
  model: document.querySelector('.ac-ctx .tag')?.textContent,
  cardExpr: document.querySelector('.ac-code')?.textContent?.trim(),
}))()`)
check('AI 对话返回消息与表达式卡片', gotCard && aiInfo?.msgs >= 2 && aiInfo?.cards >= 1, JSON.stringify({ ...aiInfo, cardExpr: undefined }))
await js(`document.querySelector('.ac-code-foot .btn.primary')?.click()`)
await sleep(400)
const after = await js(`document.querySelector('.fw-editor .cm-src').value`)
const applied = await js(`document.querySelector('.ac-code-foot .btn')?.textContent`)
check('应用按钮把 AI 表达式写入编辑器', !!after && after !== before && after.trim() === aiInfo?.cardExpr,
  `${before?.length} -> ${after?.length} 字符`)
check('应用后标记为未保存修改', await js(`!!document.querySelector('.fw-dot.dirty')`))
check('应用状态回显', applied?.includes('已应用'), applied)
await shot('15-factory-ide-ai.png')

// 5) 运行评价 → 参数弹窗确认 → 结果弹窗展示
await js(`[...document.querySelectorAll('.fw-top .btn')].find(b => b.textContent.includes('运行评价'))?.click()`)
await sleep(350)
check('运行评价先弹参数选择', await js(`!!document.querySelector('.modal .fw-pool') && !!document.querySelector('.modal input[type=number]')`))
await js(`[...document.querySelectorAll('.modal-foot .btn')].find(b => b.textContent.includes('开始评价'))?.click()`)
const gotModal = await waitFor(`!!document.querySelector('.modal-mask') && !!document.querySelector('.modal .fw-kpis')`, 30000)
const ev = await js(`(() => {
  const c = document.querySelector('.modal .chart canvas');
  return {
    title: document.querySelector('.modal-head h3')?.textContent,
    sub: document.querySelector('.modal-sub')?.textContent,
    kpis: document.querySelectorAll('.modal .fw-kpis .kpi').length,
    subtabs: document.querySelectorAll('.modal .fw-subtabs button').length,
    canvas: c ? { w: c.width, h: c.height, cssW: Math.round(c.getBoundingClientRect().width) } : null,
    flash: document.querySelector('.fw-flash')?.textContent || '',
  }
})()`)
console.log('评价弹窗:', JSON.stringify(ev))
check('运行评价后弹窗展示结果', gotModal && ev?.title?.includes('评价结果'), ev?.title)
check('弹窗含 KPI 与四个结果视图', ev?.kpis === 4 && ev?.subtabs === 4, `kpis=${ev?.kpis}, subtabs=${ev?.subtabs}`)
check('弹窗副标题带参数与耗时', !!ev?.sub && /持有/.test(ev.sub) && /耗时/.test(ev.sub), ev?.sub)
check('图表 canvas 有实际尺寸', !!ev?.canvas && ev.canvas.w > 100 && ev.canvas.h > 100, JSON.stringify(ev?.canvas))
check('评价过程无错误提示', !ev?.flash, ev?.flash || '')
await shot('16-factory-ide-result.png')

// 5b) 关闭弹窗 → 评价记录 → 查看 / 载入 / Esc
await js(`document.querySelector('.modal-head .icon-btn')?.click()`)
await sleep(280)
check('结果弹窗可关闭', await js(`!document.querySelector('.modal-mask')`))
await js(`[...document.querySelectorAll('.fw-top .btn')].find(b => b.textContent.includes('评价记录'))?.click()`)
await sleep(320)
const recInfo = await js(`(() => ({
  items: document.querySelectorAll('.rec-item').length,
  metrics: document.querySelector('.rec-metrics')?.textContent,
}))()`)
check('评价记录自动保存并可查看', recInfo?.items >= 1 && /RankIC/.test(recInfo?.metrics ?? ''), JSON.stringify(recInfo))
await shot('17-eval-records.png')
await js(`[...document.querySelectorAll('.rec-actions .btn')].find(b => b.textContent.includes('查看'))?.click()`)
await sleep(420)
check('从记录打开完整结果弹窗', await js(`!!document.querySelector('.modal-mask') && !!document.querySelector('.modal .fw-kpis')`))
await js(`document.querySelector('.modal-head .icon-btn')?.click()`)
await sleep(280)
await js(`[...document.querySelectorAll('.fw-top .btn')].find(b => b.textContent.includes('评价记录'))?.click()`)
await sleep(320)
await js(`[...document.querySelectorAll('.rec-actions .btn')].find(b => b.textContent.includes('载入'))?.click()`)
await sleep(380)
check('记录可载回编辑器（弹窗关闭且文件就绪）', await js(`!document.querySelector('.modal-mask') && !!document.querySelector('.fw-lib-item.on')`))
await js(`[...document.querySelectorAll('.fw-top .btn')].find(b => b.textContent.includes('评价记录'))?.click()`)
await sleep(320)
await js(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))`)
await sleep(280)
check('Esc 可关闭弹窗', await js(`!document.querySelector('.modal-mask')`))

// 5c) 保存入库：顶部入口打开弹窗 + 无需评价即可保存表达式到因子库
await clickTop('保存入库')
check('顶部「保存入库」打开入库弹窗', await js(`!!document.querySelector('.modal .fw-panel input[type=text]')`))
await shot('18-save-modal.png')
await js(`(() => {
  const i = document.querySelector('.modal .fw-panel input[type=text]');
  const set = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set;
  set.call(i, 'verify_temp_factor');
  i.dispatchEvent(new Event('input', { bubbles: true }));
})()`)
await js(`[...document.querySelectorAll('.modal .fw-panel .btn')].find(b => b.textContent.includes('新建并入库'))?.click()`)
const saveOk = await waitFor(`!!document.querySelector('.modal .note.ok')`, 6000)
const saveNote = await js(`document.querySelector('.modal .note.ok')?.textContent || ''`)
check('表达式直接保存进因子库', saveOk && /已入库/.test(saveNote ?? ''), String(saveNote))
await js(`document.querySelector('.modal-head .icon-btn')?.click()`)
await sleep(280)
check('入库弹窗再次可关闭', await js(`!document.querySelector('.modal-mask')`))
const cleanup = await jsAwait(`fetch('/api/factors').then(r => r.json()).then(fs => Promise.all(
  ['verify_temp_factor', 'verify_user_factor'].map((name) => {
    const f = fs.find(x => x.name === name);
    if (!f) return { name, found: false };
    return fetch('/api/factors/' + f.id, { method: 'DELETE' }).then(r => r.json()).then(d => ({ name, found: true, deleted: d.deleted }));
  })
))`)
check('测试因子已清理（含 API 直建的自建因子）', Array.isArray(cleanup) && cleanup.every(c => c.found && c.deleted), JSON.stringify(cleanup))

// 6) AI 面板折叠 / 展开
await js(`[...document.querySelectorAll('.ac-head-actions .ac-icon')].pop()?.click()`)
await sleep(250)
check('AI 面板可折叠', await js(`!!document.querySelector('.fw-chat-collapsed')`))
await js(`document.querySelector('.fw-chat-collapsed')?.click()`)
await sleep(250)
check('AI 面板可展开', await js(`!!document.querySelector('.ac') && !document.querySelector('.fw-chat-collapsed')`))

// 7) 顶栏作用域：工厂页无全局顶栏，其他页面顶栏照旧
await js(`location.hash = '#/library'`)
await sleep(900)
const libTitle = await js(`document.querySelector('.topbar .title')?.textContent`)
check('其他页面顶栏正常显示', libTitle === '因子库', String(libTitle))
await js(`location.hash = '#/factory'`)
await sleep(800)
check('因子工厂不再渲染重复标题栏', await js(`!document.querySelector('.topbar')`))

// 8) 运行时异常
const fatal = runtimeErrors.filter((e) => /Uncaught|TypeError|ReferenceError|is not a function/i.test(e))
check('无前端运行时异常', fatal.length === 0, fatal.slice(0, 2).join(' | ') || `非致命日志 ${runtimeErrors.length} 条`)

const failed = results.filter((r) => !r.ok)
console.log(`\n===== ${results.length - failed.length}/${results.length} 项通过 =====`)
if (failed.length) console.log('失败项:', failed.map((f) => f.name).join('、'))
ws.close()
chrome.kill()
process.exit(failed.length ? 1 : 0)
