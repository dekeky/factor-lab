本页是表达式语言的教材。算子完整清单在下方[算子速查](#operators)，由当前引擎注册表生成，签名以那里为准。

你不需要写 Python。引擎已经把「按股票滚动」「按交易日排名」包进算子，研究时只保留数学结构。字段名直接当变量用，最后一行的值就是当天每只股票的因子分数。

## 1. 第一段能跑的程序

```
# 20 日动量
ret20 = close / ts_delay(close, 20) - 1
alpha = cs_rank(ret20)
```

三行做了三件事：注释、算出收益率、在当天排队。粘贴到[因子工厂](#/factory)即可校验、评价。

{{dsl.result}}。分隔符：{{dsl.separators}}。空行忽略。

## 2. 变量、数值、窗口

| 规则 | 说明 |
| --- | --- |
| 赋值 | `name = expr`。`name`：字母、数字、下划线，不能以数字开头，不能与字段重名 |
| 表达式语句 | 单独一行表达式，通常用于最后一行 |
| 注释 | {{dsl.comment}} |
| 数值 | 整数、小数、科学计数法：`20`、`0.001`、`1e-3` |
| 窗口 | 必须是字面整数，范围 1～{{dsl.max_window}}。禁止 `n` 或 `10+10` |
| 嵌套 | 最多 {{dsl.max_depth}} 层 |

标识符区分大小写：`Close` 和 `close` 不是同一列。变量必须先定义后使用。

```
ma20 = ts_mean(close, 20)   # 可以
close = ts_mean(close, 20)  # 不行：close 是字段
ret 20 = ...                # 不行：变量名不能有空格
```

## 3. 算术、比较、逻辑

优先级从高到低：{{dsl.precedence}}。不确定时加括号。

| 类别 | 写法 | 语义 | 例子 |
| --- | --- | --- | --- |
| 算术 | `+ - * /`，一元 `-` | 逐元素；除数为 0 → NaN | `(close - open) / open` |
| 比较 | `< <= > >= == !=` | 真 1、假 0；NaN 参与视为假 | `pct_chg > 0` |
| 逻辑 | `not`、`and`、`or` | 非 0 且非 NaN 为真；无短路 | `pct_chg > 0 and vol > ts_mean(vol, 20)` |

不支持链式比较 `0 < pct_chg < 0.05`，应写成两个比较用 `and` 连接。

```
# 当天涨，且成交量高于 20 日均量
cond = pct_chg > 0 and vol > ts_mean(vol, 20)
alpha = if(cond, 1, 0)
```

比较结果是 0/1，可以直接参与运算：`cs_rank(pct_chg) * (pct_chg > 0)` 只保留上涨的股票的分数，其余为 0。

## 4. 时序算子：沿一只股票往回看

`ts_*` 固定股票、沿交易日滚动。窗口按行计数，停牌占行则计入。

```
ma20   = ts_mean(close, 20)          # 20 日均价
delay5 = ts_delay(close, 5)          # 5 个交易日前的收盘
ret5   = close / delay5 - 1          # 5 日收益；也可用 close / ts_delay(close, 5) - 1
vol20  = ts_std_dev(pct_chg, 20)     # 20 日收益波动（样本标准差，自由度 n−1）
corr20 = ts_corr(close, vol, 20)     # 价量 20 日相关
```

| 算子 | 一句话 |
| --- | --- |
| `ts_delay(x, n)` | n 日前的值 |
| `ts_delta(x, n)` | 今天减 n 日前，等于 `x - ts_delay(x, n)` |
| `ts_mean` / `ts_sum` / `ts_std_dev` | 窗口均值、求和、样本标准差 |
| `ts_max` / `ts_min` | 窗口最高 / 最低 |
| `ts_rank(x, n)` | 今天的值在自己近 n 日里的分位，0～1 |
| `ts_zscore(x, n)` | 相对自己近 n 日均值的标准分 |
| `ts_decay_linear(x, n)` | 越近权重越高的均值，常用来降换手 |
| `ts_corr` / `ts_cov` | 两列在窗口内的相关 / 协方差 |
| `ts_arg_max(x, n)` | 窗口最高点的位置：0 最旧，n−1 是今天 |

序列前 `n−1` 行是 NaN（warmup）。

## 5. 截面算子：同一天谁高谁低

`cs_*` 固定交易日、横着比所有股票。

```
# 最低 0、最高 1，并列取平均秩
alpha = cs_rank(roe)

# 当天相对均值多少个标准差，保留差距大小
z = cs_zscore(log(total_mv))
```

| 算子 | 一句话 |
| --- | --- |
| `cs_rank(x)` | 升序分位，输出 [0, 1] |
| `cs_zscore(x)` | 减均值除以标准差 |
| `cs_demean(x)` | 只减截面均值 |
| `cs_scale(x)` | 按绝对值之和缩放到约 1，接近「仓位占比」 |

嵌套按从内到外：`cs_rank(ts_mean(close, 20))` 先各自均价，再当天排名。

## 6. 数学与条件

逐元素，不引入窗口。

```
amp   = (high - low) / close           # 振幅
ep    = 1 / (abs(pe_ttm) + 1)          # 避免 PE 为负或为 0
lsize = log(total_mv)                  # 市值取对数；≤0 则为 NaN

# 只在上涨日取放量，否则 0
surge = vol / ts_mean(vol, 20)
alpha = if(pct_chg > 0, -1 * cs_rank(surge), 0)
```

`if(cond, a, b)`：cond 非 0 且非 NaN 取 `a`，否则取 `b`。

`log`：x≤0 → NaN。`sqrt`：x<0 → NaN。`sign`：正 1、负 −1、零 0。`power(x, 2)` 是平方。

## 7. 写作顺序

1. 先跑通内层，例如 `ts_mean(close, 20)`。
2. 再包外层，例如 `cs_rank(...)`。
3. 最后加符号、条件或加权。

复杂式拆中间变量。最后一行才是因子值。

```
mom    = cs_rank(close / ts_delay(close, 60) - 1)
lowvol = -1 * cs_rank(ts_std_dev(pct_chg, 20))
alpha  = 0.5 * mom + 0.5 * lowvol
```

加权前各自 `cs_rank` 或 `cs_zscore`，避免量纲不同的序列直接相加。

## 8. NaN 怎么传播

| 情形 | 结果 |
| --- | --- |
| 四则运算遇 NaN | NaN |
| 除数为 0 | NaN |
| `log`：x≤0；`sqrt`：x<0 | NaN |
| TS 窗口起点不足 n 行 | NaN（warmup） |
| TS 聚合 | 跳过窗口内 NaN；有效样本不足 → NaN |
| 比较遇 NaN | 视为假 |

覆盖度随 NaN 上升而下降。分母可能为 0 时加微小项：`high - low + 0.001`。

## 9. 校验

编辑器停止输入约 0.5 s 后静态检查。状态栏「✓ 校验通过」只说明句法与签名合法，不说明因子有效。报错带行号，对照表见[附录](#faq-errors)。

页面分工：

1. 因子工厂：编辑、校验、单次评价；右侧 AI 可生成或改写，应用前仍以评价为准。
2. 因子库：批量评价与排序。
3. 因子组合：多表达式标准化后加权，不在这里手写新 DSL（除非查看合成结果）。

## 10. 常用写法速查

| 想做的事 | 写法 |
| --- | --- |
| n 日收益 | `close / ts_delay(close, n) - 1` |
| n 日均线 | `ts_mean(close, n)` |
| 乖离率 | `close / ts_mean(close, n) - 1` |
| 当天排队 | `cs_rank(x)` |
| 方向取反 | `-1 * cs_rank(x)` |
| 低波动 | `-1 * cs_rank(ts_std_dev(pct_chg, 20))` |
| 量价相关 | `ts_corr(close, vol, 20)` |
| 放量 | `vol / ts_mean(vol, 20)` |
| 平滑降换手 | `ts_decay_linear(cs_rank(x), 10)` |
