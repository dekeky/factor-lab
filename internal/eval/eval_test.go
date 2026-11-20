package eval_test

import (
	"math"
	"testing"

	"factor-lab/internal/data"
	"factor-lab/internal/engine"
	"factor-lab/internal/eval"
)

// TestDegenerateCrossSectionDoesNotPoisonIC 退化截面（当日因子值或收益全相同，
// 相关系数无定义）必须被跳过，而不是把 NaN 累加进 IC 均值/IR/t 值。
// 合成数据首日收益为 0，因此 (close-open)/open 在第 1 个截面上恒为 0——天然退化。
func TestDegenerateCrossSectionDoesNotPoisonIC(t *testing.T) {
	src := &data.SyntheticSource{Days: 120, Stocks: 60, Seed: 3}
	p, err := src.Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	col, errs := engine.Run("alpha = (close - open) / open", p)
	if len(errs) > 0 {
		t.Fatalf("执行失败: %v", errs)
	}
	m, err := eval.Evaluate(p, col, eval.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if math.IsNaN(m.ICMean) || math.IsNaN(m.RankICMean) {
		t.Fatalf("退化截面污染了均值：ic=%v rankIC=%v", m.ICMean, m.RankICMean)
	}
	if math.IsNaN(m.ICStd) || math.IsNaN(m.RankICStd) || math.IsNaN(m.RankICT) {
		t.Fatalf("退化截面污染了标准差/t 值：icStd=%v ricStd=%v ricT=%v", m.ICStd, m.RankICStd, m.RankICT)
	}
	// 首日 IC 无定义，应在序列里保持 NaN 以便前端作图留缺口
	if !math.IsNaN(m.ICByDate[0]) {
		t.Errorf("首日 IC 期望 NaN（退化截面），实际 %v", m.ICByDate[0])
	}
	// 净值序列仍应与日期等长（退化截面不阻断净值推进）
	if len(m.TopNav) != len(m.Dates)+1 {
		t.Errorf("净值长度 %d 与日期数 %d 不一致", len(m.TopNav), len(m.Dates))
	}
}

// TestAllDegenerateReturnsErrorOrNaN 若所有截面都退化，应给出 NaN 指标而不是假数字。
func TestMetricsAreFiniteOnNormalFactor(t *testing.T) {
	src := &data.SyntheticSource{Days: 200, Stocks: 80, Seed: 9}
	p, err := src.Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	col, _ := engine.Run("alpha = -1 * cs_rank(close / ts_delay(close, 5) - 1)", p)
	m, err := eval.Evaluate(p, col, eval.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	for name, v := range map[string]float64{
		"ICMean": m.ICMean, "ICStd": m.ICStd, "ICIR": m.ICIR, "ICT": m.ICT,
		"RankICMean": m.RankICMean, "RankICStd": m.RankICStd, "RankICIR": m.RankICIR, "RankICT": m.RankICT,
		"TopExcessCum": m.TopExcessCum, "Coverage": m.Coverage,
	} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Errorf("%s 非有限值: %v", name, v)
		}
	}
	// 合成数据内置反转规律：反转因子应显著为正
	if m.RankICMean <= 0.01 || m.RankICT < 3 {
		t.Errorf("反转因子 RankIC=%v t=%v，与数据内置规律不符", m.RankICMean, m.RankICT)
	}
}
