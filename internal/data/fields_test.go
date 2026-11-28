package data

import (
	"testing"
	"time"

	"factor-lab/internal/panel"
)

func TestDailyBasicFieldsHaveDocs(t *testing.T) {
	for _, name := range DailyBasicCols {
		item, ok := FieldInfo(name)
		if !ok || item.Description == "" || item.Unit == "" || item.Tip == "" {
			t.Errorf("%s 缺少字段说明（description/unit/tip）", name)
		}
	}
	for _, name := range append(requiredFields, "pct_chg", "roe") {
		item, ok := FieldInfo(name)
		if !ok || item.Description == "" || item.Tip == "" {
			t.Errorf("%s 缺少字段说明", name)
		}
	}
}

func TestDescribeFieldsSkipsHidden(t *testing.T) {
	p := panel.New([]time.Time{time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}, []string{"000001.SZ"})
	if err := p.SetCol("close", panel.Col{10}); err != nil {
		t.Fatal(err)
	}
	if err := p.SetCol("raw_close", panel.Col{10}); err != nil {
		t.Fatal(err)
	}
	got := DescribeFields(p)
	if len(got) != 1 || got[0].Name != "close" {
		t.Fatalf("应只展示 close，实际 %+v", got)
	}
}
