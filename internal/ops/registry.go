// Package ops 实现因子 DSL 的全部算子。
// 约定：所有算子均为纯函数（Panel + 参数列 → 结果列）；
// 缺失值统一用 NaN 表示并按下述规则传播：
//   - 算术运算：NaN 参与即 NaN；除数为 0 结果为 NaN
//   - 比较运算：NaN 参与按 0（假）处理，结果为 1.0 / 0.0
//   - 滚动聚合：窗口内跳过 NaN；有效样本数不足时输出 NaN
package ops

import (
	"fmt"
	"sort"

	"factor-lab/internal/dsl"
	"factor-lab/internal/panel"
)

// Op 是一个可执行算子：签名 + 实现 + 给帮助页/Agent 用的说明。
type Op struct {
	Sig dsl.FuncSig
	Doc Doc
	Fn  func(p *panel.Panel, cols []panel.Col, consts []float64) panel.Col
}

// Doc 是算子在注册时一并写入的说明。Keyword 为 true 的是语言关键字（and/or/not），不进目录。
type Doc struct {
	Category    string `json:"category"`
	Signature   string `json:"signature"`
	Description string `json:"description"`
	Example     string `json:"example"`
	Tip         string `json:"tip,omitempty"`
	Keyword     bool   `json:"-"`
}

// Info 是对外目录条目（不含实现函数）。
type Info struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Signature   string `json:"signature"`
	Description string `json:"description"`
	Example     string `json:"example"`
	Tip         string `json:"tip,omitempty"`
	MinArgs     int    `json:"min_args"`
	MaxArgs     int    `json:"max_args"`
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

// Infos 列出可在 DSL 里当函数调用的算子（跳过语言关键字），按名称排序。
func Infos() []Info {
	names := make([]string, 0, len(Registry))
	for name, op := range Registry {
		if op.Doc.Keyword {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Info, 0, len(names))
	for _, name := range names {
		op := Registry[name]
		out = append(out, Info{
			Name: name, Category: op.Doc.Category, Signature: op.Doc.Signature,
			Description: op.Doc.Description, Example: op.Doc.Example, Tip: op.Doc.Tip,
			MinArgs: op.Sig.MinArgs, MaxArgs: op.Sig.MaxArgs,
		})
	}
	return out
}

func reg(name string, min, max int, consts []int, doc Doc, fn func(p *panel.Panel, cols []panel.Col, consts []float64) panel.Col) {
	Registry[name] = Op{
		Sig: dsl.FuncSig{Name: name, MinArgs: min, MaxArgs: max, ConstArgs: consts},
		Doc: doc,
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
