package api

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"factor-lab/internal/panel"
)

func TestKLinePrefersQfqAndScalesHighLow(t *testing.T) {
	dates := []time.Time{
		time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC),
	}
	p := panel.New(dates, []string{"600426.SH"})
	p.SetNames([]string{"华鲁恒升"})
	n := p.Size()
	col := func(vals ...float64) panel.Col {
		out := make(panel.Col, n)
		copy(out, vals)
		return out
	}
	_ = p.SetCol("open", col(20, 22))
	_ = p.SetCol("high", col(24, 26))
	_ = p.SetCol("low", col(18, 20))
	_ = p.SetCol("close", col(22, 24))
	_ = p.SetCol("vol", col(1000, 1100))
	_ = p.SetCol("qfq_open", col(10, 11))
	_ = p.SetCol("qfq_close", col(11, 12))

	srv, err := New(p, nil, "", "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	res, err := http.Get(ts.URL + "/api/kline?code=600426.SH")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %s", res.Status)
	}
	var out klineResp
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Name != "华鲁恒升" || out.Code != "600426.SH" {
		t.Fatalf("meta %+v", out)
	}
	if len(out.Bars) != 2 {
		t.Fatalf("bars %d", len(out.Bars))
	}
	b0 := out.Bars[0]
	if b0.Time != "2024-01-02" {
		t.Errorf("time %s", b0.Time)
	}
	if math.Abs(b0.Open-10) > 1e-9 || math.Abs(b0.Close-11) > 1e-9 {
		t.Errorf("应使用前复权开收盘，实际 open=%v close=%v", b0.Open, b0.Close)
	}
	// high/low 按 close 比例缩到前复权：24*(11/22)=12，18*(11/22)=9
	if math.Abs(b0.High-12) > 1e-9 || math.Abs(b0.Low-9) > 1e-9 {
		t.Errorf("high/low 应按前复权缩放，实际 high=%v low=%v", b0.High, b0.Low)
	}
	if math.Abs(b0.Volume-1000) > 1e-9 {
		t.Errorf("volume %v", b0.Volume)
	}
}

func TestKLineUnknownCode(t *testing.T) {
	p := panel.New([]time.Time{time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)}, []string{"000001.SZ"})
	_ = p.SetCol("open", panel.Col{10})
	_ = p.SetCol("high", panel.Col{11})
	_ = p.SetCol("low", panel.Col{9})
	_ = p.SetCol("close", panel.Col{10})
	srv, err := New(p, nil, "", "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	res, err := http.Get(ts.URL + "/api/kline?code=600426.SH")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("缺代码应 404，实际 %s", res.Status)
	}
}
