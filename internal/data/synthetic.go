package data

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"

	"factor-lab/internal/panel"
)

// SyntheticSource 生成带已知规律的内存面板，仅供单元测试使用，不挂服务入口：
//   - 短期反转：过去 5 日涨跌幅对未来收益有稳定负预测（动量因子 IC 显著为负）
//   - 质量效应：个股固定 ROE 水平对未来收益有弱正预测
//
// 其余字段为噪声，用于验证"无效因子 IC ≈ 0"。
type SyntheticSource struct {
	Days   int
	Stocks int
	Seed   int64
}

func (s *SyntheticSource) Name() string { return "synthetic" }

func (s *SyntheticSource) Load(ctx context.Context) (*panel.Panel, error) {
	T := s.Days
	if T <= 0 {
		T = 500
	}
	N := s.Stocks
	if N <= 0 {
		N = 300
	}

	// 交易日历：从 2023-01-02 起跳过周末
	dates := make([]time.Time, 0, T)
	d := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	for len(dates) < T {
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			dates = append(dates, d)
		}
		d = d.AddDate(0, 0, 1)
	}
	codes := make([]string, N)
	names := make([]string, N)
	for j := 0; j < N; j++ {
		codes[j] = fmt.Sprintf("S%04d", j+1)
		names[j] = fmt.Sprintf("合成%04d", j+1)
	}
	p := panel.New(dates, codes)
	p.SetNames(names)
	size := p.Size()

	rng := rand.New(rand.NewSource(s.Seed))
	norm := func() float64 { return rng.NormFloat64() }

	// 每股固定质量分（0~1）
	quality := make([]float64, N)
	for j := range quality {
		quality[j] = rng.Float64()
	}

	// 收益生成：r_t = -0.15 * mean(r_{t-5..t-1}) + 质量效应 + 市场因子 + 个股噪声
	rets := make([][]float64, N) // N x T
	for j := range rets {
		rets[j] = make([]float64, T)
	}
	for i := 1; i < T; i++ {
		market := 0.0003 + 0.008*norm()
		for j := 0; j < N; j++ {
			past := 0.0
			for k := 1; k <= 5; k++ {
				idx := i - k
				if idx >= 0 {
					past += rets[j][idx]
				}
			}
			past /= 5
			r := -0.30*past + 0.0015*(quality[j]-0.5) + 0.015*norm() + market
			rets[j][i] = r
		}
	}

	// 价格与字段列
	openC := make(panel.Col, size)
	highC := make(panel.Col, size)
	lowC := make(panel.Col, size)
	closeC := make(panel.Col, size)
	pctC := make(panel.Col, size)
	volC := make(panel.Col, size)
	amountC := make(panel.Col, size)
	turnC := make(panel.Col, size)
	peC := make(panel.Col, size)
	pbC := make(panel.Col, size)
	mvC := make(panel.Col, size)
	roeC := make(panel.Col, size)

	for j := 0; j < N; j++ {
		base := 10 + 60*rng.Float64()
		price := base
		vol0 := 1e6 + 5e6*rng.Float64()
		pe0 := 8 + 60*rng.Float64()
		pb0 := 0.5 + 6*rng.Float64()
		for i := 0; i < T; i++ {
			idx := p.Idx(i, j)
			r := rets[j][i]
			openC[idx] = price * (1 + 0.3*r)
			closeC[idx] = price * (1 + r)
			highC[idx] = math.Max(openC[idx], closeC[idx]) * (1 + 0.004*rng.Float64())
			lowC[idx] = math.Min(openC[idx], closeC[idx]) * (1 - 0.004*rng.Float64())
			pctC[idx] = r
			volC[idx] = vol0 * (1 + 0.5*math.Abs(norm()))
			amountC[idx] = volC[idx] * closeC[idx]
			turnC[idx] = 0.5 + 3*rng.Float64()
			peC[idx] = pe0 * (1 + 0.01*norm())
			pbC[idx] = pb0 * (1 + 0.01*norm())
			mvC[idx] = 5e9 + 5e10*rng.Float64()
			roeC[idx] = 0.02 + 0.25*quality[j] + 0.01*norm()
			price = closeC[idx]
		}
	}

	rawOpen := append(panel.Col(nil), openC...)
	rawClose := append(panel.Col(nil), closeC...)
	qfqOpen := append(panel.Col(nil), openC...)
	qfqClose := append(panel.Col(nil), closeC...)
	cols := map[string]panel.Col{
		"open": openC, "high": highC, "low": lowC, "close": closeC,
		"raw_open": rawOpen, "raw_close": rawClose, "qfq_open": qfqOpen, "qfq_close": qfqClose,
		"pct_chg": pctC, "vol": volC, "amount": amountC,
		"turnover_rate": turnC, "pe_ttm": peC, "pb": pbC,
		"total_mv": mvC, "roe": roeC,
	}
	for name, col := range cols {
		if err := p.SetCol(name, col); err != nil {
			return nil, err
		}
	}
	return p, nil
}
