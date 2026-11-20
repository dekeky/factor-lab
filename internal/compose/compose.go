// Package compose 实现因子组合构建：把多个因子表达式机械合成为一个组合表达式，
// 交给同一个 DSL 引擎与评价引擎执行——保证组合与单因子的计算口径、样本、空值规则完全一致。
//
// 合成规则（对应技术方案 §4.4）：
//   1. 每个因子的表达式拆成「前置赋值」+「终值」两段，前置变量统一加前缀隔离，
//      避免两个因子都叫 mom20 时互相覆盖；
//   2. 终值先做当日截面 z-score 标准化（cs_zscore），否则量纲不同的因子直接加权
//      等于让市值类因子压倒一切；
//   3. 按权重（可带方向）线性合成：alpha = w1*z1 + w2*z2 + ...；
//   4. 空值按 NaN 传播（宁缺毋滥：任一分因子当日缺失，该股组合值即为空）。
package compose

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Part 是参与组合的一个因子。
type Part struct {
	ID        int     `json:"id"`
	Name      string  `json:"name"`
	Expr      string  `json:"expr"`
	Weight    float64 `json:"weight"`
	Direction int     `json:"direction"` // 1 = 正向使用，-1 = 反向使用
}

// Fragment 是拆分后的因子表达式。
type Fragment struct {
	Pre   []string // 前置赋值语句（变量已重命名）
	Final string   // 终值表达式（变量已重命名）
}

// identStart/identChar 判断 DSL 标识符字符。
func identStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func identChar(c byte) bool {
	return identStart(c) || (c >= '0' && c <= '9')
}

// renameVars 把表达式中的标识符按映射重命名（仅整词匹配，不会误伤函数名与字段名的子串）。
func renameVars(src string, mapping map[string]string) string {
	if len(mapping) == 0 {
		return src
	}
	var b strings.Builder
	i := 0
	for i < len(src) {
		c := src[i]
		if identStart(c) {
			j := i + 1
			for j < len(src) && identChar(src[j]) {
				j++
			}
			word := src[i:j]
			// 函数调用（后面紧跟 '('）不重命名
			k := j
			for k < len(src) && (src[k] == ' ' || src[k] == '\t') {
				k++
			}
			isCall := k < len(src) && src[k] == '('
			if to, ok := mapping[word]; ok && !isCall {
				b.WriteString(to)
			} else {
				b.WriteString(word)
			}
			i = j
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

// Parse 把一个因子表达式拆成前置赋值与终值，并把前置变量按 prefix 重命名。
//
// 支持的写法：每行 `变量 = 表达式`；空行与 `#` 注释忽略；
// 若最后一行是裸表达式（无赋值），该行即终值。
func Parse(expr, prefix string) (Fragment, error) {
	lines := make([]string, 0, 8)
	for _, raw := range strings.Split(expr, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return Fragment{}, fmt.Errorf("因子表达式为空")
	}

	mapping := make(map[string]string)
	pre := make([]string, 0, len(lines))
	var final string

	for idx, line := range lines {
		eq := strings.Index(line, "=")
		// 注意：排除 ==、<=、>=、!= 这类比较运算符作为赋值的情况
		for eq > 0 && (line[eq-1] == '=' || line[eq-1] == '!' || line[eq-1] == '<' || line[eq-1] == '>') {
			next := strings.Index(line[eq+1:], "=")
			if next < 0 {
				eq = -1
				break
			}
			eq = eq + 1 + next
		}
		isAssign := eq > 0 && identStart(line[0])

		last := idx == len(lines)-1
		if !isAssign {
			if !last {
				return Fragment{}, fmt.Errorf("第 %d 行既不是赋值也不是终值表达式：%s", idx+1, line)
			}
			final = line
			continue
		}

		name := strings.TrimSpace(line[:eq])
		for i := 0; i < len(name); i++ {
			if i == 0 && !identStart(name[0]) {
				return Fragment{}, fmt.Errorf("第 %d 行赋值目标不是合法标识符：%s", idx+1, name)
			}
			if i > 0 && !identChar(name[i]) {
				return Fragment{}, fmt.Errorf("第 %d 行赋值目标不是合法标识符：%s", idx+1, name)
			}
		}
		rhs := strings.TrimSpace(line[eq+1:])

		if last {
			// 最后一行的左值（通常叫 alpha）丢弃，右侧作为终值
			final = renameVars(rhs, mapping)
			continue
		}
		if _, dup := mapping[name]; dup {
			return Fragment{}, fmt.Errorf("变量 %s 重复定义", name)
		}
		mapping[name] = prefix + name
		pre = append(pre, mapping[name]+" = "+renameVars(rhs, mapping))
	}

	if strings.TrimSpace(final) == "" {
		return Fragment{}, fmt.Errorf("未找到因子终值表达式")
	}
	return Fragment{Pre: pre, Final: final}, nil
}

// Method 是权重生成方式。
const (
	MethodManual = "manual" // 用户给定权重
	MethodEqual  = "equal"  // 等权
)

// Build 生成组合表达式，并返回各分因子的标准化变量名（便于 UI 展示与调试）。
func Build(parts []Part, method string) (expr string, err error) {
	if len(parts) == 0 {
		return "", fmt.Errorf("至少选择一个因子")
	}
	if len(parts) > 20 {
		return "", fmt.Errorf("单次组合最多 20 个因子")
	}

	// 权重归一
	ws := make([]float64, len(parts))
	total := 0.0
	for i, p := range parts {
		w := p.Weight
		if method == MethodEqual || w == 0 {
			w = 1
		}
		if w < 0 {
			return "", fmt.Errorf("因子 %s 的权重不能为负（方向请用 direction 表达）", p.Name)
		}
		dir := p.Direction
		if dir == 0 {
			dir = 1
		}
		ws[i] = w * float64(dir)
		total += math.Abs(ws[i])
	}
	if total == 0 {
		return "", fmt.Errorf("权重之和为 0")
	}
	// 归一化：总绝对权重缩放到 1，使组合与单因子同量纲
	for i := range ws {
		ws[i] /= total
	}

	var b strings.Builder
	terms := make([]string, 0, len(parts))
	for i, p := range parts {
		prefix := fmt.Sprintf("p%d_", i)
		frag, err := Parse(p.Expr, prefix)
		if err != nil {
			return "", fmt.Errorf("因子「%s」解析失败：%w", p.Name, err)
		}
		fmt.Fprintf(&b, "# [%d] %s  weight=%.4f\n", i+1, p.Name, ws[i])
		for _, line := range frag.Pre {
			b.WriteString(line)
			b.WriteString("\n")
		}
		// 标准化后合成
		fmt.Fprintf(&b, "z%d = cs_zscore(%s)\n", i, frag.Final)
		terms = append(terms, fmt.Sprintf("z%d * %.6f", i, ws[i]))
	}
	b.WriteString("alpha = ")
	b.WriteString(strings.Join(terms, " + "))
	return b.String(), nil
}

// Label 返回组合的紧凑标签，如 "rev5(+0.40) + low_vol20(-0.20)"。
func Label(parts []Part) string {
	segs := make([]string, 0, len(parts))
	for _, p := range parts {
		d := "+"
		if p.Direction < 0 {
			d = "-"
		}
		segs = append(segs, fmt.Sprintf("%s(%s%.2f)", p.Name, d, p.Weight))
	}
	return strings.Join(segs, " + ")
}

// ---- 相关性检查（同质化预警） ----

// avgRank 返回升序平均秩（1-based，并列取平均）。
func avgRank(vals []float64) []float64 {
	m := len(vals)
	order := make([]int, m)
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool { return vals[order[a]] < vals[order[b]] })
	ranks := make([]float64, m)
	for i := 0; i < m; {
		j := i
		for j+1 < m && vals[order[j+1]] == vals[order[i]] {
			j++
		}
		avg := float64(i+j+2) / 2
		for k := i; k <= j; k++ {
			ranks[order[k]] = avg
		}
		i = j + 1
	}
	return ranks
}

// CrossCorr 计算两列因子值的「平均截面秩相关」（逐日 Spearman 后取均值）：
// 这是业内判断两个因子是否同质的标准口径，>0.7 通常视为高度重叠。
// 要求两列长度一致、布局为 i*N+j。
func CrossCorr(a, b []float64, T, N int) (float64, error) {
	if len(a) != T*N || len(b) != T*N {
		return 0, fmt.Errorf("列长度与面板不一致")
	}
	sum, cnt := 0.0, 0
	xs := make([]float64, 0, N)
	ys := make([]float64, 0, N)
	for i := 0; i < T; i++ {
		xs, ys = xs[:0], ys[:0]
		for j := 0; j < N; j++ {
			k := i*N + j
			va, vb := a[k], b[k]
			if math.IsNaN(va) || math.IsNaN(vb) {
				continue
			}
			xs = append(xs, va)
			ys = append(ys, vb)
		}
		if len(xs) < 10 { // 样本过少的截面对相关性没有信息量
			continue
		}
		rx, ry := avgRank(xs), avgRank(ys)
		c := pearson(rx, ry)
		if math.IsNaN(c) {
			continue
		}
		sum += c
		cnt++
	}
	if cnt == 0 {
		return math.NaN(), nil
	}
	return sum / float64(cnt), nil
}

func pearson(xs, ys []float64) float64 {
	n := len(xs)
	if n < 2 {
		return math.NaN()
	}
	mx, my := 0.0, 0.0
	for i := range xs {
		mx += xs[i]
		my += ys[i]
	}
	mx /= float64(n)
	my /= float64(n)
	sxy, sxx, syy := 0.0, 0.0, 0.0
	for i := range xs {
		dx, dy := xs[i]-mx, ys[i]-my
		sxy += dx * dy
		sxx += dx * dx
		syy += dy * dy
	}
	if sxx == 0 || syy == 0 {
		return math.NaN()
	}
	return sxy / math.Sqrt(sxx*syy)
}
