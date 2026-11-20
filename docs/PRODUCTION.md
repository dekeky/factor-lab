# 生产就绪检查报告

> 本次检查针对"能否用于生产部署"这一目标，逐项核对数据真实性、故障处理、安全与可运维性。
> 结论：**数据链路已从"模拟"切换为"真实"，并于 2026-11-01 完成真实数据联调（见 §5）**；工程侧补齐 13 项缺陷 + 联调修正 3 项契约问题。产品侧仍有 5 项必须补齐才能对外服务（见 §4）。

## 1. 数据真实性（本轮核心变更）

| 项 | 变更前 | 变更后 |
|---|---|---|
| 默认数据源 | `synthetic`（模拟数据，无任何提示） | `tushare`（真实 A 股，token 缺失或接口失败则**启动失败**，绝不降级） |
| 模拟数据可用性 | 默认可用 | 必须显式加 `-allow-synthetic`，否则拒绝启动 |
| 界面标识 | 无 | 顶栏橙色「模拟数据」徽章 + 顶部警告横幅（禁止用于生产决策） |
| 健康检查 | 仅面板维度 | 增加 `data_source` / `data_report`（数据质量摘要） |

服务入口已移除 `-data synthetic` / `-allow-synthetic` 与界面模拟数据标注；单元测试仍可使用内存面板。

当前启动行为：

```
① ./factor-lab -data synthetic            → 拒绝启动（未知数据源，仅 tushare | csv）
② ./factor-lab（默认 tushare，未设 TUSHARE_TOKEN） → 启动界面，提示在「设置」填写 token
```

## 2. 数据源适配器（`internal/data/tushare.go`）

生产数据全部经 [tushare-go-sdk](https://github.com/dekeky/tushare-go-sdk) 从 Tushare Pro 获取：

| 能力 | 实现 |
|---|---|
| **PIT 财务** | `fina_indicator` 取 `ann_date + roe`，按公告日 as-of 展开到每个交易日——3 月 10 日只用当时已公告的报告期数据，杜绝前视偏差 |
| **ST 剔除** | `stock_st` 按 `(ts_code, trade_date)` 剔除，ST 当日该股因子值与收益均为空，不进截面 |
| **后复权** | `daily × (adj/adj_base)`，基准取区间首个交易日；`pct_chg` 由后复权价重算，除权日不再出现假暴跌 |
| **字段完整性校验** | `open/high/low/close/vol/amount` 任一字段全空即启动失败，并指出缺哪个字段 |
| **限流 + 并发** | 客户端 `-tushare-qps`（默认 4）令牌桶，`-parallel`（默认 4）分股票并发；SDK 对 5xx / 频次限制自动重试 |
| **数据质量摘要** | 记录 bars/ST 剔除数/ROE 记录数，随健康检查暴露，可选数据集缺失时记录 warning |

契约测试（`internal/data/tushare_test.go`，httptest 模拟 Tushare JSON 协议）覆盖：后复权换算、PIT 只取已公告期、ST 当日剔除、换手字段、pct_chg 重算、缺字段报错、空成分报错、沪深300 代码回退。

## 3. 本次发现并修复的工程缺陷

| # | 问题 | 影响 | 修复 |
|---|---|---|---|
| 1 | 默认使用模拟数据 | 生产误用，决策依据错误 | 默认 tushare + 显式许可 + 界面标注 |
| 2 | 真实数据缺 ROE | 4 个基本面因子失效 | PIT 拉取 fina_indicator |
| 3 | 未剔除 ST | 涨跌幅污染 IC，指标虚高 | 按日剔除 |
| 4 | 单次请求取全部股票 | 大数据量下超时/截断 | 分批 + 并发 + 截断检测 |
| 5 | 无字段校验 | 数据源缺陷时"静默出错" | 启动期强校验，缺字段 fatal |
| 6 | 因子库落盘非原子 | 崩溃时 JSON 损坏（已真实发生过一次） | 临时文件 + rename 原子替换 |
| 7 | 无 panic 恢复 | 单个请求可打挂整个进程 | recover 中间件 + 堆栈日志 |
| 8 | 无访问日志 | 线上问题无法定位 | 结构化 JSON 日志 + request_id（method/path/status/ms/ip） |
| 9 | 无 HTTP 超时 | 慢请求堆积耗尽连接 | Read/Write/Idle 超时 + MaxHeaderBytes |
| 10 | 无优雅关闭 | 重启丢弃进行中的请求 | signal + Shutdown(20s) |
| 11 | 数据更新需重启进程 | 每日行情更新要停机 | `POST /api/admin/reload` 原子替换面板（读请求不中断） |
| 12 | API 裸奔 | 任意人可改因子库 | `-api-token` + 登录页换 HttpOnly Cookie 会话（`/api/session`）；脚本可用 `X-API-Token` 头；health/session 放行，对外监听无令牌拒绝启动 |
| 13 | 因子不可用无提示 | 换数据源后静默失败 | 因子级 `available/unavailable_reason`，前端标红 |
| 14 | 无安全响应头 | 点击劫持/MIME 嗅探/XSS 放大 | CSP、nosniff、SAMEORIGIN、Referrer-Policy、API `no-store` |
| 15 | CORS 通配 `*` | 任意站点可携带凭据调接口 | 默认同源不下发 CORS 头，需跨域时用 `-cors-origin` 显式列出来源 |
| 16 | 登录可被爆破 | 令牌被在线枚举 | 同 IP 1 分钟内失败 5 次锁定（进程内计数） |

其余已具备：批量评价并发限制（4）、goroutine 无泄漏、服务端无阻塞式锁内计算、前端静态资源长缓存/入口 no-cache。

## 4. 仍缺失（对外服务前必须决策）

| 优先级 | 缺口 | 风险 | 建议方案 |
|---|---|---|---|
| **P0** | **无用户体系与租户隔离** | C 端用户互相可见对方因子与研究会话，无法计费 | 接入登录（手机号/微信）+ 因子表加 `owner_id` + 查询强制过滤 |
| **P0** | **真实数据未联调** | 适配器按契约实现但未跑过真实 Tushare token | 配置 `TUSHARE_TOKEN` 后跑一次全量加载 |
| P1 | 因子库为单机 JSON 文件 | 多实例部署会互相覆盖 | 迁移到 PostgreSQL（或 ClickHouse），加乐观锁 |
| P1 | 业务接口无速率限制 | 单用户刷批量评价可打满 CPU（登录接口已限流） | 按 token/IP 限流 + 评价任务排队 |
| P1 | 无监控与告警 | 故障只能靠用户反馈 | 暴露 `/metrics`（QPS、延迟、评价耗时、LLM 调用与花费） |
| P1 | 数据不自动刷新 | 行情更新需人工调 reload | 定时任务（每日收盘后）+ 加载失败告警 |
| P2 | 无审计日志 | 无法追溯谁改了哪个因子 | 因子变更写入审计表 |
| P2 | 无结果缓存 | 同一表达式重复评价重复计算 | 以（表达式哈希 + 面板版本 + 配置）为键缓存 |
| P2 | LLM 成本不可控 | AI 助手无配额、无成本统计 | 按用户配额 + 调用日志与 token 成本统计 |
| P3 | 无 HTTPS | 明文传输 | 由 Nginx/网关终止 TLS |

## 5. 真实数据联调结果（2026-11-01，已完成）

数据源（当时为 finclaw-data 远程服务，现已改为 Tushare Pro）：指数 `000300.SH` 全成分。

### 5.1 联调时发现并修正的 3 个契约问题

| 问题 | 现象 | 修正 |
|---|---|---|
| 成分字段名不符 | 真实返回 `code`，适配器读 `con_code` → 报"指数没有成分数据" | 两者兼容，优先 `code` |
| **`fields` 参数会裁掉主键** | `/v1/stock_factor_pro`、`/v1/fina_indicator` 只返回请求的列；未请求 `code/trade_date` 时响应里没有主键，估值/换手/ROE 全部无法落位（且**静默丢弃**） | 请求时显式带上 `code,trade_date` / `code,ann_date` |
| ROE 单位 | 真实值为百分数（`7.074` = 7.07%），合成数据为小数（0.07） | 记录口径差异；排序类因子不受影响，比值类需注意 |

这三项都是"不联调发现不了、上线即静默出错"的问题——也说明此前"仅按契约实现"不足以保证可用。

### 5.2 加载与计算性能（实测）

| 规模 | 耗时 | 备注 |
|---|---|---|
| 50 只 × 683 日（3.4 万 K 线） | 19.3s | 分批 80 只 / 4 并发 |
| **300 只 × 1653 日（47.6 万 K 线）** | **2m42s** | 含 6 个数据集、36,820 条 ROE |
| 46 因子全库评价（300 股 × 1653 日） | **2.2s** | 4 并发 |

数据质量：ST 剔除 1,355 条（真实市场有 ST），ROE 记录 36,820 条（PIT 生效）。

### 5.3 因子结果（真实 vs 模拟）

| 因子 | 模拟数据 RankIC / t | 真实数据 RankIC / t | 解读 |
|---|---|---|---|
| `rev5`（5 日反转） | 0.0834 / 33.0 | **0.0104 / 2.0** | 日频反转信号弱，模拟数据的高 IC 是人为注入的 |
| `mom10`（10 日动量） | -0.0586 / -21.7 | -0.0077 / -1.4 | 同上，方向一致但强度骤降 |
| `low_amplitude20` | — | **0.0316 / 4.6** | 低振幅（低波）异象，A 股经典有效因子 |
| `low_vol20` | — | **0.0297 / 4.4** | 低波异象 |
| `low_turn20` | — | **0.0290 / 4.3** | 低换手溢价 |
| `bp` / `ep` | — | 0.0224 / 3.4、0.0195 / 2.9 | 价值因子，方向与学术结论一致 |
| `roe_rank` | — | -0.0017 / -0.5 | 沪深300（大盘股）内质量因子几乎无区分度 |

全库：平均 |RankIC| = **0.0102**，|t|>2 的因子 23/47（样本 1653 天，t 值会偏大，需结合 ICIR 看；ICIR 普遍仅 0.05~0.13）。

**一个必须记录的分析陷阱**：多个因子出现「RankIC 为正、但多头累计超额为负」（如 `low_amplitude20` IC 0.0316、超额 -65.5%），也有反向极端（`small_size` IC 0.0011、超额 +1545%）。原因是 300 只股票分 10 层每层仅 30 只，累计超额极易被少数极端个股主导。**结论：分层累计超额不能单独作为因子有效性的判据**，需配合行业中性化、分组稳健性与多空拆分来看。

### 5.4 结论

真实数据链路已跑通，适配器的后复权 / PIT / ST 剔除均在真实数据上生效。数据侧可以进入生产；产品侧缺口见 §4（用户体系为 P0）。

---

## 6. 生产部署

前置：已申请 Tushare Pro token（https://tushare.pro/user/token），并具备 `index_weight` / `daily` / `adj_factor` 等接口积分。

```bash
# 编译
go build -o factor-lab.exe ./cmd/server
npm --prefix web ci && npm --prefix web run build

# 启动（真实数据，Tushare Pro）
export TUSHARE_TOKEN=***                # 不要写进脚本或仓库
./factor-lab.exe \
  -addr 127.0.0.1:8080 \
  -data tushare -index 000300.SH -topn 0 -start 20200101 \
  -store data/factors.json -web web/dist \
  -api-token "$FACTOR_LAB_TOKEN" \
  -cors-origin ""                     # 默认同源；跨域前端才填来源，逗号分隔
```

Windows 双击 `start.bat` 即上述生产模式（token 也可事后在界面「设置」里填写）。

环境变量：`TUSHARE_TOKEN`（Tushare Pro token，也可写在 `data/settings.json` 的 `tushare_token`）、`FACTOR_LAB_TOKEN`（本服务令牌）、`LLM_BASE_URL`/`LLM_API_KEY`/`LLM_MODEL`。
`start.bat` 支持覆盖：`TUSHARE_INDEX` / `TUSHARE_START` / `TUSHARE_TOPN`。

运维接口：

```bash
curl -s http://127.0.0.1:8080/api/health          # 数据源、面板、数据质量
curl -X POST -H "X-API-Token: $TOKEN" \
  http://127.0.0.1:8080/api/admin/reload           # 行情更新后热加载，无需重启
```

浏览器登录（令牌换 HttpOnly Cookie，前端不存令牌）：

```bash
curl -c ck.txt -X POST -H "Content-Type: application/json" \
  -d '{"token":"'"$TOKEN"'"}' http://127.0.0.1:8080/api/session
curl -b ck.txt -s http://127.0.0.1:8080/api/factors | head -c 120
```

鉴权回归脚本：`node scripts/verify-auth.mjs http://127.0.0.1:8080 "$FACTOR_LAB_TOKEN"`（登录门/错误令牌/HttpOnly/刷新保持/退出）。

## 7. 仍未覆盖（下一阶段）

- **股票池仅沪深300**：全 A 股（约 5000 只）下的加载耗时与内存占用未测；按 300 只 2m42s 线性外推约 20-40 分钟，需改并行度或做增量加载。
- **未做行业/市值中性化**：`cs_*` 算子目前只有 rank/zscore/demean/scale，缺少 `cs_neutralize(factor, industry)`，因此风格暴露未剥离，IC 中混有行业与市值效应。
- **模型成本与超时未压测**：AI 助手已接真实模型（eino，无 max_tokens 限制），但高并发下的配额/成本与长上下文耗时未做压测。
- **累计超额与 IC 背离未处理**：见 §5.3，需要加多空拆分、分组稳健性检验与极端值处理（winsorize）。
- 全量数据首次加载 2m42s，期间服务不可用；生产建议加"预热 + 就绪探针"，或改为增量更新。
