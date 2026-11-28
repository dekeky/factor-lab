package api

import (
	"net/http"

	"factor-lab/internal/data"
	"factor-lab/internal/dsl"
	"factor-lab/internal/factors"
	"factor-lab/internal/ops"
	"factor-lab/internal/panel"
)

type helpDSL struct {
	StatementSeparators []string `json:"statement_separators"`
	Comment             string   `json:"comment"`
	Result              string   `json:"result"`
	Precedence          []string `json:"precedence"`
	MaxWindow           int      `json:"max_window"`
	MaxDepth            int      `json:"max_depth"`
}

type helpField struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Unit        string `json:"unit,omitempty"`
	Tip         string `json:"tip,omitempty"` // 面向新手的通俗解释
}

type helpOperator struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Signature   string `json:"signature"`
	Description string `json:"description"`
	Example     string `json:"example"`
	Tip         string `json:"tip,omitempty"` // 面向新手的通俗解释
}

type helpBuiltin struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Expr        string `json:"expr"`
	Description string `json:"description"`
}

type helpDoc struct {
	DSL       helpDSL        `json:"dsl"`
	Fields    []helpField    `json:"fields"`
	Operators []helpOperator `json:"operators"`
	Builtins  []helpBuiltin  `json:"builtins"`
}

func buildHelpDoc(p *panel.Panel) helpDoc {
	doc := helpDoc{
		DSL: helpDSL{
			StatementSeparators: []string{"换行", "分号 ;"},
			Comment:             "# 到行尾",
			Result:              "最后一条语句的值作为因子结果",
			Precedence:          []string{"()", "一元 - / not", "* /", "+ -", "< <= > >=", "== !=", "and", "or"},
			MaxWindow:           dsl.MaxWindow,
			MaxDepth:            64,
		},
	}

	for _, item := range data.DescribeFields(p) {
		doc.Fields = append(doc.Fields, helpField{
			Name: item.Name, Description: item.Description, Unit: item.Unit, Tip: item.Tip,
		})
	}
	for _, item := range ops.Infos() {
		doc.Operators = append(doc.Operators, helpOperator{
			Name: item.Name, Category: item.Category, Signature: item.Signature,
			Description: item.Description, Example: item.Example, Tip: item.Tip,
		})
	}
	for _, item := range factors.Classics {
		doc.Builtins = append(doc.Builtins, helpBuiltin{
			Name: item.Name, Category: item.Category, Expr: item.Expr, Description: item.Desc,
		})
	}
	return doc
}

func (s *Server) handleHelp(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, buildHelpDoc(s.panel()))
}
