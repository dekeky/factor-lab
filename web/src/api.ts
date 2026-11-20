import type {
  AssistReply,
  BatchEvalResult,
  Combo,
  ComboItem,
  ComposeItem,
  ComposeResult,
  EvalOptions,
  Factor,
  FactorInput,
  Health,
  HelpDoc,
  ImportResult,
  Meta,
  Metrics,
  ModelSettings,
  ModelSettingsInput,
  KLine,
  PaperAccount,
  SettingsTestResult,
  SessionState,
} from './types'

const BASE = ''

// 会话：访问令牌只在登录时提交一次，之后由服务端下发的 HttpOnly Cookie 维持。
// 前端不保存令牌（不放 localStorage、不拼进 URL），令牌无法被脚本读取。
let onUnauthorized: (() => void) | null = null
export function setUnauthorizedHandler(fn: (() => void) | null) {
  onUnauthorized = fn
}

// 网络层失败给出可操作提示，而不是裸 "Failed to fetch"
async function doFetch(path: string, init?: RequestInit): Promise<Response> {
  let res: Response
  try {
    res = await fetch(BASE + path, { credentials: 'same-origin', ...init })
  } catch {
    throw new Error('无法连接后端服务：请确认 factor-lab 已启动')
  }
  if (res.status === 401) onUnauthorized?.()
  return res
}

async function parse<T>(res: Response): Promise<T> {
  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    const d = data as { errors?: string[]; error?: string }
    throw new Error(d.errors?.join('\n') ?? d.error ?? `请求失败 (${res.status})`)
  }
  return data as T
}

async function get<T>(path: string): Promise<T> {
  return parse<T>(await doFetch(path))
}

async function send<T>(method: string, path: string, body?: unknown): Promise<T> {
  return parse<T>(
    await doFetch(path, {
      method,
      headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    }),
  )
}

export const api = {
  // 会话：required=true 表示服务端启用了访问令牌，需要先登录
  session: () => get<SessionState>('/api/session'),
  login: (token: string) => send<SessionState>('POST', '/api/session', { token }),
  logout: () => send<SessionState>('DELETE', '/api/session'),

  health: () => get<Health>('/api/health'),
  meta: () => get<Meta>('/api/meta'),
  help: () => get<HelpDoc>('/api/help'),
  check: (expr: string) => send<{ ok: boolean; errors: string[] | null }>('POST', '/api/check', { expr }),
  evaluate: (expr: string, opts: EvalOptions = {}) => send<Metrics>('POST', '/api/evaluate', { expr, ...opts }),

  factors: () => get<Factor[]>('/api/factors'),
  factor: (id: number) => get<Factor>(`/api/factors/${id}`),
  createFactor: (f: FactorInput) => send<Factor>('POST', '/api/factors', f),
  updateFactor: (id: number, f: FactorInput) => send<Factor>('PUT', `/api/factors/${id}`, f),
  deleteFactor: async (id: number) => {
    await send<{ deleted: boolean }>('DELETE', `/api/factors/${id}`)
  },
  copyFactor: (id: number) => send<Factor>('POST', `/api/factors/${id}/copy`),
  batchEvaluate: (ids: number[], opts: EvalOptions = {}) =>
    send<BatchEvalResult>('POST', '/api/factors/batch-evaluate', { ids, ...opts }),
  importFactors: (payload: unknown) => send<ImportResult>('POST', '/api/factors/import', payload),
  // 下载走浏览器直连（Cookie 会自动带上），无需把令牌拼进 URL
  exportUrl: () => '/api/factors/export',

  compose: (items: ComposeItem[], method: 'equal' | 'manual', opts: EvalOptions = {}) =>
    send<ComposeResult>('POST', '/api/compose', { items, method, ...opts }),

  // 因子组合（卡片式管理）
  combos: () => get<Combo[]>('/api/combos'),
  createCombo: (c: { name: string; method: 'equal' | 'manual'; items: ComboItem[]; desc?: string }) =>
    send<Combo>('POST', '/api/combos', c),
  updateCombo: (id: number, c: { name: string; method: 'equal' | 'manual'; items: ComboItem[]; desc?: string }) =>
    send<Combo>('PUT', `/api/combos/${id}`, c),
  deleteCombo: async (id: number) => {
    await send<{ deleted: boolean }>('DELETE', `/api/combos/${id}`)
  },
  evaluateCombo: (id: number, opts: EvalOptions = {}) =>
    send<ComposeResult>('POST', `/api/combos/${id}/evaluate`, opts),

  kline: (code: string) => get<KLine>(`/api/kline?code=${encodeURIComponent(code)}`),

  papers: () => get<PaperAccount[]>('/api/paper'),
  paper: (id: number) => get<PaperAccount>(`/api/paper/${id}`),
  createPaper: (body: {
    name?: string
    factor_id?: number
    combo_id?: number
    top_n: number
    buffer?: number
    capital: number
    cost?: number
    start?: string
  }) => send<PaperAccount>('POST', '/api/paper', body),
  deletePaper: async (id: number) => {
    await send<{ deleted: boolean }>('DELETE', `/api/paper/${id}`)
  },
  syncPaper: (id: number) => send<PaperAccount>('POST', `/api/paper/${id}/sync`),
  syncAllPaper: () => send<{ synced: number; failed: number; accounts: PaperAccount[] }>('POST', '/api/paper/sync'),

  assist: (messages: { role: string; content: string }[], expr: string, mode?: 'factor' | 'combo') =>
    send<AssistReply>('POST', '/api/assist', { messages, expr, mode }),

  /** 浏览器同源 WebSocket，会话 Cookie 会自动带上。 */
  assistWSUrl: () => {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    return `${proto}//${location.host}/api/assist/ws`
  },

  // 模型配置（左侧「设置」）
  settings: () => get<ModelSettings>('/api/settings'),
  saveSettings: (s: ModelSettingsInput) => send<ModelSettings>('PUT', '/api/settings', s),
  testSettings: (s: ModelSettingsInput) => send<SettingsTestResult>('POST', '/api/settings/test', s),
}
