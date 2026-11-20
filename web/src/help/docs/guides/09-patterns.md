---
id: patterns
level: 进阶
minutes: 5
title: 常用因子模式速查
summary: 动量、反转、低波动、量价、乖离、价格位置——可以直接复制后改写的模板。
keywords: 模板 动量 反转 低波动 量价背离 均线乖离 价格位置 示例 复制
---

每个模板下面的一句话是它想表达的想法。复制到「因子工厂」后，可以试着改窗口、改方向，观察评价结果怎么变化。

#### 中期动量

最近 20 天涨得多的股票，分数更高。

```
ret20 = close / ts_delay(close, 20) - 1
alpha = cs_rank(ret20)
```

#### 短期反转

最近 5 天涨得多的股票，分数更低（赌它回调）。

```
ret5 = close / ts_delay(close, 5) - 1
alpha = -1 * cs_rank(ret5)
```

#### 低波动

最近 20 天涨跌起伏越小的股票，分数更高。

```
vol20 = ts_std_dev(pct_chg, 20)
alpha = -1 * cs_rank(vol20)
```

#### 量价背离

最近 20 天价格和成交量越是反着走，分数更高。

```
alpha = -1 * ts_corr(close, vol, 20)
```

#### 均线乖离

当前价格比 20 日均价高出多少。

```
alpha = close / ts_mean(close, 20) - 1
```

#### 价格位置

当前价格处在最近 60 天最低到最高之间的什么位置。

```
pos = (close - ts_min(low, 60)) / (ts_max(high, 60) - ts_min(low, 60) + 0.001)
alpha = cs_rank(pos)
```

更多完整模板见页面下方的“内置因子”，其中包含 WorldQuant Alpha101 和 Qlib Alpha158 的可迁移子集。
