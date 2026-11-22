写公式时，字段名直接当变量用：`close`、`vol`、`roe` 都是当前面板上的一列。下表来自**当前已加载面板**，共 {{field_count}} 列（不含模拟盘内部列）。切换 Tushare / CSV 后自动变化。

## 1. Tushare 源口径

平台从 Tushare 写入 DSL 的列就这些。日 K 之外的估值、财务接口，没有接到面板里的，公式里写了会报「未知字段」。

| 来源 | 字段 | 说明 |
| --- | --- | --- |
| 日 K + 复权因子 | `open` `high` `low` `close` `vol` `amount` | 价格为**后复权** |
| 派生 | `pct_chg` | `close(t)/close(t-1)−1`，**小数**（0.02 = 2%） |
| 每日指标 | `turnover_rate` `turnover_rate_f` `volume_ratio` `pe` `pe_ttm` `pb` `ps` `ps_ttm` `dv_ratio` `dv_ttm` `total_share` `float_share` `free_share` `total_mv` `circ_mv` `limit_status` | 积分不足时可能整组缺失。不写入接口里的 `close`，避免覆盖后复权价 |
| 财务 | `roe` | 按**公告日**对齐（PIT），单位为百分数 |

`pct_chg` 不是 Tushare 接口原始字段，不要按百分数 2 去比较。

CSV 源：`date, code` 之后的数值列都会成为字段。自定义列若无内置说明，含义以你的表头为准。

## 2. 单位（跟 Tushare 一致）

| 字段 | 单位 | 公式里怎么用 |
| --- | --- | --- |
| `open` `high` `low` `close` | 元（后复权） | 动量、均线、振幅 |
| `pct_chg` | 小数 | `pct_chg > 0`；`ts_std_dev(pct_chg, 20)` |
| `vol` | 手（1 手 = 100 股） | 放量、量价相关；量级大，合并前要 `cs_rank` |
| `amount` | 千元 | `vwap ≈ amount / vol`（内置因子里的替换） |
| `turnover_rate` `turnover_rate_f` | % | `ts_mean(turnover_rate, 20)`；`_f` 用自由流通股本 |
| `volume_ratio` | 倍 | 量比；当天成交量 / 近期均量 |
| `pe` `pe_ttm` `pb` `ps` `ps_ttm` | 倍 | 估值；亏损股 `pe`/`pe_ttm` 常为 NaN |
| `dv_ratio` `dv_ttm` | % | 股息率；没分红则为缺失 |
| `total_share` `float_share` `free_share` | 万股 | 总股本 / 流通 / 自由流通 |
| `total_mv` `circ_mv` | 万元 | 先 `log` 再截面 |
| `limit_status` | 编码 | 0 平 1 涨 2 涨停 3 一字涨停 4 跌 5 跌停 6 一字跌停 |
| `roe` | % | `cs_rank(roe)` |

## 3. 按用途选字段

**价格与收益**

```
ret20 = close / ts_delay(close, 20) - 1
intraday = (close - open) / open
amp = (high - low) / close
```

**量与流动性**

```
surge = vol / ts_mean(vol, 20)
low_turn = -1 * cs_rank(ts_mean(turnover_rate, 20))
```

**估值、质量、规模**

```
ep = cs_rank(1 / (abs(pe_ttm) + 1))
quality = cs_rank(roe)
small = -1 * cs_rank(log(total_mv))
```

## 4. 模拟盘内部列（DSL 引用不到）

`raw_open` `raw_close` `qfq_*` `suspend` `up_limit` `down_limit` 只供模拟盘处理涨跌停与成交价，不会出现在下表。评价用后复权 `close`；模拟盘成交用前复权开盘（没有该列时退回 `open`）。

下面是当前面板的完整列表。
