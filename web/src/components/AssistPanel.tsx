import { useEffect, useRef, useState } from 'react'
import { api } from '../api'
import type { AssistStreamEvent, ComboProposal } from '../types'
import { displayLLM } from './Layout'
import { AiIcon } from './AiIcon'

interface Msg {
  role: 'user' | 'assistant'
  content: string
  expr?: string
  combo?: ComboProposal
  valid?: boolean
  errors?: string[]
  applied?: boolean
  streaming?: boolean
}

const SUGGESTIONS = [
  '写一个短期反转因子',
  '结合换手率做反转增强',
  '帮我解释当前表达式',
]

const QUICK = ['解释当前表达式', '优化这个因子', '给一个变体']

/**
 * AssistPanel：全高 AI 对话面板（Cursor Chat 形态），因子工厂与因子组合共用。
 *
 * 因子工厂：AI 只产出候选表达式，用户点击确认后才写入编辑器；表达式先经引擎校验。
 * 因子组合：AI 只从现有因子库挑成分；校验通过的方案直接写入组合编辑器。
 */
export function AssistPanel({
  expr,
  onApply,
  onApplyCombo,
  mode = 'factor',
  llm,
  fileName,
  onCollapse,
  suggestions = SUGGESTIONS,
  quick = QUICK,
  placeholder = '描述你的因子想法…（Enter 发送，Shift+Enter 换行）',
  intro,
}: {
  expr: string
  onApply?: (expr: string) => void
  onApplyCombo?: (combo: ComboProposal) => void
  mode?: 'factor' | 'combo'
  llm: string
  fileName: string
  onCollapse: () => void
  suggestions?: string[]
  quick?: string[]
  placeholder?: string
  intro?: { title: string; desc: string }
}) {
  const [msgs, setMsgs] = useState<Msg[]>([])
  const [input, setInput] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [showCtx, setShowCtx] = useState(false)
  const offline = !llm || llm === 'unconfigured' // 未配置：不做降级，明确提示去「设置」配置
  const scrollRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const onEventRef = useRef<(ev: AssistStreamEvent) => void>(() => {})
  const exprRef = useRef(expr)
  const busyRef = useRef(false)
  const modeRef = useRef(mode)
  const onApplyComboRef = useRef(onApplyCombo)
  exprRef.current = expr
  modeRef.current = mode
  onApplyComboRef.current = onApplyCombo

  // 新消息/思考中自动滚到底部
  useEffect(() => {
    const el = scrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [msgs, busy])

  // Ctrl/Cmd+L 聚焦对话输入框（对齐 Cursor 的快捷键习惯）
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && (e.key === 'l' || e.key === 'L')) {
        e.preventDefault()
        inputRef.current?.focus()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  useEffect(() => {
    return () => {
      wsRef.current?.close()
      wsRef.current = null
    }
  }, [])

  onEventRef.current = (ev: AssistStreamEvent) => {
    if (!busyRef.current) return
    if (ev.type === 'delta' && ev.text) {
      setMsgs((prev) => {
        const last = prev[prev.length - 1]
        if (last?.role === 'assistant' && last.streaming) {
          return [...prev.slice(0, -1), { ...last, content: last.content + ev.text }]
        }
        return [...prev, { role: 'assistant', content: ev.text ?? '', streaming: true }]
      })
      return
    }
    if (ev.type === 'retry') {
      setMsgs((prev) => {
        const last = prev[prev.length - 1]
        if (last?.role === 'assistant' && last.streaming) {
          return [...prev.slice(0, -1), { role: 'assistant', content: '', streaming: true }]
        }
        return prev
      })
      return
    }
    if (ev.type === 'done') {
      const combo = ev.combo
      const applied = !!(combo && ev.valid && onApplyComboRef.current)
      if (applied && combo && onApplyComboRef.current) {
        onApplyComboRef.current(combo)
      }
      setMsgs((prev) => {
        const last = prev[prev.length - 1]
        const bot: Msg = {
          role: 'assistant',
          content: ev.reply || last?.content || '',
          expr: ev.expr,
          combo,
          valid: ev.valid,
          errors: ev.errors,
          applied,
          streaming: false,
        }
        if (last?.role === 'assistant' && last.streaming) {
          return [...prev.slice(0, -1), bot]
        }
        return [...prev, bot]
      })
      busyRef.current = false
      setBusy(false)
      return
    }
    if (ev.type === 'error') {
      setErr(ev.message || '辅助失败')
      setMsgs((prev) => {
        const last = prev[prev.length - 1]
        if (last?.role === 'assistant' && last.streaming) {
          if (!last.content) return prev.slice(0, -1)
          return [...prev.slice(0, -1), { ...last, streaming: false }]
        }
        return prev
      })
      busyRef.current = false
      setBusy(false)
    }
  }

  function attachWS(ws: WebSocket) {
    ws.onmessage = (e) => {
      try {
        onEventRef.current(JSON.parse(String(e.data)) as AssistStreamEvent)
      } catch {
        setErr('助手返回了无法解析的消息')
        setBusy(false)
      }
    }
    ws.onerror = () => {
      // 握手失败由 ensureWS / send 的 catch 处理（可回退 HTTP）；已连通后的中断才在这里提示。
      if (ws.readyState === WebSocket.OPEN) {
        busyRef.current = false
        setErr('AI 助手通道异常中断')
        setBusy(false)
      }
    }
    ws.onclose = () => {
      if (wsRef.current === ws) wsRef.current = null
      if (busyRef.current) {
        busyRef.current = false
        setErr('AI 助手连接已断开')
        setBusy(false)
        setMsgs((prev) => {
          const last = prev[prev.length - 1]
          if (last?.role === 'assistant' && last.streaming) {
            if (!last.content) return prev.slice(0, -1)
            return [...prev.slice(0, -1), { ...last, streaming: false }]
          }
          return prev
        })
      }
    }
  }

  function ensureWS(): Promise<WebSocket> {
    const cur = wsRef.current
    if (cur && cur.readyState === WebSocket.OPEN) return Promise.resolve(cur)
    if (cur && cur.readyState === WebSocket.CONNECTING) {
      return new Promise((resolve, reject) => {
        cur.addEventListener('open', () => resolve(cur), { once: true })
        cur.addEventListener('error', () => reject(new Error('无法连接 AI 助手通道')), { once: true })
      })
    }
    const ws = new WebSocket(api.assistWSUrl())
    wsRef.current = ws
    attachWS(ws)
    return new Promise((resolve, reject) => {
      ws.addEventListener('open', () => resolve(ws), { once: true })
      ws.addEventListener('error', () => reject(new Error('ws')), { once: true })
    })
  }

  function historyPayload(next: Msg[]) {
    return next
      .filter((m) => !m.streaming && m.content)
      .map((m) => ({ role: m.role, content: m.content }))
  }

  async function sendViaHTTP(next: Msg[]) {
    const r = await api.assist(historyPayload(next), exprRef.current, modeRef.current)
    if (!busyRef.current) return
    onEventRef.current({
      type: 'done',
      reply: r.reply,
      expr: r.expr,
      combo: r.combo,
      valid: r.valid,
      errors: r.errors,
      attempts: r.attempts,
    })
  }

  async function send(text: string) {
    const t = text.trim()
    if (!t || busy) return
    const next: Msg[] = [...msgs, { role: 'user', content: t }, { role: 'assistant', content: '', streaming: true }]
    setMsgs(next)
    setInput('')
    busyRef.current = true
    setBusy(true)
    setErr('')
    try {
      const ws = await ensureWS()
      ws.send(JSON.stringify({
        type: 'chat',
        mode: modeRef.current,
        messages: historyPayload(next),
        expr: exprRef.current,
      }))
    } catch {
      wsRef.current?.close()
      wsRef.current = null
      try {
        await sendViaHTTP(next)
      } catch (e) {
        busyRef.current = false
        setErr((e as Error).message || '无法连接 AI 助手')
        setMsgs((prev) => prev.filter((m) => !(m.role === 'assistant' && m.streaming && !m.content)))
        setBusy(false)
      }
    }
  }

  function stop() {
    busyRef.current = false
    const ws = wsRef.current
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: 'stop' }))
    }
    setBusy(false)
    setMsgs((prev) => {
      const last = prev[prev.length - 1]
      if (last?.role === 'assistant' && last.streaming) {
        if (!last.content) return prev.slice(0, -1)
        return [...prev.slice(0, -1), { ...last, streaming: false }]
      }
      return prev
    })
  }

  function apply(m: Msg, i: number) {
    if (!m.expr || !onApply) return
    onApply(m.expr)
    setMsgs((prev) => prev.map((x, j) => (j === i ? { ...x, applied: true } : x)))
  }

  function copy(text: string) {
    navigator.clipboard?.writeText(text).catch(() => {})
  }

  return (
    <div className="ac">
      <header className="ac-head">
        <div className="ac-title">
          <span className="ac-logo"><AiIcon /></span>
          AI 助手
        </div>
        <div className="ac-head-actions">
          <button className="ac-icon" title="清空会话" onClick={() => { setMsgs([]); setErr('') }} disabled={busy || msgs.length === 0}>
            清空
          </button>
          <button className="ac-icon" title="收起面板" onClick={onCollapse}>›</button>
        </div>
      </header>

      <div className="ac-ctx">
        <button className={`ac-chip${showCtx ? ' on' : ''}`} onClick={() => setShowCtx((v) => !v)} title="查看随请求发送的编辑器内容">
          <span className="ac-at">@</span> {fileName}
        </button>
        {offline ? (
          <span className="tag amber" title="未配置模型，AI 助手不可用；点左下角「设置」配置后即刻启用">模型未配置</span>
        ) : (
          <span className="tag green">{displayLLM(llm)}</span>
        )}
      </div>
      {showCtx && <pre className="ac-ctx-preview">{expr || '(编辑器为空)'}</pre>}

      <div className="ac-scroll" ref={scrollRef}>
        {msgs.length === 0 && (
          <div className="ac-empty">
            <h4>{intro?.title ?? '描述想法，让 AI 写表达式'}</h4>
            <p className="subtle">
              {intro?.desc ??
                '可以直接说「做一个低波动反转因子」，也可以让它解释、改写或生成当前表达式的变体。产出会先经字段/算子校验，由你确认后才写入编辑器。'}
            </p>
            <div className="ac-suggest">
              {suggestions.map((s) => (
                <button key={s} onClick={() => send(s)} disabled={busy}>{s}</button>
              ))}
            </div>
            {offline && (
              <div className="ac-notice">
                模型未配置，AI 助手暂不可用。点左下角 <b>设置</b> 配置模型（Base URL / API Key / 模型名）并保存后即刻启用，无需重启服务。
              </div>
            )}
          </div>
        )}

        {msgs.map((m, i) => (
          <div key={i} className={`ac-msg ${m.role}`}>
            {m.role === 'user' ? (
              <div className="ac-bubble user">{m.content}</div>
            ) : (
              <div className="ac-bot">
                <div className="ac-bot-head"><span className="ac-logo sm"><AiIcon /></span> AI</div>
                <div className={`ac-bubble bot${m.streaming ? ' streaming' : ''}`}>{m.content}</div>
                {m.combo && (
                  <div className="ac-code-card">
                    <div className="ac-code-head">
                      <span className="ac-code-file">{m.combo.name || fileName}</span>
                      {m.valid ? <span className="tag green">✓ 已写入编辑器</span> : <span className="tag red">未通过校验</span>}
                    </div>
                    <div className="ac-combo-list">
                      <div className="subtle">{m.combo.method === 'manual' ? '自定义权重' : '等权'} · {m.combo.items.length} 个因子</div>
                      {m.combo.items.map((it) => (
                        <div className="ac-combo-row" key={it.id}>
                          <span>{it.name || `#${it.id}`}</span>
                          <span className="subtle">
                            {m.combo?.method === 'manual' ? `权重 ${it.weight} · ` : ''}
                            {it.direction < 0 ? '反向' : '正向'}
                          </span>
                        </div>
                      ))}
                    </div>
                    {m.errors && m.errors.length > 0 && (
                      <div className="ac-errors">{m.errors.join('\n')}</div>
                    )}
                    {m.applied && (
                      <div className="ac-code-foot">
                        <span className="subtle">成分已写入中间编辑器，可改权重后保存</span>
                      </div>
                    )}
                  </div>
                )}
                {m.expr && !m.combo && (
                  <div className="ac-code-card">
                    <div className="ac-code-head">
                      <span className="ac-code-file">{fileName}</span>
                      {m.valid ? <span className="tag green">✓ 校验通过</span> : <span className="tag red">未通过校验</span>}
                      <button className="ac-icon" title="复制表达式" onClick={() => copy(m.expr!)}>复制</button>
                    </div>
                    <pre className="ac-code">{m.expr}</pre>
                    {m.errors && m.errors.length > 0 && (
                      <div className="ac-errors">{m.errors.join('\n')}</div>
                    )}
                    {onApply && (
                      <div className="ac-code-foot">
                        <button className={`btn sm ${m.applied ? '' : 'primary'}`} onClick={() => apply(m, i)} disabled={m.applied}>
                          {m.applied ? '✓ 已应用到编辑器' : '应用到编辑器'}
                        </button>
                        <span className="subtle">应用前请自行判断逻辑</span>
                      </div>
                    )}
                  </div>
                )}
                {!m.combo && m.errors && m.errors.length > 0 && !m.expr && (
                  <div className="ac-errors">{m.errors.join('\n')}</div>
                )}
              </div>
            )}
          </div>
        ))}

        {busy && msgs[msgs.length - 1]?.streaming !== true && (
          <div className="ac-thinking">
            <span className="spinner" />
            {offline ? '生成中…' : 'AI 思考中…'}
          </div>
        )}
      </div>

      <div className="ac-composer">
        <div className="ac-quick">
          {quick.map((q) => (
            <button key={q} onClick={() => send(q)} disabled={busy}>{q}</button>
          ))}
        </div>
        <div className="ac-input-wrap">
          <textarea
            ref={inputRef}
            rows={3}
            value={input}
            placeholder={placeholder}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                send(input)
              }
            }}
          />
          <div className="ac-input-foot">
            <span>Ctrl+L 聚焦 · Enter 发送</span>
            {busy ? (
              <button className="btn sm" onClick={stop}>停止</button>
            ) : (
              <button className="btn primary sm" onClick={() => send(input)} disabled={!input.trim()}>
                发送 ↵
              </button>
            )}
          </div>
        </div>
        {err && <div className="note err mt8">{err}</div>}
      </div>
    </div>
  )
}
