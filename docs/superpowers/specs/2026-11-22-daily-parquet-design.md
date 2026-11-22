# A 股日 K 全市场 Parquet 缓存

日期：2026-11-22  
状态：已批准  
库：[parquet-go v0.32.1](https://github.com/parquet-go/parquet-go)

## 意图

`daily` 与 `daily_basic` 按交易日拉 A 股全市场，落成 5 年一片的 parquet。之后面板、K 线、reload 都读这些文件，不再按成分股逐只打这两个接口。

## 目标

- 从 2000-01-01 起，按交易日两天一批拉 `daily`；单次超过 6000 条则拆成一天一批。
- 同一批日期并行拉 `daily_basic`，按 `(ts_code, trade_date)` 拼到同一行（不写入 `daily_basic.close`）。
- 每个 Tushare 接口独立 500 次/分钟。
- 文件 5 年一片；已有日期增量补齐。
- 内存面板仍按当前指数成分和 `-start/-end` 从 parquet 切片。

## 非目标

- 内存面板改成全 A 股。
- `adj_factor` / ST / 财务 / 停牌 / 涨跌停改存 parquet。
- 改变评价股票池或因子 DSL。

## 约束

- 框架：`github.com/parquet-go/parquet-go` **v0.32.1**。
- 单次 Tushare 结果上限按 6000 条处理；`>= 6000` 或 `has_more` 视为截断，两天窗口回退到一天一批。
- 目录：`{storeDir}/parquet/daily/{yyyy}-{yyyy}.parquet`，例如 `data/parquet/daily/2000-2004.parquet`。
- 分片：从 2000 起每 5 年，`2000-2004`、`2005-2009`、…。

## 方案

1. **全市场进内存**：否，体积太大。
2. **按股票一个 parquet**：否，与按日拉取不匹配。
3. **全市场 parquet + 面板按指数切片**（采用）：拉取与存储按全市场，`Load` 只把当前成分和日期读进内存；`adj_factor` 等仍按成分请求。

## 做法

- 先 `trade_cal`（上交所、`is_open=1`）得到 2000 至今的交易日，减去 parquet 里已有的日期。
- 剩余日期两两一批：`start_date`/`end_date` 不带 `ts_code`。`daily` 与 `daily_basic` 并行，各走自己的 500/分钟限流。
- 当前 5 年分片在内存里攒齐再原子写盘（tmp + rename）。已完成分片只读。
- `Load`：补齐缺失交易日 → 读重叠分片 → 过滤成分代码和面板日期 → 交给现有 `assemblePanel`。
- 生产默认 `DataDir` 与因子库同目录（`data/`）。启动加载超时放宽到足以跑完首次回填。
- `-tushare-qps` 不再做全局限流；改为每接口 500 次/分钟（可用 `-tushare-rpm` 覆盖）。

## 留给实现

- 1 天仍 ≥6000 时用现有分页。
- Windows 下原子替换：rename 失败则删目标再 rename。
- 测试用 `t.TempDir()` 做 DataDir，mock `trade_cal` 只返回样本日。
