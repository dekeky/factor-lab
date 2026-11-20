package dsl

import "fmt"

// TokKind 是记号类型。
type TokKind int

const (
	TokEOF TokKind = iota
	TokIdent
	TokNumber
	TokPlus
	TokMinus
	TokStar
	TokSlash
	TokLParen
	TokRParen
	TokComma
	TokAssign
	TokLT
	TokLE
	TokGT
	TokGE
	TokEq
	TokNE
	TokAnd
	TokOr
	TokNot
	TokNewline
	TokSemi
)

// Token 是单个记号。
type Token struct {
	Kind TokKind
	Text string
	Line int
}

func (t Token) String() string {
	if t.Kind == TokEOF {
		return fmt.Sprintf("EOF(第%d行)", t.Line)
	}
	return fmt.Sprintf("%q(第%d行)", t.Text, t.Line)
}

var keywords = map[string]TokKind{
	"and": TokAnd,
	"or":  TokOr,
	"not": TokNot,
}

// Lex 把源码切成记号流。注释（# 到行尾）与空白被跳过，换行保留为语句分隔。
func Lex(src string) ([]Token, []error) {
	var toks []Token
	var errs []error
	line := 1
	i := 0
	n := len(src)
	push := func(k TokKind, text string) {
		toks = append(toks, Token{Kind: k, Text: text, Line: line})
	}
	for i < n {
		c := src[i]
		switch {
		case c == '\n':
			push(TokNewline, "\n")
			line++
			i++
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case c == '#':
			for i < n && src[i] != '\n' {
				i++
			}
		case isDigit(c) || (c == '.' && i+1 < n && isDigit(src[i+1])):
			j := i
			for j < n && (isDigit(src[j]) || src[j] == '.') {
				j++
			}
			// 科学计数法
			if j < n && (src[j] == 'e' || src[j] == 'E') {
				k := j + 1
				if k < n && (src[k] == '+' || src[k] == '-') {
					k++
				}
				if k < n && isDigit(src[k]) {
					for k < n && isDigit(src[k]) {
						k++
					}
					j = k
				}
			}
			push(TokNumber, src[i:j])
			i = j
		case isIdentStart(c):
			j := i
			for j < n && isIdentPart(src[j]) {
				j++
			}
			word := src[i:j]
			if kw, ok := keywords[word]; ok {
				push(kw, word)
			} else {
				push(TokIdent, word)
			}
			i = j
		default:
			pairs := map[byte]TokKind{
				'+': TokPlus, '-': TokMinus, '*': TokStar, '/': TokSlash,
				'(': TokLParen, ')': TokRParen, ',': TokComma, ';': TokSemi,
			}
			if k, ok := pairs[c]; ok {
				push(k, string(c))
				i++
				continue
			}
			two := ""
			if i+1 < n {
				two = src[i : i+2]
			}
			switch two {
			case "<=", ">=", "==", "!=":
				switch two {
				case "<=":
					push(TokLE, two)
				case ">=":
					push(TokGE, two)
				case "==":
					push(TokEq, two)
				default:
					push(TokNE, two)
				}
				i += 2
				continue
			}
			if c == '=' {
				push(TokAssign, "=")
				i++
				continue
			}
			if c == '<' {
				push(TokLT, "<")
				i++
				continue
			}
			if c == '>' {
				push(TokGT, ">")
				i++
				continue
			}
			errs = append(errs, fmt.Errorf("第%d行: 非法字符 %q", line, string(c)))
			i++
		}
	}
	push(TokEOF, "")
	return toks, errs
}

func isDigit(c byte) bool      { return c >= '0' && c <= '9' }
func isIdentStart(c byte) bool { return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isIdentPart(c byte) bool  { return isIdentStart(c) || isDigit(c) }
