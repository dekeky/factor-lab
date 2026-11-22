---
id: pitfalls
level: 进阶
minutes: 12
title: 稳健性、陷阱与模板
summary: 过拟合、量纲、NaN、同质化、交易成本。下面的模板只改窗口或符号即可评价。
keywords: 过拟合 量纲 NaN 除零 相关 换手 平滑 窗口 模板
---

前面几章足够跑通一条因子。这一章是研究时反复会碰到的坑，以及几条可以直接改窗口的公式。

## 1. 过拟合

在 17、23 这类窗口上搜索，总能找到样本内更好的数，换区间即失效。

- 窗口用 5 / 20 / 60 / 120 / 250。
- 研究区间和验证区间切开，验证只做一次。
- 参数微扰就反号的因子丢掉。

「样本内 RankIC 特别高」优先当警告，不当成绩。

## 2. 量纲与极值

`pct_chg` 大约是 10⁻²，`vol` 经常是 10⁵。直接加减等于让成交量一项说了算。

```
# 错误：收益率和成交量量纲不同
alpha = pct_chg + vol

# 正确：先各自排队再加
alpha = 0.5 * cs_rank(pct_chg) + 0.5 * cs_rank(vol)
```

市值、成交额先 `log` 再参与截面。`cs_rank` 吃掉量纲和极端值；`cs_zscore` 保留差距大小，但更容易被极端值带着走。

## 3. 缺失值与除零

- NaN 参与四则运算，结果仍是 NaN，覆盖度下降。
- 除数为 0 → NaN。分母可能为 0 时加微小项，例如 `high - low + 0.001`。
- `log(x)` 要求 x>0，`sqrt(x)` 要求 x≥0，否则 NaN。
- 时序窗口在序列前 `n−1` 行输出 NaN，属 warmup，不是校验错误。
- 比较运算里 NaN 按假（0）处理。滚动聚合跳过窗口内 NaN；有效样本不足则输出 NaN。

## 4. 同质化与成本

成分相关 > 0.7：信息重复，组合里留一个。短周期反转 IC 高、换手高，费用常吃光毛收益。需要降换手时，对信号做衰减加权：

```
alpha = ts_decay_linear(cs_rank(pct_chg), 10)
```

越近的观测权重越高，比简单 `ts_mean` 更跟手，又比原始信号稳。

## 5. 可改参数的模板

复制到[因子工厂](#/factory)，只改窗口或最后的符号。更多完整式见[内置因子](#builtins)。

**中期动量**

```
ret20 = close / ts_delay(close, 20) - 1
alpha = cs_rank(ret20)
```

**短期反转**

```
ret5 = close / ts_delay(close, 5) - 1
alpha = -1 * cs_rank(ret5)
```

**低波动**

```
alpha = -1 * cs_rank(ts_std_dev(pct_chg, 20))
```

**量价背离**

```
alpha = -1 * ts_corr(close, vol, 20)
```

**均线乖离**

```
alpha = close / ts_mean(close, 20) - 1
```

**价格位置**

```
pos = (close - ts_min(low, 60)) / (ts_max(high, 60) - ts_min(low, 60) + 0.001)
alpha = cs_rank(pos)
```

**价值（E/P）**

```
alpha = cs_rank(1 / (abs(pe_ttm) + 1))
```

缺 `pe_ttm` 的股票当天是 NaN，覆盖度会掉一截，属正常。
