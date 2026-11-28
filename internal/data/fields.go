package data

import "factor-lab/internal/panel"

// HiddenFields 是不对用户和助手展示的内部面板列（复权底稿、停牌、涨跌停价等）。
var HiddenFields = map[string]bool{
	"raw_close": true, "raw_open": true, "qfq_open": true, "qfq_close": true,
	"suspend": true, "up_limit": true, "down_limit": true,
}

// Field 是一条面板字段的口径说明，和加载列定义放在一起。
type Field struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Unit        string `json:"unit,omitempty"`
	Tip         string `json:"tip,omitempty"`
}

var fieldDocs = map[string]Field{
	"open":            {Description: "当日开盘价", Unit: "元", Tip: "当天第一笔成交的价格。常和 close 比较，判断当天是高开低走还是低开高走。"},
	"high":            {Description: "当日最高价", Unit: "元", Tip: "当天成交过的最高价。常和 low 一起衡量当天的振幅。"},
	"low":             {Description: "当日最低价", Unit: "元", Tip: "当天成交过的最低价。"},
	"close":           {Description: "当日收盘价；Tushare 源为后复权", Unit: "元", Tip: "Tushare 按后复权写入，便于跨除权日比较。CSV 源取决于导入列本身的口径。"},
	"pct_chg":         {Description: "由后复权 close 计算的日收益率", Unit: "小数", Tip: "close(t)/close(t-1)−1。0.02 表示上涨 2%，不是百分数 2。不是 Tushare 原始 pct_chg 字段。"},
	"vol":             {Description: "成交量", Unit: "手", Tip: "当天一共成交了多少手（1 手 = 100 股），反映交易的活跃程度。"},
	"amount":          {Description: "成交额", Unit: "千元", Tip: "当天成交的总金额。比成交量更能反映有多少资金参与。"},
	"turnover_rate":   {Description: "换手率（成交量/无限售流通股）", Unit: "%", Tip: "当天成交的股数占无限售流通股的比例；越高说明换手越频繁、交易越热。"},
	"turnover_rate_f": {Description: "换手率（成交量/自由流通股）", Unit: "%", Tip: "分母是自由流通股本，比 turnover_rate 更贴近真正能买卖的那部分股份。"},
	"volume_ratio":    {Description: "量比", Unit: "倍", Tip: "当天成交量相对近期平均成交量的倍数；大于 1 表示比平时放量。"},
	"pe":              {Description: "市盈率（总市值/净利润）", Unit: "倍", Tip: "用最新年报净利润。亏损公司没有这个值；跨期比较更常用 pe_ttm。"},
	"pe_ttm":          {Description: "滚动市盈率（TTM）", Unit: "倍", Tip: "股价 ÷ 最近 12 个月的每股盈利。越低通常代表越“便宜”；亏损公司没有这个值。"},
	"pb":              {Description: "市净率", Unit: "倍", Tip: "股价 ÷ 每股净资产。越低代表相对公司账面资产越便宜。"},
	"ps":              {Description: "市销率（总市值/最新年报营业收入）", Unit: "倍", Tip: "亏损时 PE 为空，仍可用市销率比较收入规模。"},
	"ps_ttm":          {Description: "滚动市销率（TTM）", Unit: "倍", Tip: "总市值 ÷ 最近 12 个月营业收入。"},
	"dv_ratio":        {Description: "股息率（去年除息派现）", Unit: "%", Tip: "按去年期间除息的分红相对现价。没分红或数据空则为缺失。"},
	"dv_ttm":          {Description: "滚动股息率（TTM）", Unit: "%", Tip: "近 12 个月已除息且报告期也在 12 个月内的派现相对现价。"},
	"total_share":     {Description: "总股本", Unit: "万股", Tip: "公司全部股份数量。和 close 相乘可核对总市值口径。"},
	"float_share":     {Description: "流通股本", Unit: "万股", Tip: "可以在市场上流通的股份数量。"},
	"free_share":      {Description: "自由流通股本", Unit: "万股", Tip: "扣掉大股东等锁定后、真正能自由买卖的股本。"},
	"total_mv":        {Description: "总市值", Unit: "万元", Tip: "公司全部股份按当前股价计算的总价值，常用来区分大盘股和小盘股。"},
	"circ_mv":         {Description: "流通市值", Unit: "万元", Tip: "只统计可以在市场上自由买卖的那部分股份的市值。"},
	"limit_status":    {Description: "收盘涨跌状态", Unit: "编码", Tip: "0 平盘，1 上涨，2 涨停，3 一字涨停，4 下跌，5 跌停，6 一字跌停。不是涨跌停价格。"},
	"roe":             {Description: "净资产收益率；按数据源最近可用财报对齐", Unit: "%", Tip: "公司用股东投入的钱一年能赚回百分之几，衡量赚钱效率；越高通常代表盈利能力越强。"},
}

// FieldInfo 返回内置字段说明（不含当前面板是否真有这一列）。
func FieldInfo(name string) (Field, bool) {
	item, ok := fieldDocs[name]
	item.Name = name
	return item, ok
}

// DescribeFields 列出当前面板对用户可见的字段（跳过内部列），按名称排序。
func DescribeFields(p *panel.Panel) []Field {
	if p == nil {
		return nil
	}
	out := make([]Field, 0, len(p.Cols))
	for _, name := range p.Fields() {
		if HiddenFields[name] {
			continue
		}
		item := fieldDocs[name]
		item.Name = name
		if item.Description == "" {
			item.Description = "当前数据面板提供的自定义字段；具体口径由数据源或 CSV 表头定义。"
		}
		out = append(out, item)
	}
	return out
}
