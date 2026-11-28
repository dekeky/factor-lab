package api

import (
	"testing"
	"time"

	"factor-lab/internal/data"
	"factor-lab/internal/factors"
	"factor-lab/internal/panel"
)

func TestBuildHelpDocReflectsRuntimeCapabilities(t *testing.T) {
	p := panel.New([]time.Time{time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}, []string{"000001.SZ"})
	if err := p.SetCol("close", panel.Col{10}); err != nil {
		t.Fatal(err)
	}
	if err := p.SetCol("custom_signal", panel.Col{1}); err != nil {
		t.Fatal(err)
	}

	doc := buildHelpDoc(p)
	if doc.DSL.MaxWindow != 512 || doc.DSL.MaxDepth != 64 {
		t.Fatalf("unexpected DSL limits: %+v", doc.DSL)
	}
	if len(doc.Fields) != 2 || doc.Fields[0].Name != "close" || doc.Fields[1].Name != "custom_signal" {
		t.Fatalf("fields should come from the current panel and stay sorted: %+v", doc.Fields)
	}
	if doc.Fields[0].Description == "" || doc.Fields[0].Tip == "" {
		t.Fatal("known fields should include documentation and a plain-language tip")
	}
	if doc.Fields[1].Tip != "" {
		t.Fatal("custom fields have no built-in tip")
	}

	foundTSMean := false
	for _, op := range doc.Operators {
		if op.Name == "and" || op.Name == "or" || op.Name == "not" {
			t.Fatalf("language keywords must not be exposed as callable functions: %s", op.Name)
		}
		if op.Name == "ts_mean" {
			foundTSMean = op.Signature == "ts_mean(x, window)" && op.Description != "" && op.Example != ""
		}
		// 帮助页面向新手：引擎里注册的每个算子都必须有文档和通俗解释，新增算子时不能漏写。
		if op.Category == "其他" || op.Tip == "" {
			t.Fatalf("operator %s is missing documentation or a plain-language tip", op.Name)
		}
	}
	if !foundTSMean {
		t.Fatal("ts_mean should include a documented signature and example")
	}

	if len(doc.Builtins) != len(factors.Classics) {
		t.Fatalf("built-in catalog drifted: got %d, want %d", len(doc.Builtins), len(factors.Classics))
	}
	if len(doc.Builtins) == 0 || doc.Builtins[0].Expr == "" || doc.Builtins[0].Description == "" {
		t.Fatal("built-in factors should include expressions and descriptions")
	}
}

func TestDailyBasicFieldsHaveDocs(t *testing.T) {
	for _, name := range data.DailyBasicCols {
		item, ok := data.FieldInfo(name)
		if !ok || item.Description == "" || item.Unit == "" || item.Tip == "" {
			t.Errorf("%s 缺少帮助文档（description/unit/tip）", name)
		}
	}
}
