// 端到端验证「AI 助手真的走 eino 调用模型」：
// 起一个本地 OpenAI 协议 mock，把模型配置指过去，再发 /api/assist，断言回复来自 mock。
import { createServer } from 'node:http'

const API = 'http://127.0.0.1:8080'
const MOCK_PORT = 18099
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

// mock 回复的表达式要能通过引擎静态校验，这样 valid=true 才说明整条链路（含校验）跑通
const MOCK_EXPR = 'vol20 = ts_std_dev(pct_chg, 20)\nalpha = -1 * cs_rank(vol20)'
const REPLY_TEXT = '这是来自 mock 模型的真实回复：低波动因子，取 20 日收益波动率的负排名。'

let seenAuth = ''
let seenBody = ''
const server = createServer((req, res) => {
  let body = ''
  req.on('data', (c) => (body += c))
  req.on('end', () => {
    if (req.url?.endsWith('/chat/completions')) {
      seenAuth = req.headers.authorization || ''
      seenBody = body
      const payload = {
        id: 'chatcmpl-mock',
        object: 'chat.completion',
        created: Math.floor(Date.now() / 1000),
        model: 'mock-model',
        choices: [
          {
            index: 0,
            message: { role: 'assistant', content: JSON.stringify({ reply: REPLY_TEXT, expr: MOCK_EXPR }) },
            finish_reason: 'stop',
          },
        ],
        usage: { prompt_tokens: 10, completion_tokens: 20, total_tokens: 30 },
      }
      res.writeHead(200, { 'Content-Type': 'application/json' })
      res.end(JSON.stringify(payload))
      return
    }
    res.writeHead(404).end('{}')
  })
})

await new Promise((r) => server.listen(MOCK_PORT, '127.0.0.1', r))
console.log('mock openai listening on', MOCK_PORT)

const put = (body) =>
  fetch(`${API}/api/settings`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })

try {
  // 1) 指向 mock
  const saved = await put({
    base_url: `http://127.0.0.1:${MOCK_PORT}/v1`,
    model: 'mock-model',
    temperature: 0.2,
    timeout_sec: 30,
    api_key: 'sk-mock-key-123456',
  }).then((r) => r.json())
  assert('配置指向 mock 端点并生效', saved.engine === 'mock-model', JSON.stringify(saved))

  // 2) 连通性测试
  const t = await fetch(`${API}/api/settings/test`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ base_url: `http://127.0.0.1:${MOCK_PORT}/v1`, model: 'mock-model', temperature: 0.2, timeout_sec: 20 }),
  })
  const tj = await t.json()
  assert('测试连接通过并返回延迟', t.ok && tj.ok === true && typeof tj.latency_ms === 'number', JSON.stringify(tj).slice(0, 200))

  // 3) AI 助手对话：应返回 mock 的回复与表达式，且表达式通过校验
  const a = await fetch(`${API}/api/assist`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ messages: [{ role: 'user', content: '帮我做一个低波动因子' }], expr: '' }),
  })
  const aj = await a.json()
  assert('assist 请求成功', a.ok, JSON.stringify(aj).slice(0, 300))
  assert('回复内容来自 mock 模型', (aj.reply || '').includes('mock 模型'), aj.reply)
  assert('返回的表达式来自 mock', (aj.expr || '').includes('ts_std_dev(pct_chg, 20)'), aj.expr)
  assert('表达式通过引擎校验（valid=true）', aj.valid === true && (aj.errors ?? []).length === 0, JSON.stringify(aj.errors))
  assert('请求携带了 Authorization 头', seenAuth.startsWith('Bearer sk-mock'), seenAuth)
  assert('请求未携带 max_tokens（输出长度交给模型自身上限）', !seenBody.includes('"max_tokens"') && !seenBody.includes('max_tokens'), seenBody.slice(0, 160))
  assert('只调用一次（无重试）', aj.attempts === 1, String(aj.attempts))

  // 4) 恢复默认：清空密钥 → 回到未配置状态，避免假配置留在库里
  const back = await put({
    base_url: 'https://api.deepseek.com/v1',
    model: 'deepseek-chat',
    temperature: 0.3,
    timeout_sec: 90,
    clear_key: true,
  }).then((r) => r.json())
  assert('测试结束已恢复未配置状态', back.engine === 'unconfigured' && back.online === false, JSON.stringify(back))

  const off = await fetch(`${API}/api/assist`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ messages: [{ role: 'user', content: '低波动因子' }], expr: '' }),
  })
  const offj = await off.json().catch(() => ({}))
  assert('清除配置后 assist 明确报错（无降级）', off.status === 503 && /模型未配置/.test(offj.error || ''), `${off.status} ${offj.error}`)
  assert('health 返回 unconfigured', (await (await fetch(`${API}/api/health`)).json()).llm === 'unconfigured')
} finally {
  server.close()
  console.log(`\n${ok} passed, ${fail} failed`)
  process.exit(fail ? 1 : 0)
}
