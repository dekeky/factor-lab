package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"factor-lab/internal/data"
	"factor-lab/internal/paper"
)

func TestPaperAccountPicksTopN(t *testing.T) {
	src := &data.SyntheticSource{Days: 40, Stocks: 20, Seed: 1}
	p, err := src.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(p, nil, "", "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	factorBody := bytes.NewBufferString(`{"name":"rev5","expr":"alpha = -1 * cs_rank(close / ts_delay(close, 5) - 1)"}`)
	res, err := http.Post(ts.URL+"/api/factors", "application/json", factorBody)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var f Factor
	if err := json.NewDecoder(res.Body).Decode(&f); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusOK {
		t.Fatalf("创建因子失败: %s", res.Status)
	}

	body, _ := json.Marshal(map[string]any{
		"name": "rev5 实盘", "factor_id": f.ID, "top_n": 5, "capital": 100000, "cost": 0.003,
	})
	res, err = http.Post(ts.URL+"/api/paper", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		var raw map[string]any
		_ = json.NewDecoder(res.Body).Decode(&raw)
		t.Fatalf("创建实盘失败 %s: %v", res.Status, raw)
	}
	var acc paper.Account
	if err := json.NewDecoder(res.Body).Decode(&acc); err != nil {
		t.Fatal(err)
	}
	if len(acc.Days) < 10 {
		t.Fatalf("成交日数太少: %d", len(acc.Days))
	}
	d := acc.Days[len(acc.Days)-1]
	if len(d.Picks) != 5 {
		t.Fatalf("每日应选出 Top 5，实际 %d: %+v", len(d.Picks), d.Picks)
	}
	if acc.Summary == nil || acc.Summary.NAV <= 0 {
		t.Fatalf("摘要净值异常: %+v", acc.Summary)
	}
	if acc.Pending == nil || len(acc.Pending.Items) == 0 {
		t.Fatal("最后一日应展示待成交选股")
	}
}

func TestPaperAccountFromCombo(t *testing.T) {
	ts := newPaperTestServer(t)
	defer ts.Close()

	f1 := postFactor(t, ts, "rev5", `alpha = -1 * cs_rank(close / ts_delay(close, 5) - 1)`)
	f2 := postFactor(t, ts, "mom10", `alpha = cs_rank(close / ts_delay(close, 10) - 1)`)
	combo := postCombo(t, ts, "mix", []map[string]any{
		{"id": f1.ID, "weight": 1, "direction": 1},
		{"id": f2.ID, "weight": 1, "direction": 1},
	})

	acc := postPaper(t, ts, map[string]any{
		"name": "mix 实盘", "combo_id": combo.ID, "top_n": 5, "capital": 100000, "cost": 0.003,
	}, http.StatusOK)
	if acc.ComboID != combo.ID {
		t.Fatalf("combo_id: got %d want %d", acc.ComboID, combo.ID)
	}
	if acc.ComboName != "mix" {
		t.Fatalf("combo_name: got %q", acc.ComboName)
	}
	if acc.FactorID != 0 {
		t.Fatalf("组合账户不应绑定单因子, factor_id=%d", acc.FactorID)
	}
	if !strings.Contains(acc.Expr, "cs_zscore") {
		t.Fatalf("应冻结组合合成表达式, expr=%s", acc.Expr)
	}
	if len(acc.Days) < 10 {
		t.Fatalf("成交日数太少: %d", len(acc.Days))
	}
	if n := len(acc.Days[len(acc.Days)-1].Picks); n != 5 {
		t.Fatalf("每日应选出 Top 5，实际 %d", n)
	}
	if acc.Summary == nil || acc.Summary.NAV <= 0 {
		t.Fatalf("摘要净值异常: %+v", acc.Summary)
	}
}

func TestPaperCreateRejectsBothSources(t *testing.T) {
	ts := newPaperTestServer(t)
	defer ts.Close()
	f := postFactor(t, ts, "rev5", `alpha = -1 * cs_rank(close / ts_delay(close, 5) - 1)`)
	combo := postCombo(t, ts, "mix", []map[string]any{
		{"id": f.ID, "weight": 1, "direction": 1},
		{"id": postFactor(t, ts, "mom10", `alpha = cs_rank(close / ts_delay(close, 10) - 1)`).ID, "weight": 1, "direction": 1},
	})
	_ = postPaper(t, ts, map[string]any{
		"factor_id": f.ID, "combo_id": combo.ID, "top_n": 5, "capital": 100000,
	}, http.StatusBadRequest)
}

func TestPaperCreateComboMissing(t *testing.T) {
	ts := newPaperTestServer(t)
	defer ts.Close()
	b, _ := json.Marshal(map[string]any{"combo_id": 99, "top_n": 5, "capital": 100000})
	res, err := http.Post(ts.URL+"/api/paper", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("缺组合应 404，实际 %s", res.Status)
	}
	var raw map[string]any
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	msg, _ := raw["error"].(string)
	if !strings.Contains(msg, "组合") {
		t.Fatalf("错误应指向组合，实际 %q", msg)
	}
}

func newPaperTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	src := &data.SyntheticSource{Days: 40, Stocks: 20, Seed: 1}
	p, err := src.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(p, nil, "", "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(srv.Handler())
}

func postFactor(t *testing.T, ts *httptest.Server, name, expr string) Factor {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"name": name, "expr": expr})
	res, err := http.Post(ts.URL+"/api/factors", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusOK {
		t.Fatalf("创建因子失败: %s", res.Status)
	}
	var f Factor
	if err := json.NewDecoder(res.Body).Decode(&f); err != nil {
		t.Fatal(err)
	}
	return f
}

func postCombo(t *testing.T, ts *httptest.Server, name string, items []map[string]any) Combo {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"name": name, "method": "equal", "items": items})
	res, err := http.Post(ts.URL+"/api/combos", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		var raw map[string]any
		_ = json.NewDecoder(res.Body).Decode(&raw)
		t.Fatalf("创建组合失败 %s: %v", res.Status, raw)
	}
	var c Combo
	if err := json.NewDecoder(res.Body).Decode(&c); err != nil {
		t.Fatal(err)
	}
	return c
}

func postPaper(t *testing.T, ts *httptest.Server, body map[string]any, want int) paper.Account {
	t.Helper()
	b, _ := json.Marshal(body)
	res, err := http.Post(ts.URL+"/api/paper", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != want {
		var raw map[string]any
		_ = json.NewDecoder(res.Body).Decode(&raw)
		t.Fatalf("创建实盘状态 %s want %d: %v", res.Status, want, raw["error"])
	}
	if want != http.StatusOK {
		return paper.Account{}
	}
	var acc paper.Account
	if err := json.NewDecoder(res.Body).Decode(&acc); err != nil {
		t.Fatal(err)
	}
	return acc
}
