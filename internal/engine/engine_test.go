package engine

import (
	"math"
	"testing"

	"factor-lab/internal/data"
	"factor-lab/internal/eval"
)

func TestRunAlpha101Simple(t *testing.T) {
	src := &data.SyntheticSource{Days: 120, Stocks: 30, Seed: 7}
	p, err := src.Load(nil)
	if err != nil {
		t.Fatal(err)
	}

	// Alpha#101: (close - open) / (high - low + 0.001)
	col, errs := Run("alpha = (close - open) / (high - low + 0.001)", p)
	if len(errs) > 0 {
		t.Fatalf("Alpha#101 执行失败: %v", errs)
	}
	if len(col) != p.Size() {
		t.Fatalf("输出长度 %d != %d", len(col), p.Size())
	}

	// Alpha#3: -1 * ts_corr(cs_rank(open), cs_rank(vol), 10)
	col, errs = Run(`
r_open = cs_rank(open)
r_vol  = cs_rank(vol)
alpha  = -1 * ts_corr(r_open, r_vol, 10)
`, p)
	if len(errs) > 0 {
		t.Fatalf("Alpha#3 执行失败: %v", errs)
	}
	// 前 9 日窗口不足应为 NaN
	nanCount := 0
	for j := 0; j < p.N(); j++ {
		if math.IsNaN(col[j]) {
			nanCount++
		}
	}
	if nanCount != p.N() {
		t.Fatalf("前 9 日应全为 NaN, 实际 %d/%d", nanCount, p.N())
	}
}

func TestRunKnownReversalIC(t *testing.T) {
	// 合成数据内置 5 日反转规律 → 动量因子 RankIC 应显著为负
	src := &data.SyntheticSource{Days: 300, Stocks: 80, Seed: 42}
	p, err := src.Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	col, errs := Run("mom5 = close / ts_delay(close, 5) - 1\nalpha = -1 * cs_rank(mom5)", p)
	if len(errs) > 0 {
		t.Fatalf("执行失败: %v", errs)
	}
	m, err := eval.Evaluate(p, col, eval.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	// alpha = -动量，反转规律下应为正且统计显著
	// （合成数据构造推算 RankIC ≈ 0.07~0.10，阈值 0.05 已留余量）
	if m.RankICMean < 0.05 {
		t.Errorf("反转因子 RankIC = %v, 期望 > 0.05（合成数据内置 -0.30 反转系数）", m.RankICMean)
	}
	if math.IsNaN(m.RankICT) || m.RankICT < 3 {
		t.Errorf("RankIC t 值 = %v, 期望 > 3（统计显著）", m.RankICT)
	}
	// 分层单调性：第 9 层（因子最大）应高于第 0 层
	if m.LayerMean[9] <= m.LayerMean[0] {
		t.Errorf("分层不单调: Q10=%v Q1=%v", m.LayerMean[9], m.LayerMean[0])
	}
}

func TestRunValidationErrors(t *testing.T) {
	src := &data.SyntheticSource{Days: 30, Stocks: 5, Seed: 1}
	p, _ := src.Load(nil)

	cases := []string{
		"alpha = cs_rank(no_field)",          // 未知字段
		"alpha = ts_delay(close, close)",     // 窗口非常数
		"alpha = no_such_op(close)",          // 未知算子
		"alpha = ts_delay(close, 0)",         // 窗口下限
		"w = alpha_not_defined\nalpha = w + 1", // 变量未定义（顺序错）
	}
	for _, src := range cases {
		if _, errs := Run(src, p); len(errs) == 0 {
			t.Errorf("源码 %q 应校验失败", src)
		}
	}
}
