// Package ops 实现因子 DSL 的全部算子。
// 约定：所有算子均为纯函数（Panel + 参数列 → 结果列）；
// 缺失值统一用 NaN 表示并按下述规则传播：
//   - 算术运算：NaN 参与即 NaN；除数为 0 结果为 NaN
//   - 比较运算：NaN 参与按 0（假）处理，结果为 1.0 / 0.0
//   - 滚动聚合：窗口内跳过 NaN；有效样本数不足时输出 NaN
package ops

import (
	"fmt"

	"factor-lab/internal/dsl"
	"factor-lab/internal/panel"
)

// Op 是一个可执行算子：签名 + 实现。
type Op struct {
	Sig dsl.FuncSig
	Fn  func(p *panel.Panel, cols []panel.Col, consts []float64) panel.Col
}

// Registry 是全部算子的注册表。
var Registry = map[string]Op{}

// Sigs 导出签名表（供 dsl.Validate 使用，避免包依赖环）。
func Sigs() map[string]dsl.FuncSig {
	out := make(map[string]dsl.FuncSig)
	for _, op := range Registry {
		out[op.Sig.Name] = op.Sig
	}
	return out
}

func reg(name string, min, max int, consts []int, fn func(p *panel.Panel, cols []panel.Col, consts []float64) panel.Col) {
	Registry[name] = Op{
		Sig: dsl.FuncSig{Name: name, MinArgs: min, MaxArgs: max, ConstArgs: consts},
		Fn:  fn,
	}
}

func winConst(n int) []int {
	out := make([]int, n)
	for i := 0; i < n; i++ {
		out[i] = i
	}
	return out
}

// checkArgs 统一校验列长度并把常数转 int。
func checkArgs(p *panel.Panel, cols []panel.Col, consts []float64, wantCols int) (size int, err error) {
	size = p.Size()
	for i, c := range cols {
		if c == nil {
			continue
		}
		if len(c) != size {
			return 0, fmt.Errorf("第 %d 个参数长度 %d != 面板大小 %d", i+1, len(c), size)
		}
	}
	return size, nil
}
