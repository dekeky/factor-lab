package ops

import (
	"math"

	"factor-lab/internal/panel"
)

// elementwise 对等长列做逐元素二元运算（NaN 传播）。
func elementwise(p *panel.Panel, a, b panel.Col, f func(x, y float64) float64) panel.Col {
	out := make(panel.Col, p.Size())
	for k := range out {
		x, y := a[k], b[k]
		if math.IsNaN(x) || math.IsNaN(y) {
			out[k] = math.NaN()
			continue
		}
		out[k] = f(x, y)
	}
	return out
}

// elementwise1 一元逐元素运算。
func elementwise1(p *panel.Panel, a panel.Col, f func(x float64) float64) panel.Col {
	out := make(panel.Col, p.Size())
	for k := range out {
		x := a[k]
		if math.IsNaN(x) {
			out[k] = math.NaN()
			continue
		}
		out[k] = f(x)
	}
	return out
}

// truthy：非 NaN 且非 0。
func truthy(x float64) bool { return !math.IsNaN(x) && x != 0 }

func init() {
	mathDoc := func(sig, desc, example, tip string) Doc {
		return Doc{Category: "数学", Signature: sig, Description: desc, Example: example, Tip: tip}
	}

	reg("abs", 1, 1, nil, mathDoc("abs(x)", "逐元素绝对值。", "abs(pct_chg)", "去掉正负号，只看大小。例如 abs(pct_chg) 表示“不管涨跌，波动了多少”。"), func(p *panel.Panel, cols []panel.Col, _ []float64) panel.Col {
		return elementwise1(p, cols[0], math.Abs)
	})

	reg("log", 1, 1, nil, mathDoc("log(x)", "逐元素自然对数；x≤0 时结果为缺失值。", "log(total_mv)", "把相差悬殊的数字拉近，适合市值、成交额这类大小差别极大的数据。"), func(p *panel.Panel, cols []panel.Col, _ []float64) panel.Col {
		return elementwise1(p, cols[0], func(x float64) float64 {
			if x <= 0 {
				return math.NaN()
			}
			return math.Log(x)
		})
	})

	reg("sqrt", 1, 1, nil, mathDoc("sqrt(x)", "逐元素平方根；x<0 时结果为缺失值。", "sqrt(total_mv)", "开平方。作用类似 log，但压缩得更温和。"), func(p *panel.Panel, cols []panel.Col, _ []float64) panel.Col {
		return elementwise1(p, cols[0], func(x float64) float64 {
			if x < 0 {
				return math.NaN()
			}
			return math.Sqrt(x)
		})
	})

	reg("sign", 1, 1, nil, mathDoc("sign(x)", "逐元素符号：正数为 1，负数为 -1，零为 0。", "sign(pct_chg)", "只保留方向：涨记 1、跌记 -1、不变记 0。"), func(p *panel.Panel, cols []panel.Col, _ []float64) panel.Col {
		return elementwise1(p, cols[0], func(x float64) float64 {
			if x > 0 {
				return 1
			} else if x < 0 {
				return -1
			}
			return 0
		})
	})

	reg("power", 2, 2, nil, mathDoc("power(x, y)", "逐元素计算 x 的 y 次幂。", "power(pct_chg, 2)", "求幂。power(x, 2) 就是平方，会放大数值之间的差距。"), func(p *panel.Panel, cols []panel.Col, _ []float64) panel.Col {
		return elementwise(p, cols[0], cols[1], math.Pow)
	})

	reg("max", 2, 2, nil, mathDoc("max(x, y)", "逐元素取较大值。", "max(open, close)", "两个值里取大的那个。例如 max(open, close) 是当天 K 线实体的上沿。"), func(p *panel.Panel, cols []panel.Col, _ []float64) panel.Col {
		return elementwise(p, cols[0], cols[1], math.Max)
	})

	reg("min", 2, 2, nil, mathDoc("min(x, y)", "逐元素取较小值。", "min(open, close)", "两个值里取小的那个。例如 min(open, close) 是当天 K 线实体的下沿。"), func(p *panel.Panel, cols []panel.Col, _ []float64) panel.Col {
		return elementwise(p, cols[0], cols[1], math.Min)
	})

	reg("if", 3, 3, nil, Doc{Category: "条件", Signature: "if(condition, when_true, when_false)", Description: "条件非零且非缺失时取第二个参数，否则取第三个参数。", Example: "if(pct_chg > 0, vol, 0)", Tip: "“如果…就…否则…”。例如只在上涨的日子取成交量，其他日子记为 0。"}, func(p *panel.Panel, cols []panel.Col, _ []float64) panel.Col {
		c, a, b := cols[0], cols[1], cols[2]
		out := make(panel.Col, p.Size())
		for k := range out {
			if truthy(c[k]) {
				out[k] = a[k]
			} else {
				out[k] = b[k]
			}
		}
		return out
	})

	kw := Doc{Keyword: true}
	reg("and", 2, 2, nil, kw, func(p *panel.Panel, cols []panel.Col, _ []float64) panel.Col {
		return elementwise(p, cols[0], cols[1], func(x, y float64) float64 {
			if truthy(x) && truthy(y) {
				return 1
			}
			return 0
		})
	})
	reg("or", 2, 2, nil, kw, func(p *panel.Panel, cols []panel.Col, _ []float64) panel.Col {
		return elementwise(p, cols[0], cols[1], func(x, y float64) float64 {
			if truthy(x) || truthy(y) {
				return 1
			}
			return 0
		})
	})
	reg("not", 1, 1, nil, kw, func(p *panel.Panel, cols []panel.Col, _ []float64) panel.Col {
		return elementwise1(p, cols[0], func(x float64) float64 {
			if truthy(x) {
				return 0
			}
			return 1
		})
	})
}
