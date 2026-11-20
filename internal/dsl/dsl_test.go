package dsl

import "testing"

func TestParseBasic(t *testing.T) {
	src := `
# 反转因子
mom20 = close / ts_delay(close, 20) - 1
alpha = -1 * cs_rank(mom20)
`
	prog, errs := Parse(src)
	if len(errs) > 0 {
		t.Fatalf("解析失败: %v", errs)
	}
	if len(prog.Stmts) != 2 {
		t.Fatalf("语句数 = %d, 期望 2", len(prog.Stmts))
	}
	if _, ok := prog.Stmts[0].(*AssignStmt); !ok {
		t.Fatalf("第一条应为赋值语句, 实际 %T", prog.Stmts[0])
	}
}

func TestParseCallAndIf(t *testing.T) {
	src := `
down = if(pct_chg < 0, ts_std_dev(pct_chg, 20), close)
alpha = cs_rank(ts_arg_max(down * down, 5)) - 0.5
`
	if _, errs := Parse(src); len(errs) > 0 {
		t.Fatalf("解析失败: %v", errs)
	}
}

func TestParsePrecedence(t *testing.T) {
	// a + b * c 应解析为 a + (b*c)
	prog := MustParse("alpha = a + b * c\n")
	st := prog.Stmts[0].(*AssignStmt)
	bin := st.Value.(*BinExpr)
	if bin.Op != "+" {
		t.Fatalf("根运算符 = %q, 期望 +", bin.Op)
	}
	if _, ok := bin.R.(*BinExpr); !ok {
		t.Fatalf("右侧应为乘法子表达式")
	}
}

func TestParseErrors(t *testing.T) {
	cases := []string{
		"alpha = ",     // 缺表达式
		"alpha = ((close", // 括号不配平
		"alpha = 1 +",  // 悬空运算符
	}
	for _, src := range cases {
		if _, errs := Parse(src); len(errs) == 0 {
			t.Errorf("源码 %q 应该解析失败", src)
		}
	}
}

func TestValidateUnknownIdent(t *testing.T) {
	prog := MustParse("alpha = cs_rank(no_such_field)\n")
	funcs := map[string]FuncSig{"cs_rank": {Name: "cs_rank", MinArgs: 1, MaxArgs: 1}}
	errs := Validate(prog, funcs, map[string]bool{"close": true})
	if len(errs) == 0 {
		t.Fatal("未知字段应报错")
	}
}

func TestValidateConstArg(t *testing.T) {
	// ts_delay 窗口必须是常数
	prog := MustParse("alpha = ts_delay(close, n)\n")
	funcs := map[string]FuncSig{"ts_delay": {Name: "ts_delay", MinArgs: 2, MaxArgs: 2, ConstArgs: []int{1}}}
	errs := Validate(prog, funcs, map[string]bool{"close": true, "n": true})
	if len(errs) == 0 {
		t.Fatal("非常数窗口应报错")
	}

	// 变量先定义后使用
	prog = MustParse("w = 20\nalpha = ts_delay(close, 20)\n")
	if errs := Validate(prog, funcs, map[string]bool{"close": true}); len(errs) > 0 {
		t.Fatalf("合法表达式不应报错: %v", errs)
	}
}

func TestValidateWindowRange(t *testing.T) {
	prog := MustParse("alpha = ts_delay(close, 5000)\n")
	funcs := map[string]FuncSig{"ts_delay": {Name: "ts_delay", MinArgs: 2, MaxArgs: 2, ConstArgs: []int{1}}}
	if errs := Validate(prog, funcs, map[string]bool{"close": true}); len(errs) == 0 {
		t.Fatal("超长窗口应报错")
	}
}
