package api

import (
	"fmt"
	"net/http"
	"sort"

	"factor-lab/internal/dsl"
	"factor-lab/internal/factors"
	"factor-lab/internal/ops"
	"factor-lab/internal/panel"
)

type helpDSL struct {
	StatementSeparators []string `json:"statement_separators"`
	Comment             string   `json:"comment"`
	Result              string   `json:"result"`
	Precedence          []string `json:"precedence"`
	MaxWindow           int      `json:"max_window"`
	MaxDepth            int      `json:"max_depth"`
}

type helpField struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Unit        string `json:"unit,omitempty"`
	Tip         string `json:"tip,omitempty"` // 面向新手的通俗解释
}

type helpOperator struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Signature   string `json:"signature"`
	Description string `json:"description"`
	Example     string `json:"example"`
	Tip         string `json:"tip,omitempty"` // 面向新手的通俗解释
}

type helpBuiltin struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Expr        string `json:"expr"`
	Description string `json:"description"`
}

type helpDoc struct {
	DSL       helpDSL        `json:"dsl"`
	Fields    []helpField    `json:"fields"`
	Operators []helpOperator `json:"operators"`
	Builtins  []helpBuiltin  `json:"builtins"`
}

var fieldDocs = map[string]helpField{
	"open":          {Description: "当日开盘价", Unit: "元", Tip: "当天第一笔成交的价格。常和 close 比较，判断当天是高开低走还是低开高走。"},
	"high":          {Description: "当日最高价", Unit: "元", Tip: "当天成交过的最高价。常和 low 一起衡量当天的振幅。"},
	"low":           {Description: "当日最低价", Unit: "元", Tip: "当天成交过的最低价。"},
	"close":         {Description: "当日收盘价（未复权口径取决于数据源）", Unit: "元", Tip: "当天最后的成交价，也是最常用的价格字段；计算涨幅、均线基本都用它。"},
	"pct_chg":       {Description: "当日涨跌幅", Unit: "%", Tip: "今天收盘价比昨天收盘价涨或跌了百分之几；数值 2 表示上涨 2%，-3 表示下跌 3%。"},
	"vol":           {Description: "成交量", Unit: "手", Tip: "当天一共成交了多少手（1 手 = 100 股），反映交易的活跃程度。"},
	"amount":        {Description: "成交额", Unit: "千元", Tip: "当天成交的总金额。比成交量更能反映有多少资金参与。"},
	"turnover_rate": {Description: "换手率", Unit: "%", Tip: "当天成交的股数占可流通股数的比例；越高说明换手越频繁、交易越热。"},
	"pe_ttm":        {Description: "滚动市盈率（TTM）", Unit: "倍", Tip: "股价 ÷ 最近 12 个月的每股盈利。越低通常代表越“便宜”；亏损公司没有这个值。"},
	"pb":            {Description: "市净率", Unit: "倍", Tip: "股价 ÷ 每股净资产。越低代表相对公司账面资产越便宜。"},
	"total_mv":      {Description: "总市值", Unit: "万元", Tip: "公司全部股份按当前股价计算的总价值，常用来区分大盘股和小盘股。"},
	"circ_mv":       {Description: "流通市值", Unit: "万元", Tip: "只统计可以在市场上自由买卖的那部分股份的市值。"},
	"roe":           {Description: "净资产收益率；按数据源最近可用财报对齐", Unit: "%", Tip: "公司用股东投入的钱一年能赚回百分之几，衡量赚钱效率；越高通常代表盈利能力越强。"},
}

var operatorDocs = map[string]helpOperator{
	"ts_delay":        {Category: "时序", Signature: "ts_delay(x, window)", Description: "取当前观测向前 window 个交易日的值。", Example: "ts_delay(close, 5)", Tip: "“n 天前是多少”。和今天的值相除或相减，就能算出这段时间的变化。"},
	"ts_delta":        {Category: "时序", Signature: "ts_delta(x, window)", Description: "当前值减去 window 个交易日前的值。", Example: "ts_delta(close, 5)", Tip: "“比 n 天前多了多少”，等价于 x - ts_delay(x, n)。"},
	"ts_sum":          {Category: "时序", Signature: "ts_sum(x, window)", Description: "最近 window 个交易日的滚动求和，忽略缺失值。", Example: "ts_sum(pct_chg, 20)", Tip: "把最近 n 天的值加起来，例如最近 20 天的累计涨跌幅。"},
	"ts_mean":         {Category: "时序", Signature: "ts_mean(x, window)", Description: "最近 window 个交易日的滚动均值，忽略缺失值。", Example: "ts_mean(close, 20)", Tip: "最近 n 天的平均值，也就是常说的“n 日均线”。"},
	"ts_std_dev":      {Category: "时序", Signature: "ts_std_dev(x, window)", Description: "最近 window 个交易日的样本标准差。", Example: "ts_std_dev(pct_chg, 20)", Tip: "最近 n 天波动有多大。数值越大说明越不稳定，常用来衡量风险。"},
	"ts_max":          {Category: "时序", Signature: "ts_max(x, window)", Description: "最近 window 个交易日的最大值。", Example: "ts_max(high, 20)", Tip: "最近 n 天里的最高点，例如 ts_max(high, 20) 就是 20 日最高价。"},
	"ts_min":          {Category: "时序", Signature: "ts_min(x, window)", Description: "最近 window 个交易日的最小值。", Example: "ts_min(low, 20)", Tip: "最近 n 天里的最低点，例如 ts_min(low, 20) 就是 20 日最低价。"},
	"ts_arg_max":      {Category: "时序", Signature: "ts_arg_max(x, window)", Description: "窗口最大值的位置；最旧观测为 0。", Example: "ts_arg_max(close, 10)", Tip: "最高点出现在窗口里的第几天：0 表示最早那天，window-1 表示今天。数值越大，高点离今天越近。"},
	"ts_rank":         {Category: "时序", Signature: "ts_rank(x, window)", Description: "当前值在自身滚动窗口内的升序百分位，范围 0~1。", Example: "ts_rank(close, 20)", Tip: "今天的值在自己最近 n 天里排第几：0 是最低，1 是最高。用来判断“相对自己的历史算高还是低”。"},
	"ts_zscore":       {Category: "时序", Signature: "ts_zscore(x, window)", Description: "当前值相对滚动窗口均值和样本标准差的标准分。", Example: "ts_zscore(pct_chg, 20)", Tip: "今天的值偏离自己近期平均水平多少个标准差。大于 2 或小于 -2 通常算明显异常。"},
	"ts_decay_linear": {Category: "时序", Signature: "ts_decay_linear(x, window)", Description: "线性衰减加权均值，越新的观测权重越高。", Example: "ts_decay_linear(close, 10)", Tip: "一种“越近越重要”的平均值。常用来平滑信号、减少频繁换股。"},
	"ts_corr":         {Category: "时序", Signature: "ts_corr(x, y, window)", Description: "两个序列在滚动窗口内的皮尔逊相关系数。", Example: "ts_corr(close, vol, 20)", Tip: "两个指标最近 n 天是否同涨同跌：接近 1 表示同向，接近 -1 表示反向，接近 0 表示没关系。"},
	"ts_cov":          {Category: "时序", Signature: "ts_cov(x, y, window)", Description: "两个序列在滚动窗口内的样本协方差。", Example: "ts_cov(close, vol, 20)", Tip: "和相关系数类似，但数值大小会受单位影响。多数情况下优先用 ts_corr。"},
	"cs_rank":         {Category: "截面", Signature: "cs_rank(x)", Description: "每个交易日内跨股票升序排名并归一到 0~1，并列取平均秩。", Example: "cs_rank(roe)", Tip: "把当天所有股票从低到高排队，最低的得 0、最高的得 1。最常用的收尾步骤，可以消除单位差异和极端值。"},
	"cs_zscore":       {Category: "截面", Signature: "cs_zscore(x)", Description: "每个交易日内跨股票标准化。", Example: "cs_zscore(total_mv)", Tip: "当天每只股票比全体平均水平高或低多少个标准差。保留了差距大小，但容易被极端值影响。"},
	"cs_demean":       {Category: "截面", Signature: "cs_demean(x)", Description: "每个交易日内减去截面均值。", Example: "cs_demean(pct_chg)", Tip: "当天每只股票减去全体平均值，结果表示“比平均水平高多少”。"},
	"cs_scale":        {Category: "截面", Signature: "cs_scale(x)", Description: "每个交易日内按绝对值之和缩放。", Example: "cs_scale(close)", Tip: "把当天所有股票的值等比例缩放，使绝对值加起来等于 1，可以理解成换算成“仓位占比”。"},
	"abs":             {Category: "数学", Signature: "abs(x)", Description: "逐元素绝对值。", Example: "abs(pct_chg)", Tip: "去掉正负号，只看大小。例如 abs(pct_chg) 表示“不管涨跌，波动了多少”。"},
	"log":             {Category: "数学", Signature: "log(x)", Description: "逐元素自然对数；x≤0 时结果为缺失值。", Example: "log(total_mv)", Tip: "把相差悬殊的数字拉近，适合市值、成交额这类大小差别极大的数据。"},
	"sqrt":            {Category: "数学", Signature: "sqrt(x)", Description: "逐元素平方根；x<0 时结果为缺失值。", Example: "sqrt(total_mv)", Tip: "开平方。作用类似 log，但压缩得更温和。"},
	"sign":            {Category: "数学", Signature: "sign(x)", Description: "逐元素符号：正数为 1，负数为 -1，零为 0。", Example: "sign(pct_chg)", Tip: "只保留方向：涨记 1、跌记 -1、不变记 0。"},
	"power":           {Category: "数学", Signature: "power(x, y)", Description: "逐元素计算 x 的 y 次幂。", Example: "power(pct_chg, 2)", Tip: "求幂。power(x, 2) 就是平方，会放大数值之间的差距。"},
	"max":             {Category: "数学", Signature: "max(x, y)", Description: "逐元素取较大值。", Example: "max(open, close)", Tip: "两个值里取大的那个。例如 max(open, close) 是当天 K 线实体的上沿。"},
	"min":             {Category: "数学", Signature: "min(x, y)", Description: "逐元素取较小值。", Example: "min(open, close)", Tip: "两个值里取小的那个。例如 min(open, close) 是当天 K 线实体的下沿。"},
	"if":              {Category: "条件", Signature: "if(condition, when_true, when_false)", Description: "条件非零且非缺失时取第二个参数，否则取第三个参数。", Example: "if(pct_chg > 0, vol, 0)", Tip: "“如果…就…否则…”。例如只在上涨的日子取成交量，其他日子记为 0。"},
}

func buildHelpDoc(p *panel.Panel) helpDoc {
	doc := helpDoc{
		DSL: helpDSL{
			StatementSeparators: []string{"换行", "分号 ;"},
			Comment:             "# 到行尾",
			Result:              "最后一条语句的值作为因子结果",
			Precedence:          []string{"()", "一元 - / not", "* /", "+ -", "< <= > >=", "== !=", "and", "or"},
			MaxWindow:           dsl.MaxWindow,
			MaxDepth:            64,
		},
	}

	for _, name := range p.Fields() {
		if hiddenPanelFields[name] {
			continue
		}
		item := fieldDocs[name]
		item.Name = name
		if item.Description == "" {
			item.Description = "当前数据面板提供的自定义字段；具体口径由数据源或 CSV 表头定义。"
		}
		doc.Fields = append(doc.Fields, item)
	}

	sigs := ops.Sigs()
	names := make([]string, 0, len(sigs))
	for name := range sigs {
		if name != "and" && name != "or" && name != "not" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		item, ok := operatorDocs[name]
		if !ok {
			sig := sigs[name]
			item = helpOperator{
				Category:    "其他",
				Signature:   fmt.Sprintf("%s(%d~%d 个参数)", name, sig.MinArgs, sig.MaxArgs),
				Description: "当前计算引擎注册的算子。",
				Example:     name + "(...)",
			}
		}
		item.Name = name
		doc.Operators = append(doc.Operators, item)
	}

	for _, item := range factors.Classics {
		doc.Builtins = append(doc.Builtins, helpBuiltin{
			Name: item.Name, Category: item.Category, Expr: item.Expr, Description: item.Desc,
		})
	}
	return doc
}

func (s *Server) handleHelp(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, buildHelpDoc(s.panel()))
}
