package dsl

import (
	"fmt"
	"strconv"
	"strings"
)

// Parse 把 DSL 源码解析为 Program。
// 语法（自上而下）：
//
//	program  := stmt (';' | 换行) stmt ...
//	stmt     := IDENT '=' expr | expr
//	expr     := or
//	or       := and ('or' and)*
//	and      := eq ('and' eq)*
//	eq       := cmp (('=='|'!=') cmp)*
//	cmp      := add (('<'|'<='|'>'|'>=') add)*
//	add      := mul (('+'|'-') mul)*
//	mul      := unary (('*'|'/') unary)*
//	unary    := ('-'|'not') unary | primary
//	primary  := NUMBER | IDENT | IDENT '(' args ')' | '(' expr ')'
type parser struct {
	toks []Token
	pos  int
}

// Parse 解析；返回 Program 与错误列表（错误聚合而非首错即停，便于编辑器批量提示）。
func Parse(src string) (*Program, []error) {
	toks, errs := Lex(src)
	if len(errs) > 0 {
		return nil, errs
	}
	p := &parser{toks: toks}
	var stmts []Node
	for !p.at(TokEOF) {
		if p.at(TokNewline) || p.at(TokSemi) {
			p.next()
			continue
		}
		st, err := p.parseStmt()
		if err != nil {
			errs = append(errs, err)
			p.recover()
			continue
		}
		if st != nil {
			stmts = append(stmts, st)
		}
	}
	if len(stmts) == 0 {
		errs = append(errs, fmt.Errorf("空表达式：至少需要一行因子定义"))
	}
	if len(errs) > 0 {
		return nil, errs
	}
	return &Program{Stmts: stmts}, nil
}

// MustParse 测试与内部使用的便捷版本。
func MustParse(src string) *Program {
	prog, errs := Parse(src)
	if len(errs) > 0 {
		panic(strings.Join(errs2str(errs), "; "))
	}
	return prog
}

func errs2str(errs []error) []string {
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.Error()
	}
	return out
}

func (p *parser) cur() Token  { return p.toks[p.pos] }
func (p *parser) next() Token { t := p.toks[p.pos]; p.pos++; return t }
func (p *parser) at(k TokKind) bool { return p.cur().Kind == k }
func (p *parser) accept(k TokKind) bool {
	if p.at(k) {
		p.next()
		return true
	}
	return false
}
func (p *parser) expect(k TokKind, what string) error {
	if !p.at(k) {
		return fmt.Errorf("第%d行: 期望 %s，实际是 %s", p.cur().Line, what, p.cur())
	}
	p.next()
	return nil
}

// recover 跳到语句边界，保证一次错误不拖垮整段解析。
func (p *parser) recover() {
	for !p.at(TokEOF) {
		if p.at(TokNewline) || p.at(TokSemi) {
			return
		}
		p.next()
	}
}

func (p *parser) parseStmt() (Node, error) {
	line := p.cur().Line
	// IDENT '=' expr 赋值；否则按裸表达式
	if p.at(TokIdent) && p.toks[p.pos+1].Kind == TokAssign {
		name := p.next().Text
		p.next() // '='
		v, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return &AssignStmt{Name: name, Value: v, Line: line}, nil
	}
	x, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return &ExprStmt{X: x, Line: line}, nil
}

func (p *parser) parseExpr() (Node, error) { return p.parseOr() }

func (p *parser) parseOr() (Node, error) {
	l, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.at(TokOr) {
		line := p.next().Line
		r, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		l = &BinExpr{Op: "or", L: l, R: r, Line: line}
	}
	return l, nil
}

func (p *parser) parseAnd() (Node, error) {
	l, err := p.parseEq()
	if err != nil {
		return nil, err
	}
	for p.at(TokAnd) {
		line := p.next().Line
		r, err := p.parseEq()
		if err != nil {
			return nil, err
		}
		l = &BinExpr{Op: "and", L: l, R: r, Line: line}
	}
	return l, nil
}

func (p *parser) parseEq() (Node, error) {
	l, err := p.parseCmp()
	if err != nil {
		return nil, err
	}
	for p.at(TokEq) || p.at(TokNE) {
		t := p.next()
		r, err := p.parseCmp()
		if err != nil {
			return nil, err
		}
		l = &BinExpr{Op: t.Text, L: l, R: r, Line: t.Line}
	}
	return l, nil
}

func (p *parser) parseCmp() (Node, error) {
	l, err := p.parseAdd()
	if err != nil {
		return nil, err
	}
	for p.at(TokLT) || p.at(TokLE) || p.at(TokGT) || p.at(TokGE) {
		t := p.next()
		r, err := p.parseAdd()
		if err != nil {
			return nil, err
		}
		l = &BinExpr{Op: t.Text, L: l, R: r, Line: t.Line}
	}
	return l, nil
}

func (p *parser) parseAdd() (Node, error) {
	l, err := p.parseMul()
	if err != nil {
		return nil, err
	}
	for p.at(TokPlus) || p.at(TokMinus) {
		t := p.next()
		r, err := p.parseMul()
		if err != nil {
			return nil, err
		}
		l = &BinExpr{Op: t.Text, L: l, R: r, Line: t.Line}
	}
	return l, nil
}

func (p *parser) parseMul() (Node, error) {
	l, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.at(TokStar) || p.at(TokSlash) {
		t := p.next()
		r, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		l = &BinExpr{Op: t.Text, L: l, R: r, Line: t.Line}
	}
	return l, nil
}

func (p *parser) parseUnary() (Node, error) {
	if p.at(TokMinus) {
		t := p.next()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &UnExpr{Op: "-", X: x, Line: t.Line}, nil
	}
	if p.at(TokNot) {
		t := p.next()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &UnExpr{Op: "not", X: x, Line: t.Line}, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (Node, error) {
	t := p.cur()
	switch t.Kind {
	case TokNumber:
		p.next()
		v, err := strconv.ParseFloat(t.Text, 64)
		if err != nil {
			return nil, fmt.Errorf("第%d行: 非法数字 %q", t.Line, t.Text)
		}
		return &NumLit{V: v, Line: t.Line}, nil
	case TokIdent:
		p.next()
		if p.accept(TokLParen) { // 函数调用
			call := &CallExpr{Fun: t.Text, Line: t.Line}
			if p.accept(TokRParen) {
				return call, nil
			}
			for {
				arg, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				call.Args = append(call.Args, arg)
				if p.accept(TokComma) {
					continue
				}
				if err := p.expect(TokRParen, "右括号 ')'"); err != nil {
					return nil, err
				}
				break
			}
			return call, nil
		}
		return &IdentRef{Name: t.Text, Line: t.Line}, nil
	case TokLParen:
		p.next()
		x, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(TokRParen, "右括号 ')'"); err != nil {
			return nil, err
		}
		return x, nil
	default:
		return nil, fmt.Errorf("第%d行: 意外的记号 %s（期望数字/标识符/函数调用/'('）", t.Line, t)
	}
}
