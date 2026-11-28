package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"factor-lab/internal/data"
	"factor-lab/internal/panel"
)

func TestHealthPanelLoadingThenReady(t *testing.T) {
	empty := panel.New(nil, nil)
	srv, err := New(empty, nil, "", "", Options{DataSource: "tushare:000300.SH"})
	if err != nil {
		t.Fatal(err)
	}
	srv.BeginPanelLoad()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	got := getHealth(t, ts.URL)
	if !got.Loading {
		t.Fatal("startup health should report loading=true")
	}
	if got.Panel.Dates != 0 {
		t.Fatalf("empty panel dates = %d", got.Panel.Dates)
	}

	src := &data.SyntheticSource{Days: 8, Stocks: 4, Seed: 1}
	p, err := src.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	srv.FinishPanelLoad(p, nil, nil)

	got = getHealth(t, ts.URL)
	if got.Loading {
		t.Fatal("after load health should report loading=false")
	}
	if got.Panel.Dates != 8 || got.Panel.Stocks != 4 {
		t.Fatalf("loaded panel %+v", got.Panel)
	}
}

func TestFinishPanelLoadErrorKeepsEmptyPanel(t *testing.T) {
	empty := panel.New(nil, nil)
	srv, err := New(empty, nil, "", "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	srv.BeginPanelLoad()
	srv.FinishPanelLoad(nil, nil, errSentinel{})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	got := getHealth(t, ts.URL)
	if got.Loading {
		t.Fatal("failed load should not stay loading")
	}
	if got.LoadError != "load failed" {
		t.Fatalf("load_error = %q", got.LoadError)
	}
	if got.Panel.Dates != 0 {
		t.Fatal("failed load must not replace the empty panel")
	}
}

type errSentinel struct{}

func (errSentinel) Error() string { return "load failed" }

func getHealth(t *testing.T, base string) struct {
	Loading   bool   `json:"loading"`
	LoadError string `json:"load_error"`
	Panel     struct {
		Dates  int `json:"dates"`
		Stocks int `json:"stocks"`
	} `json:"panel"`
} {
	t.Helper()
	res, err := http.Get(base + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body struct {
		Loading   bool   `json:"loading"`
		LoadError string `json:"load_error"`
		Panel     struct {
			Dates  int `json:"dates"`
			Stocks int `json:"stocks"`
		} `json:"panel"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}
