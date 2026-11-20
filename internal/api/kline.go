package api

import (
	"math"
	"net/http"
	"strings"

	"factor-lab/internal/panel"
)

type kBar struct {
	Time   string  `json:"time"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

type klineResp struct {
	Code  string `json:"code"`
	Name  string `json:"name,omitempty"`
	Label string `json:"label"`
	Bars  []kBar `json:"bars"`
}

func (s *Server) handleKLine(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		writeErr(w, http.StatusBadRequest, "缺少 code")
		return
	}
	p := s.panel()
	if p == nil {
		writeErr(w, http.StatusServiceUnavailable, "数据面板未就绪")
		return
	}
	out, ok := stockKLine(p, code)
	if !ok {
		writeErr(w, http.StatusNotFound, "股票不在当前股票池："+code)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func stockKLine(p *panel.Panel, code string) (klineResp, bool) {
	j := -1
	for i, c := range p.Codes {
		if c == code {
			j = i
			break
		}
	}
	if j < 0 {
		return klineResp{}, false
	}
	N := p.N()
	dates := p.DateLabels()
	openC := p.MustCol("open")
	highC := p.MustCol("high")
	lowC := p.MustCol("low")
	closeC := p.MustCol("close")
	volC := p.MustCol("vol")
	qfqOpen, hasQfqOpen := p.Col("qfq_open")
	qfqClose, hasQfqClose := p.Col("qfq_close")

	bars := make([]kBar, 0, p.T())
	for i := 0; i < p.T(); i++ {
		idx := i*N + j
		o, h, l, c := openC[idx], highC[idx], lowC[idx], closeC[idx]
		if !pos(o) || !pos(h) || !pos(l) || !pos(c) {
			continue
		}
		qc := c
		if hasQfqClose && pos(qfqClose[idx]) {
			qc = qfqClose[idx]
		}
		scale := qc / c
		qo := o * scale
		if hasQfqOpen && pos(qfqOpen[idx]) {
			qo = qfqOpen[idx]
		}
		qh, ql := h*scale, l*scale
		hi := math.Max(math.Max(qo, qc), math.Max(qh, ql))
		lo := math.Min(math.Min(qo, qc), math.Min(qh, ql))
		vol := volC[idx]
		if math.IsNaN(vol) || vol < 0 {
			vol = 0
		}
		bars = append(bars, kBar{
			Time: dates[i], Open: qo, High: hi, Low: lo, Close: qc, Volume: vol,
		})
	}
	return klineResp{Code: code, Name: p.StockName(j), Label: p.Label(j), Bars: bars}, true
}

func pos(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v > 0
}
