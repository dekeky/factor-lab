package dsl

import (
	"fmt"
	"math"
)

// FuncSig 描述一个算子（函数）的调用签名，由 ops 包提供，dsl 不依赖 ops。
type FuncSig struct {
	Name     string
	MinArgs  int
	MaxArgs  int   // 与 MinArgs 相等 = 固定参数个数
	ConstArgs []int // 这些位置的参数必须是正整数常数（如窗口长度）
}

// MaxWindow 是滚动窗口的上限（交易日），防止意外超长窗口拖垮计算。
const MaxWindow = 512

// Validate 做静态检查：函数名 / 参数个数 / 常数位 / 字段与变量引用。
// fields 是 Panel 可用字段名集合。返回错误列表（空 = 通过）。
func Validate(prog *Program, funcs map[string]FuncSig, fields map[string]bool) []error {
	var errs []error
	env := make(map[string]bool)
	for name := range fields {
		env[name] = true
	}
	for _, st := range prog.Stmts {
		switch n := st.(type) {
		case *AssignStmt:
			if fields[n.Name] {
				errs = append(errs, fmt.Errorf("第%d行: 变量名 %q 与字段重名，请换名", n.Line, n.Name))
			}
			errs = append(errs, walk(n.Value, funcs, env, 0)...)
			env[n.Name] = true
		case *ExprStmt:
			errs = append(errs, walk(n.X, funcs, env, 0)...)
		}
	}
	return errs
}

func walk(n Node, funcs map[string]FuncSig, env map[string]bool, depth int) []error {
	if depth > 64 {
		return []error{fmt.Errorf("表达式嵌套过深（>64 层）")}
	}
	var errs []error
	switch v := n.(type) {
	case *NumLit:
		// ok
	case *IdentRef:
		if !env[v.Name] {
			errs = append(errs, fmt.Errorf("第%d行: 未知的字段或变量 %q", v.Line, v.Name))
		}
	case *UnExpr:
		errs = append(errs, walk(v.X, funcs, env, depth+1)...)
	case *BinExpr:
		errs = append(errs, walk(v.L, funcs, env, depth+1)...)
		errs = append(errs, walk(v.R, funcs, env, depth+1)...)
	case *CallExpr:
		sig, ok := funcs[v.Fun]
		if !ok {
			errs = append(errs, fmt.Errorf("第%d行: 未知算子 %q", v.Line, v.Fun))
			break
		}
		nargs := len(v.Args)
		if nargs < sig.MinArgs || (sig.MaxArgs == sig.MinArgs && nargs != sig.MinArgs) {
			errs = append(errs, fmt.Errorf("第%d行: 算子 %s 期望 %d 个参数，实际 %d 个",
				v.Line, v.Fun, sig.MinArgs, nargs))
			break
		}
		for _, pos := range sig.ConstArgs {
			if pos >= nargs {
				continue
			}
			num, ok := v.Args[pos].(*NumLit)
			if !ok {
				errs = append(errs, fmt.Errorf("第%d行: %s 第 %d 个参数必须是常数（如窗口长度 20）",
					v.Line, v.Fun, pos+1))
				continue
			}
			if num.V < 1 || num.V > MaxWindow || num.V != math.Trunc(num.V) {
				errs = append(errs, fmt.Errorf("第%d行: %s 第 %d 个参数必须是 1~%d 的整数，实际 %v",
					v.Line, v.Fun, pos+1, MaxWindow, num.V))
			}
		}
		for _, a := range v.Args {
			errs = append(errs, walk(a, funcs, env, depth+1)...)
		}
	default:
		errs = append(errs, fmt.Errorf("第%d行: 未知节点类型 %T", n.Pos(), n))
	}
	return errs
}

// ConstantsOf 抽取调用中常数位参数的值（引擎执行时使用）。
// 返回与 Args 等长的切片，非常数位为 NaN。
func ConstantsOf(c *CallExpr) []float64 {
	out := make([]float64, len(c.Args))
	for i := range out {
		out[i] = math.NaN()
		if num, ok := c.Args[i].(*NumLit); ok {
			out[i] = num.V
		}
	}
	return out
}
