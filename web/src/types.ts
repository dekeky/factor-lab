// 与后端 eval.Metrics / api.Factor / compose 对齐的类型定义

export interface Metrics {
  config: { Horizon: number; Groups: number; Cost: number; MinStocks: number }
  n_dates: number
  ic_mean: number
  ic_std: number
  ic_ir: number
  ic_t: number
  rank_ic_mean: number
  rank_ic_std: number
  rank_ic_ir: number
  rank_ic_t: number
  pic_pos: number
  pic_002: number
  coverage: number
  layer_mean: number[]
  layer_cum: number[]
  long_short_mean: number
  long_short_cum: number
  top_excess_mean: number
  top_excess_cum: number
  universe_mean: number
  turnover_daily: number
  cost_annual: number
  dates: string[]
  ic_by_date: number[]
  rank_ic_by_date: number[]
  layer_nav: number[][]
  top_nav: number[]
  top_nav_net: number[]
  universe_nav: number[]
}

/** 落盘指标摘要（列表 / 榜单用，不含逐日序列） */
export interface Summary {
  horizon: number
  n_dates: number
  ic_mean: number
  rank_ic_mean: number
  rank_ic_ir: number
  rank_ic_t: number
  pic_002: number
  coverage: number
  top_excess_cum: number
  long_short_cum: number
  turnover_daily: number
  cost_annual: number
  layer_mean: number[]
  layer_cum: number[]
  evaluated_at: string
  from: string
  to: string
}

export interface Factor {
  id: number
  name: string
  category: string
  expr: string
  source: string
  desc?: string
  created_at: string
  updated_at: string
  summary?: Summary
  available: boolean
  unavailable_reason?: string
}

export interface FactorInput {
  name: string
  expr: string
  category?: string
  desc?: string
  source?: string
}

export interface EvalOptions {
  horizon?: number
  groups?: number
  cost?: number
  start?: string
  end?: string
  universe?: string
}

export interface ComposeItem {
  id: number
  name?: string
  weight: number
  direction: number
}

export interface ComposeResult {
  expr: string
  label: string
  names: string[]
  correlation: number[][]
  warnings: string[]
  errors: string[]
  metrics?: Metrics
}

/** 因子组合卡片里的因子引用（available/missing 由后端运行期标记） */
export interface ComboItem {
  id: number
  name: string
  weight: number
  direction: number
  available?: boolean
  missing?: boolean
  reason?: string
}

/** 因子组合：多个因子按权重合成，卡片式管理，可反复评价 */
export interface Combo {
  id: number
  name: string
  method: 'equal' | 'manual'
  items: ComboItem[]
  desc?: string
  created_at: string
  updated_at: string
  summary?: Summary
}

export interface ImportResult {
  added: number
  updated: number
  skipped: number
  errors: string[]
  total: number
}

export interface BatchEvalResult {
  evaluated: number
  failed: number
  errors: string[]
  factors: Factor[]
}

export interface Candidate {
  title: string
  rationale: string
  expr: string
  valid: boolean
  errors?: string[]
  metrics?: Metrics
  attempts: number
}

/** AI 从现有因子库挑出的组合方案 */
export interface ComboProposal {
  name: string
  method: 'equal' | 'manual' | string
  items: ComboItem[]
}

/** 对话式辅助编写的一轮结果 */
export interface AssistReply {
  reply: string
  expr?: string
  combo?: ComboProposal
  valid: boolean
  errors?: string[]
  attempts: number
}

/** WebSocket 流式事件（/api/assist/ws） */
export interface AssistStreamEvent {
  type: 'delta' | 'retry' | 'done' | 'error'
  text?: string
  attempt?: number
  message?: string
  reply?: string
  expr?: string
  combo?: ComboProposal
  valid?: boolean
  errors?: string[]
  attempts?: number
}

export interface LoadReport {
  index?: string
  codes: number
  dates: number
  from: string
  to: string
  bars: number
  st_rows: number
  st_dropped: number
  roe_rows: number
  has_roe: boolean
  optional_errors?: string[]
}

export interface Health {
  status: string
  llm: string
  panel: { dates: number; stocks: number; fields: number }
  range: { from: string; to: string }
  data_source: string
  stock_pool?: string
  data_report?: LoadReport
  auth: boolean
}

export interface UniverseOption {
  code: string
  name: string
  group: string
}

export interface Meta {
  fields: string[]
  operators: { name: string; min_args: number; max_args: number }[]
  eval_default: { Horizon: number; Groups: number; Cost: number; MinStocks: number }
  range: { from: string; to: string }
  universes?: UniverseOption[]
  universe_switchable?: boolean
  stock_pool?: string
}

export interface HelpDoc {
  dsl: {
    statement_separators: string[]
    comment: string
    result: string
    precedence: string[]
    max_window: number
    max_depth: number
  }
  fields: { name: string; description: string; unit?: string; tip?: string }[]
  operators: { name: string; category: string; signature: string; description: string; example: string; tip?: string }[]
  builtins: { name: string; category: string; expr: string; description: string }[]
}

/** 会话状态（GET/POST/DELETE /api/session）；令牌存于 HttpOnly Cookie，前端不持有 */
export interface SessionState {
  required: boolean
  authed: boolean
  message?: string
}

/** 一条模型配置（GET 返回，密钥永远是掩码） */
export interface ModelProfile {
  id: string
  name: string
  base_url: string
  model: string
  api_key_masked: string
  has_key: boolean
}

/** 配置（GET /api/settings 返回，密钥永远是掩码） */
export interface ModelSettings {
  base_url: string
  model: string
  temperature: number
  timeout_sec: number
  api_key_masked: string
  has_key: boolean
  tushare_token_masked: string
  has_tushare_token: boolean
  engine: string
  online: boolean
  persisted: boolean
  reload_error?: string
  active_id: string
  models: ModelProfile[]
}

export interface ModelProfileInput {
  id: string
  name: string
  base_url: string
  model: string
  api_key?: string
  clear_key?: boolean
}

/** 保存/测试配置的请求体（密钥留空 = 沿用已保存的） */
export interface ModelSettingsInput {
  base_url: string
  model: string
  temperature: number
  timeout_sec: number
  api_key?: string
  clear_key?: boolean
  tushare_token?: string
  clear_tushare_token?: boolean
  active_id: string
  models: ModelProfileInput[]
}

/** 连通性测试结果 */
export interface SettingsTestResult {
  ok: boolean
  engine: string
  latency_ms: number
  reply: string
}

export interface PaperConfig {
  top_n: number
  rebalance_threshold?: number
  capital: number
  cost: number
  slippage_bps?: number
  start: string
}

export interface KLineBar {
  time: string
  open: number
  high: number
  low: number
  close: number
  volume: number
}

export interface KLine {
  code: string
  name?: string
  label: string
  bars: KLineBar[]
}

export interface PaperPosition {
  code: string
  name?: string
  label?: string
  shares: number
  cost?: number
  price: number
  value: number
  weight: number
  pnl?: number
  realized?: number
}

export interface PaperPick {
  code: string
  name?: string
  label?: string
  score: number
  rank: number
  status: string
}

export interface PaperIntent {
  code: string
  side: string
  buy_shares: number
  sell_shares: number
  actual_buy_shares: number
  actual_sell_shares: number
  reason?: string
  score: number
  rank: number
}

export interface PaperPlan {
  id: number
  account_id: number
  signal_date: string
  exec_at: string
  intents: PaperIntent[]
  status: string
}

export interface PaperSnapshot {
  account_id: number
  date: string
  nav: number
  cash: number
  ret: number
  holdings: PaperPosition[]
}

export interface PaperFill {
  id?: number
  account_id: number
  plan_id?: number
  date: string
  code: string
  name?: string
  label?: string
  side: string
  shares: number
  price: number
  amount: number
  fee: number
  slippage?: number
  status: string
}

export interface PaperBook {
  account_id: number
  cash: number
  positions: PaperPosition[]
  last_settled?: string
}

export interface PaperAccount {
  id: number
  name: string
  factor_id?: number
  factor_name?: string
  combo_id?: number
  combo_name?: string
  expr: string
  config: PaperConfig
  created_at: string
  updated_at: string
}

export interface Paper {
  account: PaperAccount
  book: PaperBook
  plans: PaperPlan[]
  snapshots: PaperSnapshot[]
  fills?: PaperFill[]
}
