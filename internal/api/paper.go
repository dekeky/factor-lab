package api

import (
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"factor-lab/internal/compose"
	"factor-lab/internal/data"
	"factor-lab/internal/engine"
	"factor-lab/internal/panel"
	"factor-lab/internal/paper/account"
	"factor-lab/internal/paper/jobs"
	"factor-lab/internal/paper/ledger"
	"factor-lab/internal/paper/matching"
)

var hiddenPanelFields = data.HiddenFields

type paperReq struct {
	Name               string  `json:"name"`
	FactorID           int     `json:"factor_id"`
	ComboID            int     `json:"combo_id"`
	TopN               int     `json:"top_n"`
	Buffer             *int    `json:"buffer"`
	RebalanceThreshold *int    `json:"rebalance_threshold"`
	Capital            float64 `json:"capital"`
	Cost               float64 `json:"cost"`
	SlippageBps        float64 `json:"slippage_bps"`
	Start              string  `json:"start"`
}

type paperSource struct {
	expr, factorName, comboName, defaultName string
	factorID, comboID                        int
}

// Paper 页面只读拼装：账户、账本、计划、净值快照、成交。不存库。
type Paper struct {
	Account   account.Account   `json:"account"`
	Book      ledger.Book       `json:"book"`
	Plans     []matching.Plan   `json:"plans"`
	Snapshots []ledger.Snapshot `json:"snapshots"`
	Fills     []ledger.Fill     `json:"fills,omitempty"`
}

func (s *Server) handleListPaper(w http.ResponseWriter, r *http.Request) {
	if s.paperJobs == nil || s.paperJobs.Store == nil {
		writeErr(w, http.StatusServiceUnavailable, "模拟盘未初始化")
		return
	}
	accs, err := s.paperJobs.Store.ListAccounts()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]Paper, 0, len(accs))
	for _, acc := range accs {
		view, err := s.assemblePaper(acc.ID, false)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, view)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreatePaper(w http.ResponseWriter, r *http.Request) {
	if s.paperJobs == nil || s.paperJobs.Store == nil {
		writeErr(w, http.StatusServiceUnavailable, "模拟盘未初始化")
		return
	}
	var req paperReq
	if !readJSON(w, r, &req) {
		return
	}
	if req.FactorID > 0 && req.ComboID > 0 {
		writeErr(w, http.StatusBadRequest, "只能指定因子或组合之一")
		return
	}
	src, code, msg := s.resolvePaperSource(req.FactorID, req.ComboID)
	if msg != "" {
		writeErr(w, code, msg)
		return
	}
	if errs := engine.Check(src.expr, s.panel()); len(errs) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"errors": errs})
		return
	}
	cfg := account.DefaultConfig()
	if req.TopN > 0 {
		cfg.TopN = req.TopN
	}
	if req.RebalanceThreshold != nil {
		cfg.RebalanceThreshold = *req.RebalanceThreshold
	} else if req.Buffer != nil {
		cfg.RebalanceThreshold = *req.Buffer
	}
	if req.Capital > 0 {
		cfg.Capital = req.Capital
	}
	if req.Cost > 0 {
		cfg.Cost = req.Cost
	}
	if req.SlippageBps > 0 {
		cfg.SlippageBps = req.SlippageBps
	}
	cfg.Start = strings.TrimSpace(req.Start)
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = src.defaultName
	}
	name, err := s.uniquePaperName(name)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now()
	opened, err := jobs.OpenAccount(r.Context(), s.paperJobs, account.Account{
		Name: name, FactorID: src.factorID, FactorName: src.factorName,
		ComboID: src.comboID, ComboName: src.comboName, Expr: src.expr,
		Config: cfg, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	view, err := s.assemblePaper(opened.ID, true)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleGetPaper(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if s.paperJobs == nil || s.paperJobs.Store == nil {
		writeErr(w, http.StatusServiceUnavailable, "模拟盘未初始化")
		return
	}
	view, err := s.assemblePaper(id, true)
	if err != nil {
		writeErr(w, http.StatusNotFound, "模拟盘不存在")
		return
	}
	s.backfillPlanPicks(&view)
	s.decoratePaper(&view)
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleDeletePaper(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if s.paperJobs == nil || s.paperJobs.Store == nil {
		writeErr(w, http.StatusServiceUnavailable, "模拟盘未初始化")
		return
	}
	if _, err := s.paperJobs.Store.GetAccount(id); err != nil {
		writeErr(w, http.StatusNotFound, "模拟盘不存在")
		return
	}
	if err := s.paperJobs.Store.DeleteAccount(id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) handleSyncPaper(w http.ResponseWriter, r *http.Request) {
	if _, ok := pathID(w, r); !ok {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSyncAllPaper(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) assemblePaper(id int, withFills bool) (Paper, error) {
	acc, err := s.paperJobs.Store.GetAccount(id)
	if err != nil {
		return Paper{}, err
	}
	book, err := s.paperJobs.Store.LatestBook(id)
	if err != nil {
		return Paper{}, err
	}
	if book.LastSettled == "" && book.Cash == 0 && len(book.Positions) == 0 {
		book.Cash = acc.Config.Capital
	}
	book.AccountID = acc.ID
	plans, err := s.paperJobs.Store.ListPlans(id)
	if err != nil {
		return Paper{}, err
	}
	snaps, err := s.paperJobs.Store.ListSnapshots(id)
	if err != nil {
		return Paper{}, err
	}
	if plans == nil {
		plans = []matching.Plan{}
	}
	if snaps == nil {
		snaps = []ledger.Snapshot{}
	}
	view := Paper{Account: acc, Book: book, Plans: plans, Snapshots: snaps}
	if withFills {
		fills, err := s.paperJobs.Store.ListFills(id)
		if err != nil {
			return Paper{}, err
		}
		if fills == nil {
			fills = []ledger.Fill{}
		}
		view.Fills = fills
	}
	s.decoratePaper(&view)
	return view, nil
}

func (s *Server) decoratePaper(view *Paper) {
	p := s.panel()
	if view == nil || p == nil {
		return
	}
	idx := make(map[string]int, p.N())
	for j, c := range p.Codes {
		idx[c] = j
	}
	dateIdx := make(map[string]int, p.T())
	for i, d := range p.Dates {
		dateIdx[d.Format("2006-01-02")] = i
	}
	meta := func(code string) (string, string) {
		j, ok := idx[code]
		if !ok {
			return "", code
		}
		return p.StockName(j), p.Label(j)
	}
	mark := func(date string, ps []ledger.Position, cash float64) {
		di, ok := dateIdx[date]
		nav := cash
		for i := range ps {
			ps[i].Name, ps[i].Label = meta(ps[i].Code)
			j, has := idx[ps[i].Code]
			if ok && has {
				if px := panelClose(p, di, j); px > 0 {
					ps[i].Price = px
					ps[i].Value = ps[i].Shares * px
					ps[i].PnL = (px - ps[i].Cost) * ps[i].Shares
				}
			}
			nav += ps[i].Value
		}
		if nav > 0 {
			for i := range ps {
				ps[i].Weight = ps[i].Value / nav
			}
		}
	}
	mark(view.Book.LastSettled, view.Book.Positions, view.Book.Cash)
	for i := range view.Snapshots {
		mark(view.Snapshots[i].Date, view.Snapshots[i].Holdings, view.Snapshots[i].Cash)
	}
	for i := range view.Plans {
		for j := range view.Plans[i].Intents {
			view.Plans[i].Intents[j].Name, view.Plans[i].Intents[j].Label = meta(view.Plans[i].Intents[j].Code)
		}
		for j := range view.Plans[i].Picks {
			view.Plans[i].Picks[j].Name, view.Plans[i].Picks[j].Label = meta(view.Plans[i].Picks[j].Code)
		}
	}
	for i := range view.Fills {
		view.Fills[i].Name, view.Fills[i].Label = meta(view.Fills[i].Code)
	}
}

func (s *Server) backfillPlanPicks(view *Paper) {
	if view == nil || s.paperJobs == nil || s.paperJobs.Store == nil {
		return
	}
	need := false
	for _, p := range view.Plans {
		if len(p.Picks) == 0 {
			need = true
			break
		}
	}
	if !need {
		return
	}
	pan := s.panel()
	if pan == nil || pan.T() < 1 {
		return
	}
	col, errs := engine.Run(view.Account.Expr, pan)
	if len(errs) > 0 || col == nil {
		return
	}
	topN := view.Account.Config.TopN
	if topN < 1 {
		topN = 10
	}
	for i := range view.Plans {
		if len(view.Plans[i].Picks) > 0 {
			continue
		}
		picks, err := matching.PicksOnDate(pan, col, view.Plans[i].SignalDate, topN)
		if err != nil || len(picks) == 0 {
			continue
		}
		if err := s.paperJobs.Store.UpdatePlanPicks(view.Plans[i].ID, picks); err != nil {
			continue
		}
		view.Plans[i].Picks = picks
	}
}

func panelClose(p *panel.Panel, i, j int) float64 {
	n := p.N()
	for _, name := range []string{"qfq_close", "close"} {
		c, ok := p.Col(name)
		if !ok {
			continue
		}
		v := c[i*n+j]
		if !math.IsNaN(v) && !math.IsInf(v, 0) && v > 0 {
			return v
		}
	}
	return 0
}

func (s *Server) uniquePaperName(name string) (string, error) {
	accs, err := s.paperJobs.Store.ListAccounts()
	if err != nil {
		return "", err
	}
	taken := make(map[string]bool, len(accs))
	for _, a := range accs {
		taken[a.Name] = true
	}
	if !taken[name] {
		return name, nil
	}
	base := name
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s (%d)", base, i)
		if !taken[cand] {
			return cand, nil
		}
	}
}

func (s *Server) resolvePaperSource(factorID, comboID int) (paperSource, int, string) {
	if factorID <= 0 && comboID <= 0 {
		return paperSource{}, http.StatusBadRequest, "请指定因子或组合"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if comboID > 0 {
		i := s.findComboLocked(comboID)
		if i < 0 {
			return paperSource{}, http.StatusNotFound, "组合不存在"
		}
		c := s.combos[i]
		parts, missing := s.comboPartsLocked(c)
		if len(missing) > 0 {
			return paperSource{}, http.StatusUnprocessableEntity, "组合内因子已删除，请先编辑组合：" + strings.Join(missing, "、")
		}
		expr, err := compose.Build(parts, c.Method)
		if err != nil {
			return paperSource{}, http.StatusUnprocessableEntity, err.Error()
		}
		return paperSource{
			expr: expr, factorName: c.Name, comboID: c.ID, comboName: c.Name,
			defaultName: c.Name + " 模拟盘",
		}, 0, ""
	}
	f := s.factorByIDLocked(factorID)
	if f == nil {
		return paperSource{}, http.StatusNotFound, "因子不存在"
	}
	return paperSource{
		expr: f.Expr, factorID: f.ID, factorName: f.Name, defaultName: f.Name + " 模拟盘",
	}, 0, ""
}
