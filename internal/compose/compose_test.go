package compose

import (
	"math"
	"strings"
	"testing"

	"factor-lab/internal/data"
	"factor-lab/internal/engine"
)

func TestParseRenamesIntermediateVars(t *testing.T) {
	f, err := Parse("mom20 = close / ts_delay(close, 20) - 1\nalpha = -1 * cs_rank(mom20)", "p0_")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Pre) != 1 || !strings.HasPrefix(f.Pre[0], "p0_mom20 = ") {
		t.Fatalf("前置赋值未重命名: %#v", f.Pre)
	}
	if !strings.Contains(f.Pre[0], "close / ts_delay(close, 20) - 1") {
		t.Errorf("字段/函数名被误改: %s", f.Pre[0])
	}
	if f.Final != "-1 * cs_rank(p0_mom20)" {
		t.Errorf("终值引用未重命名: %s", f.Final)
	}
}

func TestParseSingleLineBareExpr(t *testing.T) {
	f, err := Parse("(close - open) / (high - low + 0.001)", "p1_")
	if err != nil {
		t.Fatal(err)
	}
	if f.Final != "(close - open) / (high - low + 0.001)" {
		t.Errorf("裸表达式终值错误: %s", f.Final)
	}
}

// TestBuildProducesRunnableExpr 组合表达式必须能通过校验并跑出结果。
func TestBuildProducesRunnableExpr(t *testing.T) {
	src := &data.SyntheticSource{Days: 260, Stocks: 40, Seed: 11}
	p, err := src.Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	parts := []Part{
		{Name: "rev5", Expr: "alpha = -1 * cs_rank(close / ts_delay(close, 5) - 1)", Weight: 1},
		{Name: "low_vol20", Expr: "alpha = -1 * cs_rank(ts_std_dev(pct_chg, 20))", Weight: 1, Direction: 1},
	}
	expr, err := Build(parts, MethodEqual)
	if err != nil {
		t.Fatal(err)
	}
	if errs := engine.Check(expr, p); len(errs) > 0 {
		t.Fatalf("组合表达式校验失败: %v\n%s", errs, expr)
	}
	col, errs := engine.Run(expr, p)
	if len(errs) > 0 {
		t.Fatalf("组合表达式执行失败: %v", errs)
	}
	valid := 0
	for _, v := range col {
		if !math.IsNaN(v) {
			valid++
		}
	}
	if valid == 0 {
		t.Errorf("组合结果全为 NaN")
	}
	if !strings.Contains(expr, "z0 = cs_zscore(") || !strings.Contains(expr, "alpha = z0 * ") {
		t.Errorf("未按规则标准化/合成:\n%s", expr)
	}
}

func TestBuildRejectsBadInput(t *testing.T) {
	if _, err := Build(nil, MethodManual); err == nil {
		t.Error("空组合应报错")
	}
	if _, err := Build([]Part{{Name: "a", Expr: "alpha = close", Weight: -1}}, MethodManual); err == nil {
		t.Error("负权重应报错")
	}
	if _, err := Build([]Part{{Name: "a", Expr: "alpha = unknown_field * 2", Weight: 1}}, MethodManual); err != nil {
		// 字段存在性由校验层负责，Build 只做语法拆分，不应报错
		t.Errorf("Build 不应校验字段: %v", err)
	}
}

func TestCrossCorr(t *testing.T) {
	src := &data.SyntheticSource{Days: 60, Stocks: 30, Seed: 5}
	p, err := src.Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := engine.Run("alpha = -1 * cs_rank(close / ts_delay(close, 5) - 1)", p)
	b, _ := engine.Run("alpha = -1 * cs_rank(close / ts_delay(close, 10) - 1)", p)
	c, _ := engine.Run("alpha = cs_rank(close / ts_delay(close, 5) - 1)", p)
	T, N := p.T(), p.N()

	self, _ := CrossCorr(a, a, T, N)
	if math.Abs(self-1) > 1e-6 {
		t.Errorf("自相关应为 1，实际 %v", self)
	}
	opp, _ := CrossCorr(a, c, T, N)
	if math.Abs(opp+1) > 1e-6 {
		t.Errorf("反向因子相关应为 -1，实际 %v", opp)
	}
	near, _ := CrossCorr(a, b, T, N)
	if !(near > 0.3 && near < 0.95) {
		t.Errorf("5日/10日反转相关应在 (0.3, 0.95)，实际 %v", near)
	}
}
