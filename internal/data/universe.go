package data

import "strings"

// Universe 是评价可选的指数成分股票池。
type Universe struct {
	Code  string `json:"code"`
	Name  string `json:"name"`
	Group string `json:"group"`
}

// AllMarket 是内存面板的全 A 股股票池标识（不是某一指数成分）。
const AllMarket = "ALL"

// Universes 是界面股票池下拉的常用指数（Tushare index_weight 代码）。
var Universes = []Universe{
	{Code: AllMarket, Name: "全市场", Group: "沪深核心"},
	{Code: "000300.SH", Name: "沪深300", Group: "沪深核心"},
	{Code: "000905.SH", Name: "中证500", Group: "沪深核心"},
	{Code: "000906.SH", Name: "中证800", Group: "沪深核心"},
	{Code: "000852.SH", Name: "中证1000", Group: "沪深核心"},
	{Code: "000510.SH", Name: "中证A500", Group: "沪深核心"},
	{Code: "000985.SH", Name: "中证全指", Group: "沪深核心"},
	{Code: "000016.SH", Name: "上证50", Group: "上证 / 科创"},
	{Code: "000010.SH", Name: "上证180", Group: "上证 / 科创"},
	{Code: "000688.SH", Name: "科创50", Group: "上证 / 科创"},
	{Code: "000698.SH", Name: "科创100", Group: "上证 / 科创"},
	{Code: "399001.SZ", Name: "深证成指", Group: "深证 / 创业板"},
	{Code: "399330.SZ", Name: "深证100", Group: "深证 / 创业板"},
	{Code: "399006.SZ", Name: "创业板指", Group: "深证 / 创业板"},
	{Code: "399673.SZ", Name: "创业板50", Group: "深证 / 创业板"},
}

// CanonicalIndex 归一化指数代码：去空白、大写，沪深300 深市代码并入沪市。
func CanonicalIndex(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == csi300SZ {
		return defaultIndex
	}
	if code == AllMarket {
		return AllMarket
	}
	return code
}

// UniverseName 返回已知指数的中文名；未知则原样返回代码。
func UniverseName(code string) string {
	code = CanonicalIndex(code)
	for _, u := range Universes {
		if u.Code == code {
			return u.Name
		}
	}
	if code == "" {
		return ""
	}
	return code
}
