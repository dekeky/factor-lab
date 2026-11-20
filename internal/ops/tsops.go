package ops

import (
	"math"

	"factor-lab/internal/panel"
)

// tsApply 是滚动窗口通用骨架：对每只股票 j、每个交易日 i，
// 收集窗口 [i-n+1, i] 内的非 NaN 值交给聚合函数。
// 聚合函数自行决定最少样本数（不足返回 NaN）。
func tsApply(p *panel.Panel, x panel.Col, n int, agg func(vals []float64, i, j int) float64) panel.Col {
	T, N := p.T(), p.N()
	out := make(panel.Col, T*N)
	buf := make([]float64, 0, 64)
	for i := 0; i < T; i++ {
		lo := i - n + 1
		for j := 0; j < N; j++ {
			idx := i*N + j
			if lo < 0 {
				out[idx] = math.NaN()
				continue
			}
			buf = buf[:0]
			for k := lo; k <= i; k++ {
				v := x[k*N+j]
				if !math.IsNaN(v) {
					buf = append(buf, v)
				}
			}
			out[idx] = agg(buf, i, j)
		}
	}
	return out
}

func tsPairs(p *panel.Panel, x, y panel.Col, n int, agg func(xs, ys []float64) float64) panel.Col {
	T, N := p.T(), p.N()
	out := make(panel.Col, T*N)
	xs := make([]float64, 0, 64)
	ys := make([]float64, 0, 64)
	for i := 0; i < T; i++ {
		lo := i - n + 1
		for j := 0; j < N; j++ {
			idx := i*N + j
			if lo < 0 {
				out[idx] = math.NaN()
				continue
			}
			xs = xs[:0]
			ys = ys[:0]
			for k := lo; k <= i; k++ {
				a, b := x[k*N+j], y[k*N+j]
				if !math.IsNaN(a) && !math.IsNaN(b) {
					xs = append(xs, a)
					ys = append(ys, b)
				}
			}
			out[idx] = agg(xs, ys)
		}
	}
	return out
}

func mean(vals []float64) float64 {
	if len(vals) == 0 {
		return math.NaN()
	}
	s := 0.0
	for _, v := range vals {
		s += v
	}
	return s / float64(len(vals))
}

func sampleStd(vals []float64) float64 {
	if len(vals) < 2 {
		return math.NaN()
	}
	m := mean(vals)
	s := 0.0
	for _, v := range vals {
		d := v - m
		s += d * d
	}
	return math.Sqrt(s / float64(len(vals)-1))
}

func pearson(xs, ys []float64) float64 {
	// 两点相关 = ±1（与 pandas rolling corr 口径一致：窗口 2 时结果有定义）
	if len(xs) < 2 {
		return math.NaN()
	}
	mx, my := mean(xs), mean(ys)
	sxy, sxx, syy := 0.0, 0.0, 0.0
	for k := range xs {
		dx, dy := xs[k]-mx, ys[k]-my
		sxy += dx * dy
		sxx += dx * dx
		syy += dy * dy
	}
	if sxx == 0 || syy == 0 {
		return math.NaN()
	}
	return sxy / math.Sqrt(sxx*syy)
}

func init() {
	// ---- 时序算子（沿单只股票的时间轴滚动；最后一个参数是窗口长度常数） ----

	reg("ts_delay", 2, 2, []int{1}, func(p *panel.Panel, cols []panel.Col, consts []float64) panel.Col {
		x := cols[0]
		n := int(consts[1])
		T, N := p.T(), p.N()
		out := make(panel.Col, T*N)
		for i := 0; i < T; i++ {
			for j := 0; j < N; j++ {
				if i-n < 0 {
					out[i*N+j] = math.NaN()
				} else {
					out[i*N+j] = x[(i-n)*N+j]
				}
			}
		}
		return out
	})

	reg("ts_delta", 2, 2, []int{1}, func(p *panel.Panel, cols []panel.Col, consts []float64) panel.Col {
		x := cols[0]
		n := int(consts[1])
		T, N := p.T(), p.N()
		out := make(panel.Col, T*N)
		for i := 0; i < T; i++ {
			for j := 0; j < N; j++ {
				if i-n < 0 {
					out[i*N+j] = math.NaN()
				} else {
					out[i*N+j] = x[i*N+j] - x[(i-n)*N+j]
				}
			}
		}
		return out
	})

	reg("ts_sum", 2, 2, []int{1}, func(p *panel.Panel, cols []panel.Col, consts []float64) panel.Col {
		return tsApply(p, cols[0], int(consts[1]), func(vals []float64, _, _ int) float64 {
			if len(vals) == 0 {
				return math.NaN()
			}
			s := 0.0
			for _, v := range vals {
				s += v
			}
			return s
		})
	})

	reg("ts_mean", 2, 2, []int{1}, func(p *panel.Panel, cols []panel.Col, consts []float64) panel.Col {
		return tsApply(p, cols[0], int(consts[1]), func(vals []float64, _, _ int) float64 {
			return mean(vals)
		})
	})

	reg("ts_std_dev", 2, 2, []int{1}, func(p *panel.Panel, cols []panel.Col, consts []float64) panel.Col {
		return tsApply(p, cols[0], int(consts[1]), func(vals []float64, _, _ int) float64 {
			return sampleStd(vals)
		})
	})

	reg("ts_max", 2, 2, []int{1}, func(p *panel.Panel, cols []panel.Col, consts []float64) panel.Col {
		return tsApply(p, cols[0], int(consts[1]), func(vals []float64, _, _ int) float64 {
			if len(vals) == 0 {
				return math.NaN()
			}
			m := vals[0]
			for _, v := range vals[1:] {
				if v > m {
					m = v
				}
			}
			return m
		})
	})

	reg("ts_min", 2, 2, []int{1}, func(p *panel.Panel, cols []panel.Col, consts []float64) panel.Col {
		return tsApply(p, cols[0], int(consts[1]), func(vals []float64, _, _ int) float64 {
			if len(vals) == 0 {
				return math.NaN()
			}
			m := vals[0]
			for _, v := range vals[1:] {
				if v < m {
					m = v
				}
			}
			return m
		})
	})

	reg("ts_arg_max", 2, 2, []int{1}, func(p *panel.Panel, cols []panel.Col, consts []float64) panel.Col {
		x := cols[0]
		n := int(consts[1])
		T, N := p.T(), p.N()
		out := make(panel.Col, T*N)
		for i := 0; i < T; i++ {
			lo := i - n + 1
			for j := 0; j < N; j++ {
				idx := i*N + j
				if lo < 0 {
					out[idx] = math.NaN()
					continue
				}
				best, arg := math.NaN(), -1
				for k := lo; k <= i; k++ {
					v := x[k*N+j]
					if math.IsNaN(v) {
						continue
					}
					if math.IsNaN(best) || v > best {
						best, arg = v, k-lo
					}
				}
				if arg < 0 {
					out[idx] = math.NaN()
				} else {
					out[idx] = float64(arg)
				}
			}
		}
		return out
	})

	reg("ts_rank", 2, 2, []int{1}, func(p *panel.Panel, cols []panel.Col, consts []float64) panel.Col {
		x := cols[0]
		return tsApply(p, x, int(consts[1]), func(vals []float64, i, j int) float64 {
			cur := x[i*p.N()+j] // 当前值必须取 x[i] 本身（NaN 时直接无效）
			if math.IsNaN(cur) {
				return math.NaN()
			}
			m := len(vals)
			if m < 2 {
				return math.NaN()
			}
			less := 0
			for _, v := range vals {
				if v < cur {
					less++
				}
			}
			return float64(less) / float64(m-1)
		})
	})

	reg("ts_zscore", 2, 2, []int{1}, func(p *panel.Panel, cols []panel.Col, consts []float64) panel.Col {
		x := cols[0]
		return tsApply(p, x, int(consts[1]), func(vals []float64, i, j int) float64 {
			cur := x[i*p.N()+j]
			if math.IsNaN(cur) {
				return math.NaN()
			}
			if len(vals) < 2 {
				return math.NaN()
			}
			m, s := mean(vals), sampleStd(vals)
			if s == 0 || math.IsNaN(s) {
				return math.NaN()
			}
			return (cur - m) / s
		})
	})

	reg("ts_decay_linear", 2, 2, []int{1}, func(p *panel.Panel, cols []panel.Col, consts []float64) panel.Col {
		x := cols[0]
		n := int(consts[1])
		T, N := p.T(), p.N()
		out := make(panel.Col, T*N)
		for i := 0; i < T; i++ {
			lo := i - n + 1
			for j := 0; j < N; j++ {
				idx := i*N + j
				if lo < 0 {
					out[idx] = math.NaN()
					continue
				}
				// 权重按相对年龄：最新观测权重 = n，最旧 = 1（窗口前部不足时按实际年龄）
				sw, sv := 0.0, 0.0
				for k := lo; k <= i; k++ {
					v := x[k*N+j]
					if math.IsNaN(v) {
						continue
					}
					w := float64(k - lo + 1)
					sv += w * v
					sw += w
				}
				if sw == 0 {
					out[idx] = math.NaN()
				} else {
					out[idx] = sv / sw
				}
			}
		}
		return out
	})

	reg("ts_corr", 3, 3, []int{2}, func(p *panel.Panel, cols []panel.Col, consts []float64) panel.Col {
		return tsPairs(p, cols[0], cols[1], int(consts[2]), pearson)
	})

	reg("ts_cov", 3, 3, []int{2}, func(p *panel.Panel, cols []panel.Col, consts []float64) panel.Col {
		return tsPairs(p, cols[0], cols[1], int(consts[2]), func(xs, ys []float64) float64 {
			if len(xs) < 2 {
				return math.NaN()
			}
			mx, my := mean(xs), mean(ys)
			s := 0.0
			for k := range xs {
				s += (xs[k] - mx) * (ys[k] - my)
			}
			return s / float64(len(xs)-1)
		})
	})
}
