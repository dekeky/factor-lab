package agent

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"factor-lab/internal/data"
	"factor-lab/internal/engine"
	"factor-lab/internal/ops"
	"factor-lab/internal/panel"
)

const (
	toolListFields  = "list_fields"
	toolListOps     = "list_ops"
	toolListFactors = "list_factors"
	toolCheckExpr   = "check_expr"
	toolCheckCombo  = "check_combo"
)

type noArgs struct{}

type checkExprArgs struct {
	Expr string `json:"expr" jsonschema:"description=完整可独立运行的因子表达式，换行用 \\n"`
}

type checkExprOut struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors,omitempty"`
}

type checkComboArgs struct {
	Name   string              `json:"name" jsonschema:"description=组合名称"`
	Method string              `json:"method" jsonschema:"description=equal 或 manual"`
	Items  []ComboProposalItem `json:"items" jsonschema:"description=成分，id 必须来自 list_factors"`
}

type checkComboOut struct {
	Valid  bool           `json:"valid"`
	Errors []string       `json:"errors,omitempty"`
	Combo  *ComboProposal `json:"combo,omitempty"`
}

type listFactorsOut struct {
	Factors []factorToolItem `json:"factors"`
}

type factorToolItem struct {
	ID        int     `json:"id"`
	Name      string  `json:"name"`
	Category  string  `json:"category"`
	RankIC    float64 `json:"rank_ic,omitempty"`
	ICIR      float64 `json:"icir,omitempty"`
	Available bool    `json:"available"`
}

func factorTools(p *panel.Panel) ([]tool.BaseTool, error) {
	listFields, err := utils.InferTool(toolListFields,
		"返回当前数据面板的全部可用字段（因子输入列）。数量不多，一次返回完整列表；写表达式前先调用。",
		func(_ context.Context, _ noArgs) ([]data.Field, error) {
			return data.DescribeFields(p), nil
		})
	if err != nil {
		return nil, err
	}
	listOps, err := utils.InferTool(toolListOps,
		"返回引擎支持的全部 DSL 算子/指标（时序 ts_、截面 cs_、数学与条件）。数量不多，一次返回完整列表；写表达式前先调用。",
		func(_ context.Context, _ noArgs) ([]ops.Info, error) {
			return ops.Infos(), nil
		})
	if err != nil {
		return nil, err
	}
	check, err := utils.InferTool(toolCheckExpr,
		"用引擎静态校验一段因子表达式（字段、算子、窗口）。提交给用户前应先自检；valid=false 时按 errors 修正后再调。",
		func(_ context.Context, in checkExprArgs) (checkExprOut, error) {
			if p == nil {
				return checkExprOut{Valid: false, Errors: []string{"数据面板不可用"}}, nil
			}
			errs := engine.Check(in.Expr, p)
			return checkExprOut{Valid: len(errs) == 0, Errors: errs}, nil
		})
	if err != nil {
		return nil, err
	}
	return []tool.BaseTool{listFields, listOps, check}, nil
}

func comboTools(catalog []FactorRef) ([]tool.BaseTool, error) {
	list, err := utils.InferTool(toolListFactors,
		"返回当前因子库的全部因子（id、名称、类别、RankIC、ICIR、是否可用）。数量不多，一次返回完整列表，不要发明 id。",
		func(_ context.Context, _ noArgs) (listFactorsOut, error) {
			items := make([]factorToolItem, 0, len(catalog))
			for _, f := range catalog {
				items = append(items, factorToolItem{
					ID: f.ID, Name: f.Name, Category: f.Category,
					RankIC: f.RankIC, ICIR: f.ICIR, Available: f.Available,
				})
			}
			return listFactorsOut{Factors: items}, nil
		})
	if err != nil {
		return nil, err
	}
	check, err := utils.InferTool(toolCheckCombo,
		"校验组合方案：id 必须在因子库且可用，至少 2 个，method 为 equal 或 manual，direction 为 1 或 -1。提交前应先自检。",
		func(_ context.Context, in checkComboArgs) (checkComboOut, error) {
			normalized, errs := validateCombo(&ComboProposal{Name: in.Name, Method: in.Method, Items: in.Items}, catalog)
			return checkComboOut{Valid: len(errs) == 0 && normalized != nil, Errors: errs, Combo: normalized}, nil
		})
	if err != nil {
		return nil, err
	}
	return []tool.BaseTool{list, check}, nil
}

func unknownToolMessage(_ context.Context, name, _ string) (string, error) {
	return fmt.Sprintf("未知工具 %q，请只使用已提供的工具。", name), nil
}
