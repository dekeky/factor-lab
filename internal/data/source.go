// Package data 提供 Panel 数据源：Tushare（生产）与 CSV。
package data

import (
	"context"

	"factor-lab/internal/panel"
)

// Source 是面板数据源。
type Source interface {
	Name() string
	Load(ctx context.Context) (*panel.Panel, error)
}

// Reporter 可选实现：返回数据质量摘要（加载后可用）。
type Reporter interface {
	Report() *LoadReport
}

// LoadReport 是一次加载的数据质量摘要。
type LoadReport struct {
	Index        string   `json:"index"`
	Codes        int      `json:"codes"`
	Dates        int      `json:"dates"`
	From         string   `json:"from"`
	To           string   `json:"to"`
	Bars         int      `json:"bars"`
	STRows       int      `json:"st_rows"`
	STDropped    int      `json:"st_dropped"`
	ROERows      int      `json:"roe_rows"`
	HasROE       bool     `json:"has_roe"`
	OptionalErrs []string `json:"optional_errors,omitempty"`
}
