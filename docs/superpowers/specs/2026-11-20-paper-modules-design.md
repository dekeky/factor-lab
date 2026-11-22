# 模拟盘模块拆分与日更撮合

日期：2026-11-20  
状态：已批准  
依赖：[River](https://github.com/riverqueue/river)（和领域表用同一份 SQLite）

任务两种：`paper.generate`（出计划）、`paper.execute`（成交记账）。  
拉 K 是这两种任务自己的步骤，不是第三种任务。缺数或不全：本任务 **Snooze 1 分钟** 再跑，不写计划、不改账。

T+1 靠任务隔天衔接，不靠持仓字段：`generate` 用信号日收盘打分，`execute` 在**下一交易日**开盘成交。同一账户同一天不会跑两次 execute，刚买的票不会在当天再卖。

---

## 1. 背景

现在的 `Replay` 把选股和成交写在同一个日期循环里。账户是一个大 JSON，打开页面就把整段历史再跑一遍。没有「计划」对象，也没有「这步做过没有」。

评价引擎和因子 DSL 不在范围内。模拟盘仍然是虚拟资金，不报单。T+1 开盘成交、整手、费率沿用现在的口径。买卖股数在 **generate 用信号日收盘**算好并写入计划；execute 按计划数量成交，不再现场等权。停牌、涨跌停、缺价不挡入选，由执行决定做不做、做成还是失败。开盘价高于收盘估价、费用滑点吃掉现金时，买入股数每次减 100 再试。

---

## 2. 目标

实现下面三条能力。实现者按同一套顺序做，不要再合成 `Replay`。

**开户：** 写入 `account.Account` 和初始化 `ledger.Snapshot`（日期 = `Start` 日历减一天，现金 = 本金，持仓空）→ 入队 `jobs.GenerateArgs{SignalDate: Start}`，`ScheduledAt` = 起始日 **15:10:00**。该时刻已过（回放、起始日在过去）则立刻跑。

**交易计划生成：** 消费 `GenerateArgs` → 按股票池和公式所需字段拉 K → 拉不到或不全则推迟 1 分钟重试 → 按冻结公式对 **`SignalDate` 当天收盘**打分 → 结合已有 `ledger.Book` 和**当日收盘价**写出一份 `matching.Plan`（每条 Intent 带买入/卖出股数；`ExecAt` = 信号日的下一交易日 09:30:00），并入队 `jobs.ExecuteArgs`。

**交易计划执行：** 消费 `ExecuteArgs` → 拉计划股票在 `Plan.ExecAt` 那天的 K，以及当前 `Book` → `matching.Execute`（按 Intent 上的股数成交；买入现金不够则股数减 100 再试）→ **一笔事务**里写入 `ledger.Fill`、`ledger.Snapshot`，计划 `pending→done`，再入队下一条 `GenerateArgs`（`SignalDate` = 成交日，`ScheduledAt` = 成交日 15:10:00）。

一天一份计划，一份计划只成交一次（成交时刻的日历日 UNIQUE + `pending→done`）。账户上不放 T+1/T+0 开关。计划执行时间 = 成交时间，存 `YYYY-MM-DD HH:MM:SS`，精确到秒，时区 `Asia/Shanghai`。每份计划的成交时刻都是「生成所用信号日」的下一交易日 `09:30:00`。拉 K、一天一份都看这个时刻的日期部分。

`SignalDate` 不准空。打分日只看任务参数里的这一天，**不准**用 Snapshot 日期去猜。

---

## 3. 功能实现和时序流

### 3.1 开户

写 `account` 表（`account.Account`）、初始化 `snapshot`，入队 `paper.generate`（`jobs.GenerateArgs`）。不写 `plan` / `fill`。

```go
// Config 这户怎么做交易：拿几只、跌出多少名才换、本金费率、从哪天开始。
type Config struct {
    TopN               int     // 目标拿几只，等权。默认 10
    RebalanceThreshold int     // 已持仓排名仍 ≤ TopN+这个数就不换。0 = 刚跌出 TopN 就换。以前叫 Buffer
    Capital            float64 // 虚拟本金，单位元。只在开户记一笔，后面不改这张表。默认 1_000_000
    Cost               float64 // 单边费率，买一次扣、卖一次扣。默认 0.003
    SlippageBps        float64 // 滑点，单位 bp。买价 ×(1+bps/1e4)，卖价 ×(1-bps/1e4)。默认 0
    Start              string  // 交易起始日 YYYY-MM-DD。空 = 用面板第一天。写入账户前解析成日期轴上 >= 该日的第一根交易日
}

// Account 一个模拟账户。公式和规则开户时冻住，后面打分、成交都认这一份。
type Account struct {
    ID         int       // 主键
    Name       string    // 给人看的名字
    FactorID   int       // 绑哪个因子。和 ComboID 只能用一个；0 = 没用因子
    FactorName string    // 开户时抄下来的因子名，以后因子改名也不改这里
    ComboID    int       // 绑哪个组合。和 FactorID 只能用一个；0 = 没用组合
    ComboName  string    // 开户时抄下来的组合名
    Expr       string    // 冻住的因子公式。之后 generate 只拿这一串求值
    Config     Config    // 交易规则。没有「延迟几天成交」这种开关
    CreatedAt  time.Time // 开户时间
    UpdatedAt  time.Time // 上次改账户资料的时间（改名等；不改公式）
}

// GenerateArgs 让队列去出一份交易计划。开户这一条 SignalDate=已写入的 Start。
type GenerateArgs struct {
    AccountID  int    `json:"account_id"`  // 给哪户出计划
    SignalDate string `json:"signal_date"` // 用哪天收盘打分 YYYY-MM-DD。不准空。成交=这天的下一交易日 09:30:00
} // Kind: paper.generate  Unique: 同一账户同一 SignalDate 不能入队第二条（含已完成）
```

一次事务：

1. 写入 `account`（冻结 `Expr`、TopN、本金、费率、起始日）。`Start` 空则取面板第一天；所填日期不在日期轴上则取轴上 `>=` 该日的第一根；晚于面板最后一天则开户失败。
2. 写入初始化 `snapshot`：`Date` = 已写入的 `Start` **日历减一天**（可以不是交易日），现金 = 本金，持仓空，NAV = 本金，Ret = 0。这只是账本起点，**不是**打分日。
3. 入队 `paper.generate`，参数 `GenerateArgs{AccountID, SignalDate: Start}`，`ScheduledAt` = `Start` 当天 **15:10:00**（Asia/Shanghai）。该时刻已过则立刻可跑。

HTTP 立刻返回。

开户失败则账户、初始化 snapshot、generate 任务都不存在。

### 3.2 交易计划生成 `paper.generate`

入参 `GenerateArgs`。读 `Account`、最新 `ledger.Book`。写出 `matching.Plan`，入队 `ExecuteArgs`。不写 `fill` / `snapshot`。

```go
// GenerateArgs 出计划任务的参数。SignalDate 不准空：用那天收盘打分，成交下一交易日 09:30:00。
type GenerateArgs struct {
    AccountID  int    `json:"account_id"`  // 给哪户出计划
    SignalDate string `json:"signal_date"` // 打分日 YYYY-MM-DD。开户填 Start；成交后填成交日。不准空
} // Kind: paper.generate  Unique: 同一账户同一 SignalDate 不能入队第二条（含已完成）

// Position 手里一只股票。Price/Value/Weight/PnL 是算出来给人看的，不存库。
type Position struct {
    Code   string  // 股票代码
    Name   string  // 股票名称
    Label  string  // 展示用标签（板块、ST 之类）
    Shares float64 // 持有多少股
    Cost   float64 // 剩下这些股的买入均价，买入费已经摊进去
    Price  float64 `json:"-"` // 现价（一般用收盘），不存库
    Value  float64 `json:"-"` // 市值 = 现价×股数，不存库
    Weight float64 `json:"-"` // 占净值的比例，不存库
    PnL    float64 `json:"-"` // 浮盈亏 = (现价−成本)×股数，不存库
}

// Book 此刻账上有多少钱、拿着什么。从最新一份 Snapshot 拼出来；没有 Snapshot 则现金=本金、持仓空。
type Book struct {
    AccountID   int        // 哪户
    Cash        float64    // 还能用的现金
    Positions   []Position // 当前持仓
    LastSettled string     // 账算到哪一天，等于最新 Snapshot.Date；没有快照则空
}

// Intent 计划里的一只目标或待清仓股票。留下的持仓也要写进来（hold），带打分、排名、买卖股数和原因。
// 计划股数和原因在 Generate 用信号日收盘算死；Execute 只按 BuyShares / SellShares 成交，不再现场等权。
// 实际股数生成时为 0，成交后写回同一条 Intent，计划行与 Fill 同一事务更新。不准改原因。
type Intent struct {
    Code             string  // 股票代码
    Side             string  // buy 新开 | sell 清掉 | hold 仍在目标集合（可能加减仓）
    BuyShares        float64 // 计划买入股数。无买入为 0。必须是 100 的整数倍
    SellShares       float64 // 计划卖出股数。无卖出为 0。清仓=当前持仓（零股也全卖）。其余必须是 100 的整数倍
    ActualBuyShares  float64 // 实际买入股数。生成时为 0；execute 成交后更新；没买到仍为 0
    ActualSellShares float64 // 实际卖出股数。生成时为 0；execute 成交后更新；没卖出仍为 0
    Reason           string  // 买入或卖出原因。无调仓为空。enter=空位补位新开；exit=排名跌出阈值清仓；rebalance=权重偏离目标超过 3% 加减仓。方向看 Side / 股数字段
    Score            float64 // 打分日的因子分数；没有分数则 NaN
    Rank             int     // 当天截面排名，1 最好；没有分数则 0
}

// Plan 一天一份的交易计划。执行时间就是成交时间，开盘价撮合。
type Plan struct {
    ID         int64    // 主键
    AccountID  int      // 哪户
    SignalDate string   // 用哪天的收盘分数选出这批股票 YYYY-MM-DD。等于 GenerateArgs.SignalDate
    ExecAt     string   // 成交时刻 YYYY-MM-DD HH:MM:SS，精确到秒，Asia/Shanghai，不准空。= 信号日下一交易日 09:30:00。空或解析失败则 execute 报错
    Intents    []Intent // buy / sell / hold，含计划股数和买卖原因；成交后带实际股数。停牌涨跌停缺价也可以出现在这里
    Status     string   // pending 还没成交；done 成交过了，不能再跑
}

// ExecuteArgs 去成交某一份计划。不传时间，成交时刻只看 Plan.ExecAt。
type ExecuteArgs struct {
    AccountID int   `json:"account_id"` // 哪户
    PlanID    int64 `json:"plan_id"`    // 成交哪一份计划
} // Kind: paper.execute  Unique: 同一份计划不能入队两条成交任务

// Generate 按分数、现有持仓和信号日收盘写出一份计划（Intents 含买卖股数 + SignalDate），不碰成交、不改账。ExecAt 由 worker 在 INSERT 前填。
func Generate(p *panel.Panel, factor panel.Col, acc account.Account, book ledger.Book, signalDate string) (Plan, error)
```

`Generate` 不调用 `Risk`。持仓字段含义与 3.3 相同。

Worker 按序：

1. 读账户。没有账户 → 成功返回。`SignalDate` 为空 → **报错返回**（River 当失败重试），不准改用 Snapshot 日期。
2. **拉数。** 股票池 = 当前数据源成分（与进程面板 / Tushare 指数相同）。需要的字段 = 冻结 `Expr` 用到的 DSL 字段，外加算股数用的信号日收盘：`qfq_close`（无则 `close`）。因子有窗口，必须有足够历史，v1 复用现有全量 `Load`/`Reload`，不另做「只拉一根 K」。停牌、涨跌停、开盘价是执行侧字段，生成不依赖它们来决定入选。
3. **数齐了没有。** 不齐就不要写计划，这条任务 `JobSnooze(1 * time.Minute)` 再拉。CSV 不能 Reload：这条任务 Discard，页面告诉人只能用已经加载的数据。
   齐的标准：
   - 股票池里有股票；公式用到的列都在；面板有 `qfq_close` 或 `close`。
   - **打分日 = `GenerateArgs.SignalDate`**（不准空、不准用 Snapshot 日期替代）。这根 K 在日期轴上。
   - 日期轴上能看到打分日的**下一交易日**（用来写 `ExecAt`）。下一根只从日期轴认，那天的开收盘、停牌、涨跌停一律不看。
   看不到打分日、或看不到下一交易日，就算没齐。单只股票信号日缺收盘不挡出计划，该条 Intent 买卖股数为 0。
4. **求值用到哪段行情。** 用现成 `panel.Slice`（`to=打分日`）得到**本次调用的副本**（或只取打分日那一行截面），再对冻结 `Expr` 求值。打分日之后的 K **不准**进入公式。**不准**把截断结果写回进程内存面板。求值必须用打分日那一行，不准默认用全局面板最后一根。
5. 读最新账本（无 snapshot 则现金=本金、持仓空）。开户已经写了初始化 snapshot，正常走最新那一行。
6. `matching.Generate`：用账户的 `TopN` 和 `RebalanceThreshold`（现 Buffer）写计划。按分数排序（有分的都排，不管停牌涨跌停缺价）：
   - 持仓内股票排序掉落至 RebalanceThreshold 后，添加至全部卖出计划。`Reason=exit`。
   - 根据卖出数量，按排序补位新的排名靠前的股票进来。`Reason=enter`。
   - 持仓股票+待买入的新股票，一起计算权重，权重不均衡，偏移超过 3% 时，重新 rebalance，过高的卖出，过低的买入，添加到买入和卖出计划。`Reason=rebalance`。
   无调仓则 `Reason` 必须空。只准用 `enter` / `exit` / `rebalance`。成交失败原因写在 Fill.Status，不准写进 `Reason`。
   算买入股数时预留费率和滑点：分母用 `px × (1+SlippageBps/1e4) × (1+Cost)`，再向下取整到 100 股。卖出股数不因滑点变少。
7. **一笔事务：** worker 把 `Plan.ExecAt` 写成「打分日下一交易日 `09:30:00`」（不准空）；`INSERT plan`；`InsertTx` `paper.execute`，参数 `ExecuteArgs{AccountID, PlanID}`，`ScheduledAt` = 解析后的 `ExecAt`。该时刻已过则马上跑；还没到就等到这一秒。成交日还没有 K 则 execute 自己推迟 1 分钟。拉 K 用 `ExecAt` 的日期部分。同一 `SignalDate` 已有计划（成交日 unique 冲突）→ 视为已完成，不插第二行、不重复入队 execute。

生成 worker 不准成交、不准写 fill/snapshot。

### 3.3 交易计划执行 `paper.execute`

入参 `ExecuteArgs`。读 `Account`、`Plan`、最新 `Book`。同一事务写 `Fill`、`Snapshot`、把 Intent 实际股数写回 `plan.intents` 并 `status=done`，再入队下一条 `GenerateArgs`。不准调用 `Generate`。

```go
// ExecuteArgs 成交任务：把这一份计划在 Plan.ExecAt 这一秒做成成交记录。
type ExecuteArgs struct {
    AccountID int   `json:"account_id"` // 哪户
    PlanID    int64 `json:"plan_id"`    // 成交哪一份计划
} // Kind: paper.execute  Unique: 同一份计划不能入队两条成交任务

// Position 手里一只股票。现价市值权重盈亏是算的，不存库。没有可卖字段：T+1 由「下一交易日才 execute」保证。
type Position struct {
    Code   string  // 股票代码
    Name   string  // 股票名称
    Label  string  // 展示用标签
    Shares float64 // 持有多少股
    Cost   float64 // 剩下这些股的买入均价，买入费已经摊进去
    Price  float64 `json:"-"` // 现价，不存库
    Value  float64 `json:"-"` // 市值，不存库
    Weight float64 `json:"-"` // 占净值比例，不存库
    PnL    float64 `json:"-"` // 浮盈亏 = (现价−成本)×股数，不存库
}

// Fill 一笔成交或一次没做成。停牌、涨跌停、没价也留一行，Status 说明原因，不换别的股票顶上。
type Fill struct {
    ID            int64   // 主键
    AccountID     int     // 哪户
    PlanID        int64   // 来自哪份计划
    Date          string  // 成交时刻，等于 Plan.ExecAt，YYYY-MM-DD HH:MM:SS
    Code          string  // 股票代码
    Side          string  // buy 买 | sell 卖。hold 加减仓也落成 buy 或 sell
    Shares        float64 // 成交股数；没做成则为 0
    Price         float64 // 成交价（开盘价，含滑点口径见 Execute）
    Amount        float64 // 成交金额
    Fee           float64 // 这一笔扣的费用
    Slippage      float64 // 这一笔滑点金额
    Status        string  // filled 成交 | skipped_suspend 停牌 | skipped_limit_up 开盘涨停买不进 | skipped_limit_down 开盘跌停卖不出 | skipped_untradeable 没价或不能成交
}

// Snapshot 某一天收盘时账户长什么样。开户那份日期是起始日日历减一天；成交写入的日期是成交日。
type Snapshot struct {
    AccountID int        // 哪户
    Date      string     // 这是哪天收盘的快照 YYYY-MM-DD
    NAV       float64    // 当天净值 = 现金 + 持仓市值
    Cash      float64    // 当天收盘现金
    Ret       float64    // 相对上一份快照的收益率
    Holdings  []Position // 当天收盘还拿着什么。入库只存 code/name/label/shares/cost
}

// Book 成交前读进来的当前账，成交后 Apply 得到下一份。
type Book struct {
    AccountID   int        // 哪户
    Cash        float64    // 现金
    Positions   []Position // 持仓
    LastSettled string     // 已经结算到哪一天
}

// GenerateArgs 成交做完后，排一条「用今天收盘再选一次股」。跑的时间是成交日 15:10。
type GenerateArgs struct {
    AccountID  int    `json:"account_id"`  // 给哪户出计划
    SignalDate string `json:"signal_date"` // 填成交日（ExecAt 的日期）：用这天收盘打分，下一份计划成交=下一交易日 09:30:00
} // Kind: paper.generate  Unique: 开户那条 SignalDate=Start；这里是成交日。第一笔成交日是 Start 的下一交易日，不撞

// Apply 把成交记到账上，返回成交后的现金和持仓。不写库。
func Apply(book Book, fills []Fill) (Book, error)

// Execute 按计划里已经写好的股数成交，并填好每条 Intent 的 ActualBuyShares / ActualSellShares（不写库）。停牌涨跌停缺价只跳过该笔，不另找股票，不重算等权。
func Execute(p *panel.Panel, plan Plan, book ledger.Book, cfg account.Config, execAt string) (fills []ledger.Fill, intents []Intent, next ledger.Book, snap ledger.Snapshot, err error)
```

「更新账户持仓、资产」= 写 `snapshot` + `Apply` 得到的 `Book`，**不改** `account` 表里的本金和公式。风控只在 `Execute`：停牌不能动；开盘涨停不能买；开盘跌停不能卖；缺价不能成交。

Worker 按序：

1. 读账户、该 `plan_id`。没有计划或已是 `done` → 成功返回（重试空操作）。
2. 成交时刻只用 `plan.exec_at`。为空或不是 `YYYY-MM-DD HH:MM:SS` → **报错返回**（River 当失败重试），不准从面板猜，不准写回，不准自己补 `:00`。
3. **拉数。** 需要成交日（`exec_at` 的日期部分）这根 K，字段至少：计划内代码 + 当前持仓代码 的 `qfq_open`（无则 `open`）、`qfq_close`（无则 `close`）、`suspend`、`up_limit`、`down_limit`。内存里已有且完整则不打网；否则 `Load`/`Reload`。本次调用只用成交日及以前的行情（`Slice` `to=成交日` 的**副本**），成交日之后的不用，**不准**写回进程内存面板。
4. **完整性：** 没有成交日这根 K，或上述列缺失 → **不改计划、不改账**，`JobSnooze(1 * time.Minute)`。
5. 读当前账户基础信息：最新 snapshot 拼出的 `Book`（现金、持仓）。没有 snapshot 则现金=本金、持仓空。
6. `matching.Execute`（先卖后买，数量只用 Intent 上的股数，**不准**再用开盘价重算等权）：
   - 成交价 `px` = `qfq_open`→`open`，再套滑点：买 `px×(1+SlippageBps/1e4)`，卖 `px×(1−SlippageBps/1e4)`。收盘计价仍 `qfq_close`→`close`。
   - **先卖：** 所有 `SellShares>0` 的 Intent（`sell` 清仓和 `hold` 减仓）。能卖则按 `min(SellShares, 当前持仓)` 成交；停牌 / 开盘跌停 / 缺价 → fill `skipped_*`、股数 0，这只票留在账上，不换别的股票顶上。`SellShares=0` 不写卖出 fill。
   - **后买：** 所有 `BuyShares>0` 的 Intent（`buy` 新开和 `hold` 加仓）。停牌 / 开盘涨停 / 缺价 → fill `skipped_*`、股数 0，不换股票。
   - **现金不够：** 买入占用现金 = `股数 × 成交价 × (1+Cost)`。成交价已含滑点（上一条的买价），展开即 `股数 × 开盘价 × (1+SlippageBps/1e4) × (1+Cost)`。从计划 `BuyShares` 起，若现金不够，**股数减 100** 再算，循环直到现金够或股数 < 100。减完仍买不起 1 手 → fill `skipped_untradeable`、股数 0。减过手但仍买到的 → fill `filled`，`Shares` 为实际买到的整手数×100（可以小于计划）。`Fill.Amount` = 股数×含滑点成交价；`Fill.Fee` = Amount×Cost；`Fill.Slippage` = 股数×开盘价×SlippageBps/1e4。
   - 每条 Intent 成交后立刻写回实际股数（只改内存副本，入库见下一步）：`ActualBuyShares` = 该股买入 Fill 的 `Shares`（无买入 fill 或 skipped 则为 0）；`ActualSellShares` 同理。不准改 `BuyShares`/`SellShares`/`Reason`。`BuyShares=SellShares=0` 的 hold：不写 fill，实际股数保持 0。
   - 同一份 Execute 里先处理全部卖，再处理全部买，不会把本笔刚买的票再卖掉。
7. **一笔事务（必须同一 tx）：**
   - `UPDATE plan SET status='done', intents=? WHERE id=? AND status='pending'`。`intents` 为已填实际股数的完整 JSON。0 行 → 空提交，不准覆盖已成交计划上的实际股数。
   - `INSERT fill…`（含 skipped）。
   - `INSERT snapshot`（该日现金、持仓、NAV、Ret）。该日已有 snapshot → 整笔当已完成，空提交（幂等；初始化 snapshot 写的是 Start 日历减一天，不会和成交日撞）。
   - `InsertTx` `paper.generate`，参数 `GenerateArgs{AccountID, SignalDate:成交日}`（成交日 = `ExecAt` 的日期），`ScheduledAt` = 成交日 **15:10:00**。已过则马上跑。开户那条是 `SignalDate=Start`，第一笔成交日是 Start 的下一交易日，Unique 不撞。写出的下一份 `Plan.ExecAt` = 该信号日的下一交易日 `09:30:00`。

失败整笔回滚，计划仍 pending，River 按失败重试。执行 worker 不准调用 `Generate` 领域函数。

### 3.4 这三条之间怎么接

```text
开户 → snapshot(Start 日历-1天) + generate(SignalDate=Start) @ Start 15:10
     → 第一份计划 ExecAt=Start 的下一交易日 09:30:00
     → 成交记账（Fill.Date = ExecAt；Snapshot.Date = 成交日）
     → generate(SignalDate=成交日) @ 成交日 15:10:00
     → 下一份计划 ExecAt=再下一交易日 09:30:00
     → … 互相推着走
```

同一信号日：15:10 选股，下一交易日 09:30 成交；成交后再用成交日收盘选下一轮。缓冲换手看到的是已经成交后的持仓。  
时刻已过（回放历史、起始日在过去）→ 任务到期马上跑。  
进程重启：River 把 scheduled / snoozed / retryable 接着投。不另做 `kick`。

`ScheduledAt` 是逻辑时戳。实盘 09:30 往往还没有当日完整日 K，execute 缺数就 snooze，日 K 齐了再按开盘价补记。齐数以日 K 为准，不以墙钟为准。

`GET` 只读。同步按钮不必入队第三种任务：缺数的 generate/execute 自己每分钟重试。

### 3.5 禁止

HTTP 不准调 `Generate`/`Execute`，不准写成交。  
一个 worker 里不准连续跑两种领域函数。  
队列 `paper`，`MaxWorkers: 1`（SQLite 单写者，拉数也不会并发打爆）。  
不准把 `panel.Slice` 的结果写回进程内存面板。

### 3.6 页面

`GET` 组装只读 `Paper`，不落盘：

```go
// Paper 打开页面时拼出来给人看的，不存库。
type Paper struct {
    Account account.Account     // 这户是谁、公式和规则
    Book    ledger.Book         // 现在账上的钱和持仓
    Plans   []matching.Plan     // 已经生成的计划，含还没成交的
}
```

净值横轴用 `Snapshot.Date`（初始化点 + 每个成交日）。待成交看 pending `Plan.ExecAt`。删掉 `fill_date`。回放中展示未完成任务（`river_job`）。改 UI 后走 Go 的 `web/dist`。

### 3.7 测试

用 River 在进程内入队并消费，不要用 HTTP 当撮合驱动。

- 开户：一条 generate（`signal_date=Start`）排在 `Start` 15:10；一份初始化 snapshot（日期 = Start 日历减一天）；不写 plan / fill / 起始日当天 snapshot。
- 第一份计划 `signal_date=Start`，`exec_at=Start 的下一交易日 09:30:00`；execute 排在这一秒（已过则马上成交）；成交后才有成交日 snapshot。
- 开户回滚：无 account、无 snapshot、无 generate。
- generate 的 `signal_date` 为空：报错，不写计划，不准改用 snapshot 日期。
- generate 数据不全：无新 plan 行，任务处于 snoozed。
- generate 成功：一行 plan（含 hold/buy/sell Intent，留下的持仓有 Score、Rank，buy/sell/hold 带 BuyShares、SellShares 和 Reason；买入股数为 100 的整数倍，由信号日收盘、费率、滑点向下取整；ActualBuyShares/ActualSellShares 均为 0），一条 execute(plan_id) 排在 `exec_at` 这一秒。
- generate 原因：跌出阈值清仓 `exit`；空位补位 `enter`；权重偏移超 3% 加减仓 `rebalance`；无调仓则空。
- generate 数量：等权按信号日收盘估 NAV；缺收盘的 buy/hold 股数为 0 仍写入；sell 股数=当前持仓。
- generate 求值不得改写进程内存面板；本次求值用到的最后一天 = 打分日；打分日之后的收盘不进公式。
- 第二次 generate 同一信号日：不插第二行计划。
- execute 缺成交日 K：无 fill，plan 仍 pending，实际股数仍为 0。
- execute 成功：fill + snapshot + plan.done 同一事务，plan.intents 写回 ActualBuyShares/ActualSellShares（与对应 Fill.Shares 一致；skipped 为 0；BuyShares/SellShares/Reason 不变）；再入队 generate(成交日) @ 15:10。
- 该 generate 能写出**第二份**计划（`exec_at`=再下一交易日 09:30:00），不和开户任务 Unique 撞车。
- 第二次 execute：资金不变，不追加 fill，计划上的实际股数不变。
- 停牌/涨跌停/缺价：计划里可以有这些股票和计划股数；执行记 skipped，Actual*=0，不改该笔资金，不另找股票、不重算数量。
- execute 按计划股数成交：不因开盘价重做等权。买入占用现金含滑点和费率；不够则股数每次减 100 再试，买到的写入 Fill 和 Intent.ActualBuyShares（可小于 BuyShares）；减到不足 1 手则 skipped_untradeable，ActualBuyShares=0。
- RebalanceThreshold / 先卖后买 / 费用 / 滑点 与现口径一致；hold 加减仓的股数以计划为准。

---

## 4. 类型放哪、表怎么对

结构体写在对应功能里，这里只登记包和落盘规则。

| 包 | 类型 | 出现在 |
|---|---|---|
| `internal/paper/account` | `Config` `Account` | 3.1 开户（生成/执行只读） |
| `internal/paper/jobs` | `GenerateArgs` `ExecuteArgs` | 3.1 入队 generate；3.2 入队 execute；3.3 入队下一笔 generate |
| `internal/paper/matching` | `Intent` `Plan`；`Generate` `Execute` | 3.2 出计划；3.3 读计划 |
| `internal/paper/ledger` | `Position` `Fill` `Snapshot` `Book`；`Apply` | 3.2 读 Book；3.3 写成交和快照 |
| 不落盘 | `Paper` | 3.6 GET |

`json:"-"` 不入库。K 线是内存 `*panel.Panel`，用现有 `Options.Reload` / `src.Load`，不进 `paper.db`。没有 `reload_panel`、没有 `kick`。`Generate` 和 `Execute` 不得互相调用。求值 / 撮合用的 `Slice` 只作用于本次调用的副本。

---

## 5. DB 变更

`data/paper.db`，与 `factors.json` 同目录。K 线不入库。River 表官方 migrate。旧 `paper.json` 不导入。

```sql
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;

CREATE TABLE IF NOT EXISTS account (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    name                TEXT    NOT NULL,
    factor_id           INTEGER NOT NULL DEFAULT 0,
    factor_name         TEXT    NOT NULL DEFAULT '',
    combo_id            INTEGER NOT NULL DEFAULT 0,
    combo_name          TEXT    NOT NULL DEFAULT '',
    expr                TEXT    NOT NULL,
    top_n               INTEGER NOT NULL,
    rebalance_threshold INTEGER NOT NULL DEFAULT 0,
    capital             REAL    NOT NULL,
    cost                REAL    NOT NULL,
    slippage_bps        REAL    NOT NULL DEFAULT 0,
    start               TEXT    NOT NULL DEFAULT '',
    created_at          TEXT    NOT NULL,
    updated_at          TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS plan (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id  INTEGER NOT NULL REFERENCES account(id) ON DELETE CASCADE,
    signal_date TEXT    NOT NULL,
    exec_at     TEXT    NOT NULL, -- YYYY-MM-DD HH:MM:SS Asia/Shanghai，精确到秒，不准空
    intents     TEXT    NOT NULL,
    status      TEXT    NOT NULL CHECK (status IN ('pending','done'))
);
-- 一天一份计划看成交时刻的日期部分。signal_date 开户=Start，之后=成交日，正常不重复。

CREATE UNIQUE INDEX IF NOT EXISTS plan_account_exec_day
    ON plan (account_id, substr(exec_at, 1, 10));

CREATE TABLE IF NOT EXISTS fill (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id     INTEGER NOT NULL REFERENCES account(id) ON DELETE CASCADE,
    plan_id        INTEGER REFERENCES plan(id) ON DELETE SET NULL,
    date           TEXT    NOT NULL, -- 成交时刻，等于 plan.exec_at
    code           TEXT    NOT NULL,
    side           TEXT    NOT NULL,
    shares         REAL    NOT NULL,
    price          REAL    NOT NULL,
    amount         REAL    NOT NULL,
    fee            REAL    NOT NULL,
    slippage       REAL    NOT NULL DEFAULT 0,
    status         TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS fill_account_date ON fill (account_id, date);
CREATE INDEX IF NOT EXISTS fill_plan ON fill (plan_id);

CREATE TABLE IF NOT EXISTS snapshot (
    account_id INTEGER NOT NULL REFERENCES account(id) ON DELETE CASCADE,
    date       TEXT    NOT NULL,
    cash       REAL    NOT NULL,
    nav        REAL    NOT NULL,
    ret        REAL    NOT NULL,
    holdings   TEXT    NOT NULL,
    PRIMARY KEY (account_id, date)
);
```

当前账本 = 该账户 `snapshot.date` 最大一行；没有行则现金=本金、持仓空。成交查 `fill.date`。任务查 `river_job`。
