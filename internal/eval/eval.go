// Package eval 实现因子评价引擎（"冻结裁判"）。
//
// 收益对齐口径（防前向偏差）：
//   信号日 t 使用 t 及以前的数据；t+1 收盘执行；持有 Horizon 个交易日后（t+1+Horizon 收盘）离场。
//   即 fwd_ret(t) = close(t+1+H) / close(t+1) - 1，H >= 1。
//
// 指标口径参考 PandaAI 社区教程的两层体系：
//   第一层（因子）：IC / RankIC 的均值、标准差、ICIR、t 值、P(IC>0.02)、覆盖度
//   第二层（组合）：G 分位分层收益、多空、多头相对全市场超额、换手与成本
package eval

import (
	"fmt"
	"math"
	"sort"

	"factor-lab/internal/panel"
)

// Config 是回测口径参数（向 PandaAI 第四届赛制对齐的默认值）。
type Config struct {
	Horizon   int     // 持有期（交易日），默认 1
	Groups    int     // 分层数，默认 10
	Cost      float64 // 单边交易成本（含滑点粗估），默认 0.003
	MinStocks int     // 每个截面的最少有效股票数，默认 5
}

func DefaultConfig() Config {
	return Config{Horizon: 1, Groups: 10, Cost: 0.003, MinStocks: 5}
}

// Metrics 是一次因子评价的全部产出。
type Metrics struct {
	Config Config `json:"config"`

	NDates  int     `json:"n_dates"`   // 有效评价日数
	ICMean  float64 `json:"ic_mean"`   // Pearson IC 均值
	ICStd   float64 `json:"ic_std"`
	ICIR    float64 `json:"ic_ir"`
	ICT     float64 `json:"ic_t"`      // t 统计量
	RankICMean float64 `json:"rank_ic_mean"`
	RankICStd  float64 `json:"rank_ic_std"`
	RankICIR   float64 `json:"rank_ic_ir"`
	RankICT    float64 `json:"rank_ic_t"`
	PICPos  float64 `json:"pic_pos"`   // P(IC > 0)
	PIC002  float64 `json:"pic_002"`   // P(IC > 0.02)，PandaAI 口径
	Coverage float64 `json:"coverage"` // 因子非 NaN 覆盖度（全股票）

	LayerMean []float64 `json:"layer_mean"`   // 各层平均单期收益（升序：第 0 层因子最小）
	LayerCum  []float64 `json:"layer_cum"`    // 各层累计收益（复利）
	LongShortMean float64 `json:"long_short_mean"` // 最高层 - 最低层，单期
	LongShortCum  float64 `json:"long_short_cum"`
	TopExcessMean float64 `json:"top_excess_mean"` // 最高层 - 全市场均值
	TopExcessCum  float64 `json:"top_excess_cum"`
	UniverseMean  float64 `json:"universe_mean"`  // 全市场等权平均单期收益
	TurnoverDaily float64 `json:"turnover_daily"`  // 头部组合日均换手（成员更换比例）
	CostAnnual    float64 `json:"cost_annual"`     // 年化成本粗估 = 换手 × 2 × 单边成本 × 252

	Dates       []string  `json:"dates"`
	ICByDate    []float64 `json:"ic_by_date"`
	RankICByDate []float64 `json:"rank_ic_by_date"`
	LayerNav    [][]float64 `json:"layer_nav"` // 每层净值序列（与 Dates 对齐）
	TopNav      []float64  `json:"top_nav"`    // 头部组合净值（毛）
	TopNavNet   []float64  `json:"top_nav_net"` // 头部组合净值（扣成本）
	UniverseNav []float64  `json:"universe_nav"`
}

type pair struct {
	j    int
	f, r float64
}

// Evaluate 在面板上评价一列因子值。
func Evaluate(p *panel.Panel, f panel.Col, cfg Config) (*Metrics, error) {
	if cfg.Horizon < 1 {
		return nil, fmt.Errorf("持有期 Horizon 必须 >= 1")
	}
	if cfg.Groups < 2 {
		return nil, fmt.Errorf("分层数 Groups 必须 >= 2")
	}
	close, ok := p.Col("close")
	if !ok {
		return nil, fmt.Errorf("面板缺少 close 列，无法计算前向收益")
	}
	T, N := p.T(), p.N()

	// 前向收益：fwd(t, H) = close(t+1+H)/close(t+1) - 1
	h := cfg.Horizon
	lastSignal := T - 2 - h
	if lastSignal < 1 {
		return nil, fmt.Errorf("面板日期数 %d 不足以按持有期 %d 评价", T, h)
	}

	m := &Metrics{Config: cfg}
	var covSum float64
	var icN, ricN int // 有效 IC 截面数（排除退化截面）
	layerAcc := make([]float64, cfg.Groups)
	var topExcessAcc, lsAcc, uniRetAcc float64
	var topPrev []int     // 上一期头部成员（换手计算）
	var turnoverSum float64
	var turnoverN int

	for i := 0; i <= lastSignal; i++ {
		pairs := make([]pair, 0, N)
		validF := 0
		for j := 0; j < N; j++ {
			fv := f[i*N+j]
			if !math.IsNaN(fv) {
				validF++
			}
			ret := close[(i+1+h)*N+j] / close[(i+1)*N+j] - 1
			if math.IsNaN(fv) || math.IsNaN(ret) {
				continue
			}
			pairs = append(pairs, pair{j: j, f: fv, r: ret})
		}
		covSum += float64(validF) / float64(N)
		if len(pairs) < cfg.MinStocks {
			continue
		}

		// IC 族
		fvals := make([]float64, len(pairs))
		rvals := make([]float64, len(pairs))
		for k, pr := range pairs {
			fvals[k] = pr.f
			rvals[k] = pr.r
		}
		ic := pearson(fvals, rvals)
		rankIC := pearson(avgRank(fvals), avgRank(rvals))
		m.Dates = append(m.Dates, p.Dates[i].Format("2006-01-02"))
		m.ICByDate = append(m.ICByDate, round(ic))
		m.RankICByDate = append(m.RankICByDate, round(rankIC))
		// 退化截面（因子或收益全相同 → 相关系数无定义）不参与 IC 统计，
		// 否则单个 NaN 会污染整条均值/IR/t 值。净值与分层仍按该截面正常推进。
		if !math.IsNaN(ic) {
			m.ICMean += ic
			icN++
			if ic > 0 {
				m.PICPos++
			}
			if ic > 0.02 {
				m.PIC002++
			}
		}
		if !math.IsNaN(rankIC) {
			m.RankICMean += rankIC
			ricN++
		}
		m.NDates++

		// 分层：按因子升序排序后切 G 组
		sort.Slice(pairs, func(a, b int) bool { return pairs[a].f < pairs[b].f })
		mm := len(pairs)
		layerRet := make([]float64, cfg.Groups)
		counts := make([]int, cfg.Groups)
		for k, pr := range pairs {
			g := k * cfg.Groups / mm
			if g >= cfg.Groups {
				g = cfg.Groups - 1
			}
			layerRet[g] += pr.r
			counts[g]++
		}
		for g := 0; g < cfg.Groups; g++ {
			if counts[g] > 0 {
				layerRet[g] /= float64(counts[g])
			}
			layerAcc[g] += layerRet[g]
		}

		// 全市场平均收益（超额基准：等权）
		uniRet := 0.0
		for _, pr := range pairs {
			uniRet += pr.r
		}
		uniRet /= float64(mm)
		uniRetAcc += uniRet

		// 头部组（因子最大层）多头超额与多空
		top := layerRet[cfg.Groups-1]
		bottom := layerRet[0]
		topExcessAcc += top - uniRet
		lsAcc += top - bottom

		// 头部组成员（换手统计）
		topStart := (cfg.Groups - 1) * mm / cfg.Groups
		curTop := make([]int, 0, mm-topStart)
		for k := topStart; k < mm; k++ {
			curTop = append(curTop, pairs[k].j)
		}
		if topPrev != nil && len(curTop) > 0 {
			turnoverSum += membershipTurnover(topPrev, curTop)
			turnoverN++
		}
		topPrev = curTop

		// 净值序列
		if m.LayerNav == nil {
			m.LayerNav = make([][]float64, cfg.Groups)
			for g := range m.LayerNav {
				m.LayerNav[g] = []float64{1}
			}
			m.TopNav = []float64{1}
			m.UniverseNav = []float64{1}
			m.TopNavNet = []float64{1}
			m.LayerCum = make([]float64, cfg.Groups)
		}
		for g := 0; g < cfg.Groups; g++ {
			m.LayerNav[g] = append(m.LayerNav[g], m.LayerNav[g][len(m.LayerNav[g])-1]*(1+layerRet[g]))
			m.LayerCum[g] += layerRet[g]
		}
		m.TopNav = append(m.TopNav, m.TopNav[len(m.TopNav)-1]*(1+top))
		m.UniverseNav = append(m.UniverseNav, m.UniverseNav[len(m.UniverseNav)-1]*(1+uniRet))
		// 净值扣成本：当日换手 × 双边成本
		dailyTurn := 0.0
		if topPrev != nil {
			dailyTurn = membershipTurnover(topPrev, curTop)
		}
		netRet := top - dailyTurn*2*cfg.Cost
		m.TopNavNet = append(m.TopNavNet, m.TopNavNet[len(m.TopNavNet)-1]*(1+netRet))
	}

	if m.NDates == 0 {
		return nil, fmt.Errorf("没有满足最少股票数（%d）的有效截面，无法评价", cfg.MinStocks)
	}

	n := float64(m.NDates)
	icNf, ricNf := float64(icN), float64(ricN)
	m.Coverage = covSum / float64(lastSignal+1)
	if icNf > 0 {
		m.ICMean /= icNf
		m.PICPos /= icNf
		m.PIC002 /= icNf
	} else {
		m.ICMean, m.PICPos, m.PIC002 = math.NaN(), math.NaN(), math.NaN()
	}
	if ricNf > 0 {
		m.RankICMean /= ricNf
	} else {
		m.RankICMean = math.NaN()
	}

	// 标准差与 IR（跳过 NaN 截面）
	var icSS, ricSS float64
	for k := 0; k < m.NDates; k++ {
		if v := m.ICByDate[k]; !math.IsNaN(v) {
			d := v - m.ICMean
			icSS += d * d
		}
		if v := m.RankICByDate[k]; !math.IsNaN(v) {
			d2 := v - m.RankICMean
			ricSS += d2 * d2
		}
	}
	m.ICStd = math.Sqrt(safeDiv(icSS, icNf))
	m.RankICStd = math.Sqrt(safeDiv(ricSS, ricNf))
	m.ICIR = safeDiv(m.ICMean, m.ICStd)
	m.RankICIR = safeDiv(m.RankICMean, m.RankICStd)
	m.ICT = safeDiv(m.ICMean, m.ICStd/math.Sqrt(icNf))
	m.RankICT = safeDiv(m.RankICMean, m.RankICStd/math.Sqrt(ricNf))

	m.LayerMean = make([]float64, cfg.Groups)
	for g := range m.LayerMean {
		m.LayerMean[g] = layerAcc[g] / n
	}
	m.LongShortMean = lsAcc / n
	m.TopExcessMean = topExcessAcc / n
	m.UniverseMean = uniRetAcc / n

	// 累计（复利）
	if m.LayerCum != nil {
		for g := 0; g < cfg.Groups; g++ {
			m.LayerCum[g] = m.LayerNav[g][len(m.LayerNav[g])-1] - 1
		}
		m.LongShortCum = m.TopNav[len(m.TopNav)-1] - m.UniverseNav[len(m.UniverseNav)-1]
		m.TopExcessCum = m.LayerCum[cfg.Groups-1] - (m.UniverseNav[len(m.UniverseNav)-1] - 1)
	}
	if turnoverN > 0 {
		m.TurnoverDaily = turnoverSum / float64(turnoverN)
		m.CostAnnual = m.TurnoverDaily * 2 * cfg.Cost * 252
	}
	return m, nil
}

// UniverseMean 导出字段见 Metrics.UniverseMean。

func membershipTurnover(prev, cur []int) float64 {
	set := make(map[int]bool, len(prev))
	for _, j := range prev {
		set[j] = true
	}
	kept := 0
	for _, j := range cur {
		if set[j] {
			kept++
		}
	}
	if len(cur) == 0 {
		return 0
	}
	return 1 - float64(kept)/float64(len(cur))
}

func pearson(xs, ys []float64) float64 {
	n := float64(len(xs))
	if len(xs) != len(ys) || n == 0 {
		return math.NaN()
	}
	mx, my := 0.0, 0.0
	for k := range xs {
		mx += xs[k]
		my += ys[k]
	}
	mx /= n
	my /= n
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

// avgRank 平均秩（1..m，并列取平均）。
func avgRank(vals []float64) []float64 {
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
		avg := float64(i+j+2) / 2
		for k := i; k <= j; k++ {
			ranks[order[k]] = avg
		}
		i = j + 1
	}
	return ranks
}

func safeDiv(a, b float64) float64 {
	if math.IsNaN(b) || b == 0 {
		return math.NaN()
	}
	return a / b
}

func round(v float64) float64 {
	if math.IsNaN(v) {
		return v
	}
	return math.Round(v*1e6) / 1e6
}
