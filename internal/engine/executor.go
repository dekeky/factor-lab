// Package engine 把 DSL 源码在 Panel 上执行为因子列。
// 使用方式：engine.Run(src, panel) → 因子值列（NaN 表示缺失）。
package engine

import (
	"fmt"
	"math"
	"strings"

	"factor-lab/internal/dsl"
	"factor-lab/internal/ops"
	"factor-lab/internal/panel"
)

// Run 编译 + 校验 + 执行一段因子 DSL。
func Run(src string, p *panel.Panel) (panel.Col, []error) {
	prog, errs := dsl.Parse(src)
	if len(errs) > 0 {
		return nil, errs
	}
	fields := make(map[string]bool)
	for name := range p.Cols {
		fields[name] = true
	}
	if errs := dsl.Validate(prog, ops.Sigs(), fields); len(errs) > 0 {
		return nil, errs
	}
	col, err := exec(prog, p)
	if err != nil {
		return nil, []error{err}
	}
	return col, nil
}

func exec(prog *dsl.Program, p *panel.Panel) (panel.Col, error) {
	size := p.Size()
	vars := make(map[string]panel.Col)
	var result panel.Col
	for _, st := range prog.Stmts {
		switch n := st.(type) {
		case *dsl.AssignStmt:
			v, err := evalNode(n.Value, p, vars, size)
			if err != nil {
				return nil, err
			}
			vars[n.Name] = v
			result = v
		case *dsl.ExprStmt:
			v, err := evalNode(n.X, p, vars, size)
			if err != nil {
				return nil, err
			}
			result = v
		default:
			return nil, fmt.Errorf("未知语句类型 %T", st)
		}
	}
	if result == nil {
		return nil, fmt.Errorf("表达式没有产生结果")
	}
	return result, nil
}

func broadcast(v float64, size int) panel.Col {
	out := make(panel.Col, size)
	for i := range out {
		out[i] = v
	}
	return out
}

func evalNode(n dsl.Node, p *panel.Panel, vars map[string]panel.Col, size int) (panel.Col, error) {
	switch v := n.(type) {
	case *dsl.NumLit:
		return broadcast(v.V, size), nil

	case *dsl.IdentRef:
		if c, ok := vars[v.Name]; ok {
			return c, nil
		}
		if c, ok := p.Col(v.Name); ok {
			return c, nil
		}
		return nil, fmt.Errorf("第%d行: 未知字段或变量 %q（校验层应已拦截）", v.Line, v.Name)

	case *dsl.UnExpr:
		x, err := evalNode(v.X, p, vars, size)
		if err != nil {
			return nil, err
		}
		out := make(panel.Col, size)
		if v.Op == "-" {
			for k := range out {
				if math.IsNaN(x[k]) {
					out[k] = math.NaN()
				} else {
					out[k] = -x[k]
				}
			}
			return out, nil
		}
		// not
		for k := range out {
			xv := x[k]
			if !math.IsNaN(xv) && xv != 0 {
				out[k] = 0
			} else {
				out[k] = 1
			}
		}
		return out, nil

	case *dsl.BinExpr:
		l, err := evalNode(v.L, p, vars, size)
		if err != nil {
			return nil, err
		}
		r, err := evalNode(v.R, p, vars, size)
		if err != nil {
			return nil, err
		}
		return binApply(v.Op, l, r, size)

	case *dsl.CallExpr:
		op, ok := ops.Registry[v.Fun]
		if !ok {
			return nil, fmt.Errorf("第%d行: 未知算子 %q", v.Line, v.Fun)
		}
		consts := dsl.ConstantsOf(v)
		constSet := make(map[int]bool)
		for _, pos := range op.Sig.ConstArgs {
			constSet[pos] = true
		}
		cols := make([]panel.Col, len(v.Args))
		for i, arg := range v.Args {
			if constSet[i] {
				continue // 常数位由 consts 传值
			}
			c, err := evalNode(arg, p, vars, size)
			if err != nil {
				return nil, err
			}
			cols[i] = c
		}
		return op.Fn(p, cols, consts), nil

	default:
		return nil, fmt.Errorf("未知节点类型 %T", n)
	}
}

// binApply 二元运算的空值规则：
//   - + - * /：NaN 传播；除数为 0 → NaN
//   - 比较与 and/or：NaN 按假处理，结果 1.0 / 0.0
func binApply(op string, l, r panel.Col, size int) (panel.Col, error) {
	out := make(panel.Col, size)
	switch op {
	case "+", "-", "*", "/":
		for k := range out {
			x, y := l[k], r[k]
			if math.IsNaN(x) || math.IsNaN(y) {
				out[k] = math.NaN()
				continue
			}
			switch op {
			case "+":
				out[k] = x + y
			case "-":
				out[k] = x - y
			case "*":
				out[k] = x * y
			default:
				if y == 0 {
					out[k] = math.NaN()
				} else {
					out[k] = x / y
				}
			}
		}
		return out, nil
	case "<", "<=", ">", ">=", "==", "!=":
		for k := range out {
			x, y := l[k], r[k]
			// NaN 参与比较 → 0（假）
			res := false
			if !math.IsNaN(x) && !math.IsNaN(y) {
				switch op {
				case "<":
					res = x < y
				case "<=":
					res = x <= y
				case ">":
					res = x > y
				case ">=":
					res = x >= y
				case "==":
					res = x == y
				default:
					res = x != y
				}
			}
			if res {
				out[k] = 1
			} else {
				out[k] = 0
			}
		}
		return out, nil
	case "and", "or":
		for k := range out {
			tx := !math.IsNaN(l[k]) && l[k] != 0
			ty := !math.IsNaN(r[k]) && r[k] != 0
			res := false
			if op == "and" {
				res = tx && ty
			} else {
				res = tx || ty
			}
			if res {
				out[k] = 1
			} else {
				out[k] = 0
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("未知二元运算符 %q", op)
	}
}

// Check 便捷封装：只编译校验不执行（编辑器实时提示用）。
func Check(src string, p *panel.Panel) []string {
	prog, errs := dsl.Parse(src)
	if len(errs) > 0 {
		return errsStr(errs)
	}
	fields := make(map[string]bool)
	for name := range p.Cols {
		fields[name] = true
	}
	if errs := dsl.Validate(prog, ops.Sigs(), fields); len(errs) > 0 {
		return errsStr(errs)
	}
	return nil
}

func errsStr(errs []error) []string {
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.Error()
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

var _ = strings.TrimSpace
