package dsl

// Node 是 AST 节点。Pos 返回记号行号，用于错误定位。
type Node interface{ Pos() int }

// NumLit 数字字面量（会广播为常数列）。
type NumLit struct {
	V    float64
	Line int
}

// IdentRef 字段引用或中间变量引用。
type IdentRef struct {
	Name string
	Line int
}

// BinExpr 二元运算：+ - * / < <= > >= == != and or。
// 比较与逻辑运算结果为 1.0 / 0.0；NaN 参与比较按 0（假）处理。
type BinExpr struct {
	Op   string
	L, R Node
	Line int
}

// UnExpr 一元运算（- / not）。
type UnExpr struct {
	Op   string
	X    Node
	Line int
}

// CallExpr 函数调用（算子）。参数中的常数位（如窗口长度）由校验层约束为 NumLit。
type CallExpr struct {
	Fun  string
	Args []Node
	Line int
}

// AssignStmt 中间变量赋值：name = expr。
type AssignStmt struct {
	Name  string
	Value Node
	Line  int
}

// ExprStmt 裸表达式语句；程序中最后一个求值结果即因子值。
type ExprStmt struct {
	X    Node
	Line int
}

// Program 是一段因子源码（语句序列，换行或分号分隔）。
type Program struct {
	Stmts []Node
}

func (n *NumLit) Pos() int     { return n.Line }
func (n *IdentRef) Pos() int   { return n.Line }
func (n *BinExpr) Pos() int    { return n.Line }
func (n *UnExpr) Pos() int     { return n.Line }
func (n *CallExpr) Pos() int   { return n.Line }
func (n *AssignStmt) Pos() int { return n.Line }
func (n *ExprStmt) Pos() int   { return n.Line }
