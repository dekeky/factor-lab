package ops

import (
	"math"
	"sort"

	"factor-lab/internal/panel"
)

// csApply 是截面算子通用骨架：对每个交易日 i，把当日 N 只股票的值
// 交给变换函数（输入已剔除 NaN，输出需回填到原位置，NaN 位置保持 NaN）。
func csApply(p *panel.Panel, x panel.Col, f func(vals []float64) []float64) panel.Col {
	T, N := p.T(), p.N()
	out := make(panel.Col, T*N)
	idxs := make([]int, 0, N)
	vals := make([]float64, 0, N)
	for i := 0; i < T; i++ {
		idxs = idxs[:0]
		vals = vals[:0]
		for j := 0; j < N; j++ {
			v := x[i*N+j]
			if !math.IsNaN(v) {
				idxs = append(idxs, j)
				vals = append(vals, v)
			}
		}
		if len(vals) == 0 {
			for j := 0; j < N; j++ {
				out[i*N+j] = math.NaN()
			}
			continue
		}
		res := f(vals)
		for j := 0; j < N; j++ {
			out[i*N+j] = math.NaN()
		}
		for k, j := range idxs {
			out[i*N+j] = res[k]
		}
	}
	return out
}

// averageRanks 返回 1..m 的平均秩（并列取平均），升序。
func averageRanks(vals []float64) []float64 {
	m := len(vals)
	order := make([]int, m)
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool { return vals[order[a]] < vals[order[b]] })
	ranks := make([]float64, m)
	i := 0
	for i < m {
		j := i
		for j+1 < m && vals[order[j+1]] == vals[order[i]] {
			j++
		}
		avg := float64(i+j+2) / 2 // 并列平均秩（1-based）
		for k := i; k <= j; k++ {
			ranks[order[k]] = avg
		}
		i = j + 1
	}
	return ranks
}

func init() {
	cs := func(sig, desc, example, tip string) Doc {
		return Doc{Category: "截面", Signature: sig, Description: desc, Example: example, Tip: tip}
	}

	reg("cs_rank", 1, 1, nil, cs("cs_rank(x)", "每个交易日内跨股票升序排名并归一到 0~1，并列取平均秩。", "cs_rank(roe)", "把当天所有股票从低到高排队，最低的得 0、最高的得 1。最常用的收尾步骤，可以消除单位差异和极端值。"), func(p *panel.Panel, cols []panel.Col, consts []float64) panel.Col {
		return csApply(p, cols[0], func(vals []float64) []float64 {
			m := len(vals)
			if m < 2 {
				out := make([]float64, m)
				for i := range out {
					out[i] = math.NaN()
				}
				return out
			}
			r := averageRanks(vals)
			out := make([]float64, m)
			for i := range vals {
				out[i] = (r[i] - 1) / float64(m-1) // 归一到 (0,1)，升序
			}
			return out
		})
	})

	reg("cs_zscore", 1, 1, nil, cs("cs_zscore(x)", "每个交易日内跨股票标准化。", "cs_zscore(total_mv)", "当天每只股票比全体平均水平高或低多少个标准差。保留了差距大小，但容易被极端值影响。"), func(p *panel.Panel, cols []panel.Col, consts []float64) panel.Col {
		return csApply(p, cols[0], func(vals []float64) []float64 {
			if len(vals) < 2 {
				out := make([]float64, len(vals))
				for i := range out {
					out[i] = math.NaN()
				}
				return out
			}
			m, s := mean(vals), sampleStd(vals)
			out := make([]float64, len(vals))
			for i := range vals {
				if s == 0 || math.IsNaN(s) {
					out[i] = math.NaN()
				} else {
					out[i] = (vals[i] - m) / s
				}
			}
			return out
		})
	})

	reg("cs_demean", 1, 1, nil, cs("cs_demean(x)", "每个交易日内减去截面均值。", "cs_demean(pct_chg)", "当天每只股票减去全体平均值，结果表示“比平均水平高多少”。"), func(p *panel.Panel, cols []panel.Col, consts []float64) panel.Col {
		return csApply(p, cols[0], func(vals []float64) []float64 {
			m := mean(vals)
			out := make([]float64, len(vals))
			for i := range vals {
				out[i] = vals[i] - m
			}
			return out
		})
	})

	reg("cs_scale", 1, 1, nil, cs("cs_scale(x)", "每个交易日内按绝对值之和缩放。", "cs_scale(close)", "把当天所有股票的值等比例缩放，使绝对值加起来等于 1，可以理解成换算成“仓位占比”。"), func(p *panel.Panel, cols []panel.Col, consts []float64) panel.Col {
		return csApply(p, cols[0], func(vals []float64) []float64 {
			s := 0.0
			for _, v := range vals {
				s += math.Abs(v)
			}
			out := make([]float64, len(vals))
			for i := range vals {
				if s == 0 {
					out[i] = math.NaN()
				} else {
					out[i] = vals[i] / s
				}
			}
			return out
		})
	})
}
