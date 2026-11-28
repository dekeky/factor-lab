package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"

	"factor-lab/internal/data"
)

func TestFactorToolsReturnFullCatalog(t *testing.T) {
	src := &data.SyntheticSource{Days: 40, Stocks: 10, Seed: 1}
	p, err := src.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tools, err := factorTools(p)
	if err != nil {
		t.Fatal(err)
	}
	byName := toolByName(t, tools)
	fieldsJSON := invokeTool(t, byName[toolListFields], "{}")
	if !strings.Contains(fieldsJSON, `"name":"close"`) || !strings.Contains(fieldsJSON, `"name":"pct_chg"`) {
		t.Fatalf("list_fields 应返回全部面板字段: %s", fieldsJSON)
	}
	opsJSON := invokeTool(t, byName[toolListOps], "{}")
	for _, name := range []string{"ts_mean", "cs_rank", "if"} {
		if !strings.Contains(opsJSON, `"name":"`+name+`"`) {
			t.Fatalf("list_ops 缺少 %s: %s", name, opsJSON)
		}
	}
	if strings.Contains(opsJSON, `"name":"and"`) {
		t.Fatal("and/or/not 不应作为可调用算子列出")
	}

	ok := invokeTool(t, byName[toolCheckExpr], `{"expr":"alpha = -1 * cs_rank(ts_std_dev(pct_chg, 20))"}`)
	if !strings.Contains(ok, `"valid":true`) {
		t.Fatalf("合法表达式应通过: %s", ok)
	}
	bad := invokeTool(t, byName[toolCheckExpr], `{"expr":"alpha = no_such_op(close, 5)"}`)
	if strings.Contains(bad, `"valid":true`) {
		t.Fatalf("非法表达式不应通过: %s", bad)
	}
}

func TestComboToolsReturnAllFactors(t *testing.T) {
	tools, err := comboTools(testFactorRefs())
	if err != nil {
		t.Fatal(err)
	}
	byName := toolByName(t, tools)
	out := invokeTool(t, byName[toolListFactors], "{}")
	if !strings.Contains(out, `"id":1`) || !strings.Contains(out, `"id":4`) || !strings.Contains(out, `"name":"停用动量"`) {
		t.Fatalf("list_factors 应一次返回全部因子: %s", out)
	}
	ok := invokeTool(t, byName[toolCheckCombo], `{"name":"反转+低波","method":"equal","items":[{"id":1,"weight":1,"direction":1},{"id":2,"weight":1,"direction":-1}]}`)
	if !strings.Contains(ok, `"valid":true`) {
		t.Fatalf("合法组合应通过: %s", ok)
	}
	bad := invokeTool(t, byName[toolCheckCombo], `{"name":"坏","method":"equal","items":[{"id":99,"weight":1,"direction":1},{"id":1,"weight":1,"direction":1}]}`)
	if strings.Contains(bad, `"valid":true`) {
		t.Fatalf("未知 id 不应通过: %s", bad)
	}
}

func toolByName(t *testing.T, tools []tool.BaseTool) map[string]tool.InvokableTool {
	t.Helper()
	out := map[string]tool.InvokableTool{}
	for _, bt := range tools {
		info, err := bt.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		inv, ok := bt.(tool.InvokableTool)
		if !ok {
			t.Fatalf("%s 不是 InvokableTool", info.Name)
		}
		out[info.Name] = inv
	}
	return out
}

func invokeTool(t *testing.T, inv tool.InvokableTool, args string) string {
	t.Helper()
	out, err := inv.InvokableRun(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
