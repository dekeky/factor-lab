package api

import (
	"context"
	"testing"
	"time"

	"factor-lab/internal/panel"
)

func testPanel(t *testing.T, codes ...string) *panel.Panel {
	t.Helper()
	dates := make([]time.Time, 40)
	d := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	for i := range dates {
		dates[i] = d
		d = d.AddDate(0, 0, 1)
	}
	if len(codes) == 0 {
		codes = []string{"000001.SZ", "600000.SH"}
	}
	p := panel.New(dates, codes)
	col := make(panel.Col, p.Size())
	if err := p.SetCol("close", col); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPanelForUsesStartupPool(t *testing.T) {
	p := testPanel(t)
	s, err := New(p, nil, "", "", Options{StockPool: "000300.SH"})
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := s.panelFor(context.Background(), evalConfig{Universe: "000300.SH", Horizon: 1, Groups: 10})
	if err != nil {
		t.Fatal(err)
	}
	if got.N() != p.N() {
		t.Fatalf("应复用启动面板，股票数 %d != %d", got.N(), p.N())
	}
}

func TestPanelForLoadsOtherUniverseOnce(t *testing.T) {
	p := testPanel(t, "000001.SZ")
	loads := 0
	star := testPanel(t, "688001.SH", "688002.SH")
	s, err := New(p, nil, "", "", Options{
		StockPool: "000300.SH",
		LoadUniverse: func(ctx context.Context, index string) (*panel.Panel, error) {
			loads++
			if index != "000688.SH" {
				t.Fatalf("unexpected index %s", index)
			}
			return star, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := evalConfig{Universe: "000688.SH", Horizon: 1, Groups: 5, Cost: 0.003}
	got, _, err := s.panelFor(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got.N() != 2 || got.Codes[0] != "688001.SH" {
		t.Fatalf("未切到科创50 面板: %+v", got.Codes)
	}
	_, _, err = s.panelFor(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if loads != 1 {
		t.Fatalf("股票池应缓存，实际加载 %d 次", loads)
	}
}

func TestPanelForRejectsSwitchWithoutLoader(t *testing.T) {
	p := testPanel(t)
	s, err := New(p, nil, "", "", Options{StockPool: "000300.SH"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.panelFor(context.Background(), evalConfig{Universe: "000688.SH"})
	if err == nil {
		t.Fatal("无 LoadUniverse 时切换股票池应失败")
	}
}

func TestPanelForCSI300Alias(t *testing.T) {
	p := testPanel(t)
	s, err := New(p, nil, "", "", Options{
		StockPool: "000300.SH",
		LoadUniverse: func(ctx context.Context, index string) (*panel.Panel, error) {
			t.Fatalf("沪深300 深市别名不应重新加载，得到 %s", index)
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.panelFor(context.Background(), evalConfig{Universe: "399300.SZ"}); err != nil {
		t.Fatal(err)
	}
}
