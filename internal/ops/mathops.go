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
	// ---- 数学/逻辑函数（逐元素） ----

	reg("abs", 1, 1, nil, func(p *panel.Panel, cols []panel.Col, _ []float64) panel.Col {
		return elementwise1(p, cols[0], math.Abs)
	})

	reg("log", 1, 1, nil, func(p *panel.Panel, cols []panel.Col, _ []float64) panel.Col {
		return elementwise1(p, cols[0], func(x float64) float64 {
			if x <= 0 {
				return math.NaN()
			}
			return math.Log(x)
		})
	})

	reg("sqrt", 1, 1, nil, func(p *panel.Panel, cols []panel.Col, _ []float64) panel.Col {
		return elementwise1(p, cols[0], func(x float64) float64 {
			if x < 0 {
				return math.NaN()
			}
			return math.Sqrt(x)
		})
	})

	reg("sign", 1, 1, nil, func(p *panel.Panel, cols []panel.Col, _ []float64) panel.Col {
		return elementwise1(p, cols[0], func(x float64) float64 {
			if x > 0 {
				return 1
			} else if x < 0 {
				return -1
			}
			return 0
		})
	})

	reg("power", 2, 2, nil, func(p *panel.Panel, cols []panel.Col, _ []float64) panel.Col {
		return elementwise(p, cols[0], cols[1], math.Pow)
	})

	reg("max", 2, 2, nil, func(p *panel.Panel, cols []panel.Col, _ []float64) panel.Col {
		return elementwise(p, cols[0], cols[1], math.Max)
	})

	reg("min", 2, 2, nil, func(p *panel.Panel, cols []panel.Col, _ []float64) panel.Col {
		return elementwise(p, cols[0], cols[1], math.Min)
	})

	// if(cond, a, b)：cond 为非 0 非 NaN 时取 a，否则取 b（NaN 视为假）
	reg("if", 3, 3, nil, func(p *panel.Panel, cols []panel.Col, _ []float64) panel.Col {
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

	reg("and", 2, 2, nil, func(p *panel.Panel, cols []panel.Col, _ []float64) panel.Col {
		return elementwise(p, cols[0], cols[1], func(x, y float64) float64 {
			if truthy(x) && truthy(y) {
				return 1
			}
			return 0
		})
	})

	reg("or", 2, 2, nil, func(p *panel.Panel, cols []panel.Col, _ []float64) panel.Col {
		return elementwise(p, cols[0], cols[1], func(x, y float64) float64 {
			if truthy(x) || truthy(y) {
				return 1
			}
			return 0
		})
	})

	reg("not", 1, 1, nil, func(p *panel.Panel, cols []panel.Col, _ []float64) panel.Col {
		return elementwise1(p, cols[0], func(x float64) float64 {
			if truthy(x) {
				return 0
			}
			return 1
		})
	})
}
