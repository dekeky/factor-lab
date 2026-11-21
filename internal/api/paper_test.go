package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"factor-lab/internal/data"
	"factor-lab/internal/paper/jobs"

	"github.com/riverqueue/river"
)

func TestCreatePaperEnqueuesGenerateNotReplay(t *testing.T) {
	srv, ts := newPaperFixture(t)
	f := postFactor(t, ts, "rev5", `alpha = -1 * cs_rank(close / ts_delay(close, 5) - 1)`)
	view := postPaper(t, ts, map[string]any{
		"name": "rev5 实盘", "factor_id": f.ID, "top_n": 5, "capital": 100000, "cost": 0.003,
	}, http.StatusOK)
	if view.Account.ID < 1 || view.Account.Expr == "" {
		t.Fatalf("account %+v", view.Account)
	}
	if len(view.Fills) != 0 {
		t.Fatalf("create must not write fills: %d", len(view.Fills))
	}
	if len(view.Plans) != 0 {
		t.Fatalf("create must not write plans: %d", len(view.Plans))
	}
	if len(view.Snapshots) != 1 {
		t.Fatalf("init snapshots %d", len(view.Snapshots))
	}
	res, err := srv.paperJobs.Client.JobList(context.Background(), river.NewJobListParams().Kinds(jobs.GenerateArgs{}.Kind()))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Jobs) != 1 {
		t.Fatalf("generate jobs %d", len(res.Jobs))
	}
	var args jobs.GenerateArgs
	if err := json.Unmarshal(res.Jobs[0].EncodedArgs, &args); err != nil {
		t.Fatal(err)
	}
	if args.AccountID != view.Account.ID || args.SignalDate != view.Account.Config.Start {
		t.Fatalf("args %+v start %s", args, view.Account.Config.Start)
	}
}

func TestGetPaperDoesNotWriteFills(t *testing.T) {
	_, ts := newPaperFixture(t)
	f := postFactor(t, ts, "rev5", `alpha = -1 * cs_rank(close / ts_delay(close, 5) - 1)`)
	created := postPaper(t, ts, map[string]any{
		"name": "rev5 实盘", "factor_id": f.ID, "top_n": 5, "capital": 100000,
	}, http.StatusOK)
	res, err := http.Get(ts.URL + "/api/paper/" + strconv.Itoa(created.Account.ID))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s", res.Status)
	}
	var got Paper
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Fills) != 0 {
		t.Fatalf("GET wrote fills: %d", len(got.Fills))
	}
	res2, err := http.Get(ts.URL + "/api/paper/" + strconv.Itoa(created.Account.ID))
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	var again Paper
	if err := json.NewDecoder(res2.Body).Decode(&again); err != nil {
		t.Fatal(err)
	}
	if len(again.Fills) != len(got.Fills) || len(again.Snapshots) != len(got.Snapshots) {
		t.Fatal("GET must be read-only")
	}
}

func TestCreatePaperRollbackOnBadStart(t *testing.T) {
	srv, ts := newPaperFixture(t)
	f := postFactor(t, ts, "rev5", `alpha = -1 * cs_rank(close / ts_delay(close, 5) - 1)`)
	_ = postPaper(t, ts, map[string]any{
		"factor_id": f.ID, "top_n": 5, "capital": 100000, "start": "2099-01-01",
	}, http.StatusUnprocessableEntity)
	accs, err := srv.paperJobs.Store.ListAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(accs) != 0 {
		t.Fatalf("rolled back accounts %d", len(accs))
	}
	res, err := srv.paperJobs.Client.JobList(context.Background(), river.NewJobListParams().Kinds(jobs.GenerateArgs{}.Kind()))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Jobs) != 0 {
		t.Fatalf("generate jobs %d", len(res.Jobs))
	}
}

func TestPaperAccountFromCombo(t *testing.T) {
	_, ts := newPaperFixture(t)
	f1 := postFactor(t, ts, "rev5", `alpha = -1 * cs_rank(close / ts_delay(close, 5) - 1)`)
	f2 := postFactor(t, ts, "mom10", `alpha = cs_rank(close / ts_delay(close, 10) - 1)`)
	combo := postCombo(t, ts, "mix", []map[string]any{
		{"id": f1.ID, "weight": 1, "direction": 1},
		{"id": f2.ID, "weight": 1, "direction": 1},
	})
	view := postPaper(t, ts, map[string]any{
		"name": "mix 实盘", "combo_id": combo.ID, "top_n": 5, "capital": 100000, "cost": 0.003,
	}, http.StatusOK)
	if view.Account.ComboID != combo.ID {
		t.Fatalf("combo_id: got %d want %d", view.Account.ComboID, combo.ID)
	}
	if view.Account.ComboName != "mix" {
		t.Fatalf("combo_name: got %q", view.Account.ComboName)
	}
	if view.Account.FactorID != 0 {
		t.Fatalf("组合账户不应绑定单因子, factor_id=%d", view.Account.FactorID)
	}
	if !strings.Contains(view.Account.Expr, "cs_zscore") {
		t.Fatalf("应冻结组合合成表达式, expr=%s", view.Account.Expr)
	}
}

func TestPaperCreateRejectsBothSources(t *testing.T) {
	_, ts := newPaperFixture(t)
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
	_, ts := newPaperFixture(t)
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

func newPaperFixture(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	src := &data.SyntheticSource{Days: 40, Stocks: 20, Seed: 1}
	p, err := src.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(p, nil, "", "", Options{PaperDB: filepath.Join(t.TempDir(), "paper.db")})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		_ = srv.Close()
	})
	return srv, ts
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

func postPaper(t *testing.T, ts *httptest.Server, body map[string]any, want int) Paper {
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
		return Paper{}
	}
	var view Paper
	if err := json.NewDecoder(res.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	return view
}
