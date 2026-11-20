# factor-lab

面向 C 端的 AI 因子研究平台（Go + Eino + React）。技术方案见上层目录《AI 因子研究平台MVP技术方案.md》。

> **数据真实性**：从 [Tushare Pro](https://tushare.pro) 拉取真实 A 股数据（也可 `-data csv` 导入自有面板）。生产就绪检查见 [docs/PRODUCTION.md](docs/PRODUCTION.md)。

## 快速开始

**生产模式**（需 [Tushare token](https://tushare.pro/user/token)，三种方式任选）：

1. 界面左下角 **设置** → Tushare Token（写入 `data/settings.json`）
2. 环境变量 `TUSHARE_TOKEN`
3. 启动参数 `-tushare-token`

优先级：命令行 > 环境变量 > 设置文件。未配置 token 时服务仍会启动，打开设置填好保存后会自动拉数。

```bat
start.bat
```

或手动：

```bash
go build -o factor-lab.exe ./cmd/server
./factor-lab.exe -data tushare -index 000300.SH -start 20180101 -api-token "$FACTOR_LAB_TOKEN"
```

其他数据源：CSV 长表 `-data csv -csv panel.csv`。

启用 LLM（OpenAI 协议兼容端点，如 DeepSeek）：

```bash
export LLM_BASE_URL=https://api.deepseek.com/v1
export LLM_API_KEY=sk-...
export LLM_MODEL=deepseek-chat
```

也可以在界面左下角 **设置** 里配置（写入 `data/settings.json`，保存即热生效）。未配置时 AI 助手会明确提示去设置里配置，不做任何模板降级。

## 界面结构

| 页面 | 用途 |
|---|---|
| 因子工厂 | DSL 编辑器（实时校验、字段/算子点选插入）+ **AI 助手对话式编写**（自然语言 → 表达式，引擎校验后一键应用）+ 评价参数（区间/持有期/分层/成本）+ 结果可视化 |
| 因子库 | 搜索/筛选/排序、批量评价、编辑抽屉、复制、删除、导入导出 |
| 因子组合 | 从因子库已有因子加权合成（三栏 IDE：我的组合 / 组合编辑器 / AI 助手）；AI 只挑现有因子，方案直接写入编辑器；标准化、两两相关性热力矩阵（>0.7 同质化告警）、组合保存与反复评价 |
| 实盘模拟 | 冻结因子开纸上账户：每日打分、Top N、可配置调仓阈值（跌出 Top N+阈值才换股）、T+1 前复权开盘成交、整手 100 股；停牌/涨跌停从股票池剔除；展示名称（代码）、买入价、现价与累计盈利；点击股票名打开 TradingView 日 K |

## 后端模块

| 模块 | 位置 | 说明 |
|---|---|---|
| DSL 引擎 | `internal/dsl` | 词法 → 语法 → AST → 静态校验（算子白名单/常数位/字段与变量引用） |
| 算子库 | `internal/ops` | `ts_*` 时序 11 个、`cs_*` 截面 4 个、数学/逻辑 10 个；NaN 传播统一 |
| 执行器 | `internal/engine` | AST 在 Panel 上的编译执行：`Run` / `Check` |
| 评价引擎 | `internal/eval` | T+1 执行的前向收益对齐、IC/RankIC/ICIR/t、P(IC>0.02)、分层、多头净超额、换手成本（PandaAI 口径）；退化截面不污染均值 |
| 纸上账户 | `internal/paper` | 冻结因子逐日打分、Top N、调仓阈值、T+1 前复权开盘成交、整手 100 股；停牌/涨跌停剔除；账本只追加 |
| 组合引擎 | `internal/compose` | 表达式拆分 + 变量隔离重命名 + cs_zscore 标准化 + 加权合成；平均截面秩相关（同质化检查） |
| 数据源 | `internal/data` | `tushare`（真实 A 股：后复权 / PIT 财务 / ST 剔除 / 分股票并发 / 字段强校验）、`csv` |
| AI Agent | `internal/agent` | Eino（OpenAI 协议）对话式辅助编写（`RunAssist`，人机协作）；模型配置热更新（`settings.go`）；提议/裁决分离——LLM 产出必须过引擎校验，未配置模型时明确报错 |
| 因子库 | `internal/api` + `data/factors.json` | 全量 CRUD、批量评价（并发 4）、导入导出、指标摘要落盘、原子写入、因子可用性检查 |
| 前端 | `web/` | Vite + React + TS + ECharts；自绘下拉与日历（不用原生弹层） |

## 数据源要求（生产）

`internal/data/tushare.go` 通过 [tushare-go-sdk](https://github.com/dekeky/tushare-go-sdk) 直连 Tushare Pro（`https://api.tushare.pro`）。token 来自 `-tushare-token`、环境变量 `TUSHARE_TOKEN`，或界面「设置」写入的 `data/settings.json`（字段 `tushare_token`，接口只回掩码）。`go.mod` 用 `replace` 指向 `third_party/tushare-go-sdk`（与上游一致的本地副本）；GitHub 可达时也可 `go get github.com/dekeky/tushare-go-sdk@latest` 后去掉 replace。

| 数据 | 接口 | 用途 |
|---|---|---|
| 指数成分 | `index_weight` | 股票池（取最近一期权重，`-topn 0` = 全部成分；`000300.SH` 无数据时回退 `399300.SZ`） |
| 日 K | `daily` | 量价字段 |
| 复权因子 | `adj_factor` | 后复权价，避免除权假信号 |
| 每日指标 | `daily_basic` | 换手率、PE_TTM、PB、市值 |
| 财务指标 | `fina_indicator` | ROE（按 `ann_date` 做 PIT 展开） |
| ST 名单 | `stock_st` | 按日剔除 ST |
| 停牌 | `suspend_d` | 纸上账户当日剔除 |
| 涨跌停价 | `stk_limit` | 纸上账户买/卖约束 |
| 证券简称 | `stock_basic` | 纸上账户「名称（代码）」展示 |

客户端侧按 `-tushare-qps`（默认 4）限流，按 `-parallel`（默认 4）分股票并发；可选接口（估值 / ROE / ST）失败不阻断启动，但会记入数据质量摘要，相关因子不可用。

启动时校验 `open/high/low/close/vol/amount` 齐全，缺失即失败而不是静默降级。

## 因子库

启动时把 `internal/factors/classic.go` 的 **46 个经典因子**播种到 `data/factors.json`（只播种一次，记录在 `.seeded`）。播种后它们与用户因子完全平等：可编辑、可删除、可导出、可重新评价。

| 分类 | 数量 | 内容 |
|---|---|---|
| alpha101 | 20 | WorldQuant 101 Alphas 可迁移子集，`Desc` 保留论文原式 |
| alpha158 | 7 | Qlib 风格：K 线形态、RSV、乖离率、成交额波动 |
| momentum / reversal | 6 | 多周期动量、短期/中期反转、隔夜跳空 |
| volatility | 4 | 低波异象、波动率期限结构、低振幅、价格位置 |
| volume | 4 | 低换手溢价、换手放大反转、量能异动、量价相关 |
| valuation / quality / size | 5 | E/P、B/P、ROE、稳定 ROE、小市值 |

## DSL 语法

```text
# 中间变量 = 赋值；最后一行是因子值
mom20 = close / ts_delay(close, 20) - 1
alpha = -1 * cs_rank(mom20)

# 条件
down  = if(pct_chg < 0, ts_std_dev(pct_chg, 20), close)
alpha = cs_rank(ts_arg_max(down * down, 5)) - 0.5
```

- `ts_` 前缀：沿单只股票时间轴滚动，**窗口必须显式给出**（1~512 整数）
- `cs_` 前缀：每个交易日横截面上计算
- 字段：`open/high/low/close/pct_chg/vol/amount/turnover_rate/pe_ttm/pb/total_mv/roe`

## API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/health` `/api/meta` `/api/stats` | 状态（含数据源/数据质量）/ 字段与算子 / 汇总统计 |
| POST | `/api/check` | DSL 静态校验 |
| POST | `/api/evaluate` | `{expr, horizon, groups, cost, start, end}` → 完整指标 |
| POST | `/api/compose` | `{items:[{id,weight,direction}], method, ...}` → 合成表达式 + 指标 + 相关矩阵 |
| GET/POST | `/api/factors` | 列表（含 `available`）/ 新建 |
| GET/PUT/DELETE | `/api/factors/{id}` | 详情 / 更新 / 删除 |
| POST | `/api/factors/{id}/copy` | 复制 |
| POST | `/api/factors/batch-evaluate` | `{ids?:[], ...}` 空 ids = 全部；自动跳过当前数据源不支持的因子 |
| GET/POST | `/api/factors/export` `/api/factors/import` | 导出 / 导入（同名按表达式变更决定是否作废旧指标） |
| POST | `/api/assist` | `{messages, expr, mode?}` → 一次性辅助结果。`mode=combo` 时从因子库挑组合（不写新 DSL）；界面走 WebSocket |
| GET | `/api/assist/ws` | AI 助手流式对话：客户端发 `{type:chat,mode?,messages,expr}` / `{type:stop}`，服务端推 `delta` / `retry` / `done`（含 `combo`） / `error` |
| GET/POST/DELETE | `/api/session` | 登录状态 / 登录（下发 HttpOnly Cookie）/ 退出 |
| GET/PUT | `/api/settings` | 模型配置（密钥只回掩码）/ 保存并热生效 |
| POST | `/api/settings/test` | 连通性测试（返回引擎、延迟、样例回复） |
| GET/POST | `/api/combos` | 组合列表 / 新建 |
| PUT/DELETE | `/api/combos/{id}` | 更新 / 删除 |
| POST | `/api/combos/{id}/evaluate` | 组合评价（摘要写回列表） |
| GET/POST | `/api/paper` | 实盘账户列表 / 新建（`{factor_id, top_n, capital, cost, start}`） |
| GET/DELETE | `/api/paper/{id}` | 账户详情（含逐日名单与成交，GET 时自动补齐新交易日） |
| POST | `/api/paper/{id}/sync` `/api/paper/sync` | 同步单个 / 全部账户 |
| GET | `/api/kline?code=` | 个股前复权日 K（open/high/low/close/volume） |
| POST | `/api/admin/reload` | 热加载数据面板（需 token 或本机调用） |

配置了 `-api-token`（或环境变量 `FACTOR_LAB_TOKEN`）时，除 `/api/health`、`/api/session` 外所有接口需要鉴权：浏览器登录后靠 **HttpOnly Cookie**（`fl_session`，12 小时，SameSite=Strict）自动携带；脚本/curl 可用 `X-API-Token` 头。令牌不落 localStorage、不进 URL。对外监听（非回环地址）未配令牌会拒绝启动。

## 评价口径（默认，PandaAI 赛制对齐）

- 信号日 t 用 t 及以前数据；**t+1 收盘执行**，持有 `Horizon`（默认 1）个交易日
- 分层 10 组（因子升序，Q10 = 因子最大）；多头 = 最高分位等权
- 单边成本 0.3%（换手折算年化成本单独展示）；基准 = 全市场等权
- **以多头净超额为主结论口径**（A 股个人无空头腿）；多空仅作研究参考
- 退化截面（当日因子值或收益全相同）跳过 IC 统计，但仍推进净值

## 测试

```bash
go test ./...   # DSL 解析/校验、算子语义（NaN 边界）、Alpha101 端到端、
                # 合成数据已知规律 IC、退化截面健壮性、组合拆分与相关性、
                # 内置因子 46 条全量校验 + 执行冒烟、AI 助手 JSON 提取
```

## 目录

```text
factor-lab/
├── cmd/server/              # 入口
├── internal/
│   ├── dsl/ ops/ engine/    # 语言与计算
│   ├── eval/                # 评价引擎（冻结裁判）
│   ├── paper/               # 纸上账户（实盘模拟）
│   ├── compose/             # 因子组合与相关性
│   ├── data/                # 数据源适配
│   ├── factors/             # 内置经典因子（播种数据）
│   ├── agent/               # Eino 模型接入 + 对话式辅助
│   └── api/                 # HTTP 接口
├── web/                     # React 前端
└── data/factors.json        # 因子库（gitignore）
```

## 开发工具：界面截图回归

无额外依赖（Node 18+ 与本机 Chrome），用 CDP 驱动交互并截图，便于检查弹窗/抽屉/布局：

```bash
node scripts/shot.mjs            # 抽屉、下拉、日历、各页面、1024 窄屏
node scripts/verify-ui.mjs       # 交互验证：下拉回填表达式、日历回填日期、评价链路
```

环境变量：`CHROME_PATH` 指定浏览器，`BASE_URL` 指定服务地址。

## 界面实现约定

- **DSL 编辑器是自研组件**（`components/CodeEditor.tsx`）：「透明 textarea + 高亮层」双层结构（字体度量完全一致、滚动同步），带行号、语法高亮（算子紫 / 字段青 / 数字蓝 / 注释灰 / 关键字琥珀）、状态栏（行:列 / 字符数 / 校验状态）、Tab 缩进与 Ctrl+Enter 评价。零第三方依赖。点选字段/算子通过 `insert()` 在光标处插入。
- **不使用原生 `<select>` / `<input type="date">`**：它们的弹层由浏览器绘制，位置不受页面控制（滚动容器、sticky 祖先、页面缩放下会错位）。统一用 `components/Select.tsx`、`components/DatePicker.tsx`，基于 `components/Popover.tsx`（fixed 定位、按可用空间自动向上/向下展开、滚动/Esc/外部点击关闭）。
- 弹层**必须首帧就 `position: fixed`**：否则插入 flex 容器会撑高整行，锚点被推走，随后测算的位置就是错的。
- 图表用 `components/Chart.tsx`，容器高度由 `.chart` 类提供（默认 320px），并用 `ResizeObserver` 自适应重绘；时序图一律 time 轴（category 轴在上千交易日下标签会挤成一团）。
- 评价结果区用 Tab 组织（分层回测 / 分层净值 / IC 时序 / 多头 vs 全市场），避免整页无限下滚。
- 入口 `index.html` 走 `no-cache`，`/assets/*` 走长缓存（文件名带内容哈希），避免新旧产物错配白屏。
