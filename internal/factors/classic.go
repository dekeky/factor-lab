// Package factors 内置经典因子库（随服务启动自动注册，source=classic，不可删除）。
//
// 收录原则：
//   - Alpha101 子集：仅收录公式记忆有把握、且现有算子可直接表达的条目；
//     全 101 个的批量迁移与数值对照属于 M2 里程碑（见技术方案 4.2）。
//   - Alpha158 风格：K 线形态/乖离/波动类，出自微软 Qlib（MIT），公式语义自实现。
//   - 风格因子：动量/反转/波动/量价/估值/质量/规模，教科书口径。
// 字段口径：vwap = amount/vol；returns = pct_chg；volume = vol；adv20 = ts_mean(vol, 20)。
package factors

// Def 是一条内置因子定义。
type Def struct {
	Name     string
	Category string // alpha101 | alpha158 | momentum | reversal | volatility | volume | valuation | quality | size
	Expr     string
	Desc     string
}

// Classics 是内置因子全集。
var Classics = []Def{
	// ---- WorldQuant 101 Alphas（Kakushadze, 2016）可迁移子集 ----
	{
		Name:     "alpha101_001",
		Category: "alpha101",
		Expr:     "down = if(pct_chg < 0, ts_std_dev(pct_chg, 20), close)\nsq = down * down\nalpha = cs_rank(ts_arg_max(sq, 5)) - 0.5",
		Desc:     "Alpha#1: rank(Ts_ArgMax(SignedPower(((returns<0)?stddev(returns,20):close),2),5))-0.5",
	},
	{
		Name:     "alpha101_002",
		Category: "alpha101",
		Expr:     "lvol = log(vol)\ndvol = cs_rank(ts_delta(lvol, 2))\nret = cs_rank((close - open) / open)\nalpha = -1 * ts_corr(dvol, ret, 6)",
		Desc:     "Alpha#2: -1*correlation(rank(delta(log(volume),2)), rank(((close-open)/open)), 6)",
	},
	{
		Name:     "alpha101_003",
		Category: "alpha101",
		Expr:     "alpha = -1 * ts_corr(cs_rank(open), cs_rank(vol), 10)",
		Desc:     "Alpha#3: -1*correlation(rank(open), rank(volume), 10)",
	},
	{
		Name:     "alpha101_004",
		Category: "alpha101",
		Expr:     "alpha = -1 * ts_rank(cs_rank(low), 9)",
		Desc:     "Alpha#4: -1*Ts_Rank(rank(low), 9)",
	},
	{
		Name:     "alpha101_006",
		Category: "alpha101",
		Expr:     "alpha = -1 * ts_corr(open, vol, 10)",
		Desc:     "Alpha#6: -1*correlation(open, volume, 10)",
	},
	{
		Name:     "alpha101_012",
		Category: "alpha101",
		Expr:     "alpha = sign(ts_delta(vol, 1)) * (-1 * ts_delta(close, 1))",
		Desc:     "Alpha#12: sign(delta(volume,1)) * (-1*delta(close,1))",
	},
	{
		Name:     "alpha101_013",
		Category: "alpha101",
		Expr:     "alpha = -1 * cs_rank(ts_cov(cs_rank(close), cs_rank(vol), 5))",
		Desc:     "Alpha#13: -1*rank(covariance(rank(close), rank(volume), 5))",
	},
	{
		Name:     "alpha101_014",
		Category: "alpha101",
		Expr:     "alpha = -1 * cs_rank(ts_delta(pct_chg, 3)) * ts_corr(open, vol, 2)",
		Desc:     "Alpha#14: (-1*rank(delta(returns,3))) * correlation(open, volume, 2)",
	},
	{
		Name:     "alpha101_015",
		Category: "alpha101",
		Expr:     "c = ts_corr(cs_rank(high), cs_rank(vol), 3)\nalpha = -1 * ts_sum(cs_rank(c), 3)",
		Desc:     "Alpha#15: -1*sum(rank(correlation(rank(high), rank(volume), 3)), 3)",
	},
	{
		Name:     "alpha101_016",
		Category: "alpha101",
		Expr:     "alpha = -1 * cs_rank(ts_cov(cs_rank(high), cs_rank(vol), 5))",
		Desc:     "Alpha#16: -1*rank(covariance(rank(high), rank(volume), 5))",
	},
	{
		Name:     "alpha101_018",
		Category: "alpha101",
		Expr:     "co = close - open\nalpha = -1 * cs_rank(ts_std_dev(abs(co), 5) + co + ts_corr(close, open, 10))",
		Desc:     "Alpha#18: -1*rank(stddev(abs(close-open),5) + (close-open) + correlation(close,open,10))",
	},
	{
		Name:     "alpha101_019",
		Category: "alpha101",
		Expr:     "gap = close - ts_delay(close, 7)\nalpha = -1 * sign(gap + ts_delta(close, 7)) * (1 + cs_rank(1 + ts_sum(pct_chg, 250)))",
		Desc:     "Alpha#19: -1*sign((close-delay(close,7))+delta(close,7)) * (1+rank(1+sum(returns,250)))",
	},
	{
		Name:     "alpha101_028",
		Category: "alpha101",
		Expr:     "adv20 = ts_mean(vol, 20)\nmid = (high + low) / 2\nalpha = cs_scale(ts_corr(adv20, low, 5) + mid - close)",
		Desc:     "Alpha#28: scale(((correlation(adv20, low, 5) + ((high+low)/2)) - close))",
	},
	{
		Name:     "alpha101_033",
		Category: "alpha101",
		Expr:     "alpha = cs_rank(-1 * (1 - open / close))",
		Desc:     "Alpha#33: rank(-1*(1-(open/close)))",
	},
	{
		Name:     "alpha101_034",
		Category: "alpha101",
		Expr:     "alpha = 1 - cs_rank(ts_std_dev(pct_chg, 2) / ts_std_dev(pct_chg, 5)) + (1 - cs_rank(ts_delta(close, 1)))",
		Desc:     "Alpha#34: (1-rank(stddev(returns,2)/stddev(returns,5))) + (1-rank(delta(close,1)))",
	},
	{
		Name:     "alpha101_035",
		Category: "alpha101",
		Expr:     "alpha = ts_rank(vol, 32) * (1 - ts_rank(close + high - low, 16)) * (1 - ts_rank(pct_chg, 32))",
		Desc:     "Alpha#35: Ts_Rank(volume,32) * (1-Ts_Rank(((close+high)-low),16)) * (1-Ts_Rank(returns,32))",
	},
	{
		Name:     "alpha101_038",
		Category: "alpha101",
		Expr:     "alpha = -1 * cs_rank(ts_rank(close, 10)) * cs_rank(close / open)",
		Desc:     "Alpha#38: (-1*rank(Ts_Rank(close,10))) * rank((close/open))",
	},
	{
		Name:     "alpha101_041",
		Category: "alpha101",
		Expr:     "vwap = amount / vol\nalpha = sqrt(high * low) - vwap",
		Desc:     "Alpha#41: ((high*low)^0.5) - vwap",
	},
	{
		Name:     "alpha101_042",
		Category: "alpha101",
		Expr:     "vwap = amount / vol\nalpha = cs_rank(vwap - close) / cs_rank(vwap + close)",
		Desc:     "Alpha#42: rank((vwap-close)) / rank((vwap+close))",
	},
	{
		Name:     "alpha101_101",
		Category: "alpha101",
		Expr:     "alpha = (close - open) / (high - low + 0.001)",
		Desc:     "Alpha#101: ((close-open)/((high-low)+0.001))",
	},

	// ---- Qlib Alpha158 风格（K 线形态 / 乖离 / 波动） ----
	{
		Name:     "a158_kmid",
		Category: "alpha158",
		Expr:     "alpha = (close - open) / open",
		Desc:     "K线中枢方向：(close-open)/open",
	},
	{
		Name:     "a158_klen",
		Category: "alpha158",
		Expr:     "alpha = (high - low) / open",
		Desc:     "K线长度（振幅）",
	},
	{
		Name:     "a158_kup",
		Category: "alpha158",
		Expr:     "upper = max(open, close)\nalpha = (high - upper) / open",
		Desc:     "上影线长度",
	},
	{
		Name:     "a158_klow",
		Category: "alpha158",
		Expr:     "lower = min(open, close)\nalpha = (lower - low) / open",
		Desc:     "下影线长度",
	},
	{
		Name:     "a158_rsv20",
		Category: "alpha158",
		Expr:     "alpha = (close - ts_min(low, 20)) / (ts_max(high, 20) - ts_min(low, 20) + 0.001)",
		Desc:     "20 日随机指标（价格位置）",
	},
	{
		Name:     "a158_bias20",
		Category: "alpha158",
		Expr:     "alpha = close / ts_mean(close, 20) - 1",
		Desc:     "20 日乖离率",
	},
	{
		Name:     "a158_wvma20",
		Category: "alpha158",
		Expr:     "alpha = ts_std_dev(amount, 20) / (ts_mean(amount, 20) + 1)",
		Desc:     "20 日成交额波动率",
	},

	// ---- 动量 ----
	{
		Name:     "mom10",
		Category: "momentum",
		Expr:     "alpha = close / ts_delay(close, 10) - 1",
		Desc:     "10 日动量",
	},
	{
		Name:     "mom60",
		Category: "momentum",
		Expr:     "alpha = close / ts_delay(close, 60) - 1",
		Desc:     "60 日动量（中期）",
	},
	{
		Name:     "mom120",
		Category: "momentum",
		Expr:     "alpha = close / ts_delay(close, 120) - 1",
		Desc:     "120 日动量（半年维度）",
	},

	// ---- 反转 ----
	{
		Name:     "rev5",
		Category: "reversal",
		Expr:     "alpha = -1 * cs_rank(close / ts_delay(close, 5) - 1)",
		Desc:     "5 日短期反转（A 股经典负动量）",
	},
	{
		Name:     "rev20",
		Category: "reversal",
		Expr:     "alpha = -1 * cs_rank(close / ts_delay(close, 20) - 1)",
		Desc:     "20 日中期反转",
	},
	{
		Name:     "overnight_rev",
		Category: "reversal",
		Expr:     "alpha = -1 * cs_rank(open / ts_delay(close, 1) - 1)",
		Desc:     "隔夜跳空反转",
	},

	// ---- 波动率 ----
	{
		Name:     "low_vol20",
		Category: "volatility",
		Expr:     "alpha = -1 * cs_rank(ts_std_dev(pct_chg, 20))",
		Desc:     "20 日低波动异象",
	},
	{
		Name:     "vol_struct",
		Category: "volatility",
		Expr:     "alpha = ts_std_dev(pct_chg, 5) / ts_std_dev(pct_chg, 60)",
		Desc:     "波动率期限结构：短期/长期波动比",
	},
	{
		Name:     "low_amplitude20",
		Category: "volatility",
		Expr:     "amp = (high - low) / close\nalpha = -1 * cs_rank(ts_mean(amp, 20))",
		Desc:     "20 日平均振幅（低波风格）",
	},
	{
		Name:     "price_pos60",
		Category: "volatility",
		Expr:     "alpha = (close - ts_min(low, 60)) / (ts_max(high, 60) - ts_min(low, 60) + 0.001)",
		Desc:     "60 日价格位置（0=最低 1=最高）",
	},

	// ---- 量价 / 流动性 ----
	{
		Name:     "low_turn20",
		Category: "volume",
		Expr:     "alpha = -1 * cs_rank(ts_mean(turnover_rate, 20))",
		Desc:     "20 日低换手溢价（流动性溢价反转使用）",
	},
	{
		Name:     "turn_trend",
		Category: "volume",
		Expr:     "alpha = -1 * cs_rank(turnover_rate / ts_mean(turnover_rate, 20))",
		Desc:     "换手放大反转：当日/20日均值换手",
	},
	{
		Name:     "vol_surge",
		Category: "volume",
		Expr:     "alpha = -1 * cs_rank(vol / ts_mean(vol, 20))",
		Desc:     "成交量异动反转",
	},
	{
		Name:     "corr_pv20",
		Category: "volume",
		Expr:     "alpha = -1 * ts_corr(close, vol, 20)",
		Desc:     "20 日量价相关性（量价背离）",
	},

	// ---- 估值 / 质量 / 规模 ----
	{
		Name:     "ep",
		Category: "valuation",
		Expr:     "alpha = cs_rank(1 / (abs(pe_ttm) + 1))",
		Desc:     "盈利市值比 E/P（亏损用 |PE| 处理）",
	},
	{
		Name:     "bp",
		Category: "valuation",
		Expr:     "alpha = cs_rank(1 / (abs(pb) + 0.1))",
		Desc:     "账面市值比 B/P",
	},
	{
		Name:     "roe_rank",
		Category: "quality",
		Expr:     "alpha = cs_rank(roe)",
		Desc:     "ROE 质量因子",
	},
	{
		Name:     "roe_stable",
		Category: "quality",
		Expr:     "alpha = cs_rank(roe / (ts_std_dev(roe, 60) + 0.001))",
		Desc:     "高且稳的 ROE（信息比率口径）",
	},
	{
		Name:     "small_size",
		Category: "size",
		Expr:     "alpha = -1 * cs_rank(log(total_mv))",
		Desc:     "小市值（规模因子，长期溢价方向）",
	},
}
