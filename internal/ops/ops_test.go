package ops

import (
	"math"
	"testing"
	"time"

	"factor-lab/internal/panel"
)

// mkPanel 构造 T×N 小面板；cols: 字段名 → 值按 (日期,股票) 行主序给出。
func mkPanel(t *testing.T, T, N int, cols map[string][]float64) *panel.Panel {
	t.Helper()
	dates := make([]time.Time, T)
	for i := range dates {
		dates[i] = time.Date(2024, 1, 1+i, 0, 0, 0, 0, time.UTC)
	}
	codes := make([]string, N)
	for j := range codes {
		codes[j] = "S" + string(rune('A'+j))
	}
	p := panel.New(dates, codes)
	for name, vals := range cols {
		if len(vals) != T*N {
			t.Fatalf("列 %s 长度 %d != %d", name, len(vals), T*N)
		}
		col := make(panel.Col, T*N)
		copy(col, vals)
		if err := p.SetCol(name, col); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

var nan = math.NaN()

func isNA(v float64) bool { return math.IsNaN(v) }

func TestTsDelay(t *testing.T) {
	// 2 日 × 1 股: x = [1, 2]
	p := mkPanel(t, 2, 1, map[string][]float64{"x": {1, 2}})
	out := Registry["ts_delay"].Fn(p, []panel.Col{p.MustCol("x")}, []float64{1, 1})
	if !isNA(out[0]) {
		t.Errorf("i=0 应为 NaN（窗口不足）, 实际 %v", out[0])
	}
	if out[1] != 1 {
		t.Errorf("i=1 期望 1, 实际 %v", out[1])
	}
}

func TestTsMeanSkipNaN(t *testing.T) {
	// 3 日 × 1 股: x = [1, NaN, 3] → 窗口 3 的均值应为 2（跳过 NaN）
	p := mkPanel(t, 3, 1, map[string][]float64{"x": {1, nan, 3}})
	out := Registry["ts_mean"].Fn(p, []panel.Col{p.MustCol("x")}, []float64{2, 2})
	if out[1] != 1 { // 窗口 [1, NaN] → 跳过 NaN 只算 1
		t.Errorf("i=1 期望 1, 实际 %v", out[1])
	}
	if out[2] != 3 { // 窗口 [NaN, 3]
		t.Errorf("i=2 期望 3, 实际 %v", out[2])
	}
}

func TestCsRank(t *testing.T) {
	// 1 日 × 4 股: x = [10, 20, 30, 40] → 升序排名 0, 1/3, 2/3, 1
	p := mkPanel(t, 1, 4, map[string][]float64{"x": {10, 20, 30, 40}})
	out := Registry["cs_rank"].Fn(p, []panel.Col{p.MustCol("x")}, nil)
	want := []float64{0, 1.0 / 3, 2.0 / 3, 1}
	for j, w := range want {
		if math.Abs(out[j]-w) > 1e-9 {
			t.Errorf("cs_rank[%d] = %v, 期望 %v", j, out[j], w)
		}
	}
}

func TestCsRankTiesAverage(t *testing.T) {
	// 并列取平均秩: [5, 5, 10] → 秩 1.5, 1.5, 3 → 归一 (0.25, 0.25, 1)
	p := mkPanel(t, 1, 3, map[string][]float64{"x": {5, 5, 10}})
	out := Registry["cs_rank"].Fn(p, []panel.Col{p.MustCol("x")}, nil)
	if math.Abs(out[0]-0.25) > 1e-9 || math.Abs(out[1]-0.25) > 1e-9 || out[2] != 1 {
		t.Errorf("并列平均秩错误: %v", out)
	}
}

func TestTsCorr(t *testing.T) {
	// 5 日 × 1 股，两条完全同向序列 → 相关 = 1
	x := []float64{1, 2, 3, 4, 5}
	y := []float64{2, 4, 6, 8, 10}
	T, N := 5, 1
	cols := map[string][]float64{}
	for i := range x {
		cols["x"] = append(cols["x"], x[i])
		cols["y"] = append(cols["y"], y[i])
	}
	p := mkPanel(t, T, N, cols)
	out := Registry["ts_corr"].Fn(p, []panel.Col{p.MustCol("x"), p.MustCol("y")}, []float64{3, 3, 3})
	// 窗口 3 的最后一个位置: [3,4,5] vs [6,8,10] → corr = 1
	if math.Abs(out[4]-1) > 1e-9 {
		t.Errorf("完全正相关序列 ts_corr = %v, 期望 1", out[4])
	}
	// 前两个位置窗口不足
	if !isNA(out[0]) || !isNA(out[1]) {
		t.Errorf("窗口不足应为 NaN: %v %v", out[0], out[1])
	}
}

func TestTsArgMax(t *testing.T) {
	// x = [3, 1, 2] 窗口 3 → argmax = 0（值 3 在窗口首位）
	p := mkPanel(t, 3, 1, map[string][]float64{"x": {3, 1, 2}})
	out := Registry["ts_arg_max"].Fn(p, []panel.Col{p.MustCol("x")}, []float64{3, 3})
	if out[2] != 0 {
		t.Errorf("ts_arg_max = %v, 期望 0", out[2])
	}
}

func TestTsRankCurrentNaN(t *testing.T) {
	// 当前值为 NaN 时 ts_rank 必须为 NaN（不能拿窗口内旧值充当当前值）
	p := mkPanel(t, 3, 1, map[string][]float64{"x": {1, 2, nan}})
	out := Registry["ts_rank"].Fn(p, []panel.Col{p.MustCol("x")}, []float64{3, 3})
	if !isNA(out[2]) {
		t.Errorf("当前值 NaN 时 ts_rank = %v, 期望 NaN", out[2])
	}
}

func TestTsDecayLinear(t *testing.T) {
	// x = [1, 2] 窗口 2: 最新值权重 2, 最旧 1 → (1*1 + 2*2)/3 = 5/3
	p := mkPanel(t, 2, 1, map[string][]float64{"x": {1, 2}})
	out := Registry["ts_decay_linear"].Fn(p, []panel.Col{p.MustCol("x")}, []float64{2, 2})
	if math.Abs(out[1]-5.0/3) > 1e-9 {
		t.Errorf("ts_decay_linear = %v, 期望 5/3", out[1])
	}
}
