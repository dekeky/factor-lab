package factors

import (
	"math"
	"testing"

	"factor-lab/internal/data"
	"factor-lab/internal/engine"
)

// TestClassicExprsValid 全部内置因子必须通过静态校验（字段/算子/窗口/语法）。
func TestClassicExprsValid(t *testing.T) {
	src := &data.SyntheticSource{Days: 320, Stocks: 50, Seed: 7}
	p, err := src.Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range Classics {
		if errs := engine.Check(d.Expr, p); len(errs) > 0 {
			t.Errorf("%s: %v", d.Name, errs)
		}
	}
}

// TestClassicExprsRun 全部内置因子必须可执行且产出非全 NaN 的因子列
//（长窗口因子允许前段 NaN，整体有效值比例 > 30%）。
func TestClassicExprsRun(t *testing.T) {
	// Days=400：覆盖最长 250 日窗口后仍余 ~37% 有效样本
	src := &data.SyntheticSource{Days: 400, Stocks: 50, Seed: 7}
	p, err := src.Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range Classics {
		col, errs := engine.Run(d.Expr, p)
		if len(errs) > 0 {
			t.Errorf("%s: %v", d.Name, errs)
			continue
		}
		valid := 0
		for _, v := range col {
			if !math.IsNaN(v) && !math.IsInf(v, 0) {
				valid++
			}
		}
		ratio := float64(valid) / float64(len(col))
		if ratio < 0.30 {
			t.Errorf("%s: 有效值比例 %.1f%%（期望 > 30%%），疑似算子语义问题", d.Name, ratio*100)
		}
	}
}
