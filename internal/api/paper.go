package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	"factor-lab/internal/compose"
	"factor-lab/internal/engine"
	"factor-lab/internal/paper"
)

// 面板里给实盘用的可交易列，不进因子 DSL 字段表。
var hiddenPanelFields = map[string]bool{
	"raw_close": true, "raw_open": true, "qfq_open": true, "qfq_close": true, "suspend": true, "up_limit": true, "down_limit": true,
}

type paperReq struct {
	Name     string  `json:"name"`
	FactorID int     `json:"factor_id"`
	ComboID  int     `json:"combo_id"`
	TopN     int     `json:"top_n"`
	Buffer   int     `json:"buffer"`
	Capital  float64 `json:"capital"`
	Cost     float64 `json:"cost"`
	Start    string  `json:"start"`
}

type paperSource struct {
	expr, factorName, comboName, defaultName string
	factorID, comboID                        int
}

func (s *Server) persistPapers() {
	if s.paperStore == "" {
		return
	}
	b, err := json.MarshalIndent(s.papers, "", "  ")
	if err != nil {
		b, err = json.MarshalIndent(toJSONAny(reflect.ValueOf(s.papers)), "", "  ")
		if err != nil {
			return
		}
	}
	writeAtomic(s.paperStore, b)
}

func (s *Server) handleListPaper(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	out := make([]paper.Account, len(s.papers))
	for i, a := range s.papers {
		out[i] = a.Brief()
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreatePaper(w http.ResponseWriter, r *http.Request) {
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
	cfg := paper.DefaultConfig()
	if req.TopN > 0 {
		cfg.TopN = req.TopN
	}
	if req.Buffer > 0 {
		cfg.Buffer = req.Buffer
	}
	if req.Capital > 0 {
		cfg.Capital = req.Capital
	}
	if req.Cost > 0 {
		cfg.Cost = req.Cost
	}
	cfg.Start = strings.TrimSpace(req.Start)
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = src.defaultName
	}

	s.mu.Lock()
	taken := make(map[string]bool, len(s.papers))
	for _, a := range s.papers {
		taken[a.Name] = true
	}
	if taken[name] {
		base := name
		for i := 2; ; i++ {
			cand := fmt.Sprintf("%s (%d)", base, i)
			if !taken[cand] {
				name = cand
				break
			}
		}
	}
	now := time.Now()
	acc := paper.Account{
		ID: s.nextPaperID, Name: name,
		FactorID: src.factorID, FactorName: src.factorName,
		ComboID: src.comboID, ComboName: src.comboName,
		Expr: src.expr, Config: cfg, CreatedAt: now, UpdatedAt: now,
	}
	s.nextPaperID++
	s.mu.Unlock()

	if err := s.runPaper(&acc); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	s.mu.Lock()
	s.papers = append(s.papers, acc)
	s.persistPapers()
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, acc)
}

func (s *Server) handleGetPaper(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	i := s.findPaperLocked(id)
	if i < 0 {
		s.mu.Unlock()
		writeErr(w, http.StatusNotFound, "模拟盘不存在")
		return
	}
	acc := s.papers[i]
	s.mu.Unlock()

	if err := s.runPaper(&acc); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	s.mu.Lock()
	if j := s.findPaperLocked(id); j >= 0 {
		s.papers[j] = acc
		s.persistPapers()
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, acc)
}

func (s *Server) handleDeletePaper(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.findPaperLocked(id)
	if i < 0 {
		writeErr(w, http.StatusNotFound, "模拟盘不存在")
		return
	}
	s.papers = append(s.papers[:i], s.papers[i+1:]...)
	s.persistPapers()
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) handleSyncPaper(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	i := s.findPaperLocked(id)
	if i < 0 {
		s.mu.Unlock()
		writeErr(w, http.StatusNotFound, "模拟盘不存在")
		return
	}
	acc := s.papers[i]
	s.mu.Unlock()
	if err := s.runPaper(&acc); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	s.mu.Lock()
	if j := s.findPaperLocked(id); j >= 0 {
		s.papers[j] = acc
		s.persistPapers()
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, acc)
}

func (s *Server) handleSyncAllPaper(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	ids := make([]int, len(s.papers))
	for i, a := range s.papers {
		ids[i] = a.ID
	}
	s.mu.Unlock()
	n, failed := 0, 0
	for _, id := range ids {
		s.mu.Lock()
		i := s.findPaperLocked(id)
		if i < 0 {
			s.mu.Unlock()
			continue
		}
		acc := s.papers[i]
		s.mu.Unlock()
		if err := s.runPaper(&acc); err != nil {
			failed++
			continue
		}
		s.mu.Lock()
		if j := s.findPaperLocked(id); j >= 0 {
			s.papers[j] = acc
		}
		s.mu.Unlock()
		n++
	}
	s.mu.Lock()
	s.persistPapers()
	briefs := make([]paper.Account, len(s.papers))
	for i, a := range s.papers {
		briefs[i] = a.Brief()
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"synced": n, "failed": failed, "accounts": briefs})
}

func (s *Server) runPaper(acc *paper.Account) error {
	p := s.panel()
	if p == nil || p.T() == 0 {
		return fmt.Errorf("尚未加载行情")
	}
	col, errs := engine.Run(acc.Expr, p)
	if len(errs) > 0 {
		return fmt.Errorf("因子无法计算：%s", strings.Join(errsToStrings(errs), "; "))
	}
	return paper.Replay(p, col, acc)
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

func (s *Server) findPaperLocked(id int) int {
	for i := range s.papers {
		if s.papers[i].ID == id {
			return i
		}
	}
	return -1
}
