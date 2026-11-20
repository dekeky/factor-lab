// Package panel 提供因子计算的列式宽表面板。
// 面板 = 交易日 × 股票 的网格；每个字段是一列，按日期主序展平存储。
package panel

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// Col 是一列因子/字段值，长度恒为 T*N，下标 i*N+j（i=日期序号，j=股票序号）。
// 缺失值用 NaN 表示；所有算子的空值传播规则以 NaN 为准。
type Col []float64

// Panel 是因子计算的数据底座。
type Panel struct {
	Dates []time.Time // T 个交易日，升序
	Codes []string    // N 个股票代码
	Names []string    // 与 Codes 对齐的证券简称；可空
	Cols  map[string]Col

	n int
}

// New 创建空列面板。
func New(dates []time.Time, codes []string) *Panel {
	return &Panel{
		Dates: dates,
		Codes: codes,
		Cols:  make(map[string]Col),
		n:     len(codes),
	}
}

func (p *Panel) T() int    { return len(p.Dates) }
func (p *Panel) N() int    { return len(p.Codes) }
func (p *Panel) Size() int { return p.T() * p.N() }

// SetNames 写入与 Codes 对齐的证券简称。
func (p *Panel) SetNames(names []string) {
	if p == nil || len(names) != p.N() {
		return
	}
	p.Names = append([]string(nil), names...)
}

// Label 返回「名称（代码）」；没有简称时只返回代码。
func (p *Panel) Label(j int) string {
	if p == nil || j < 0 || j >= p.N() {
		return ""
	}
	code := p.Codes[j]
	if j < len(p.Names) && p.Names[j] != "" && p.Names[j] != code {
		return p.Names[j] + "（" + code + "）"
	}
	return code
}

func (p *Panel) StockName(j int) string {
	if p == nil || j < 0 || j >= len(p.Names) {
		return ""
	}
	return p.Names[j]
}

// Idx 返回 (日期 i, 股票 j) 在列中的下标。
func (p *Panel) Idx(i, j int) int { return i*p.n + j }

// SetCol 写入（或覆盖）一列，长度必须匹配面板大小。
func (p *Panel) SetCol(name string, col Col) error {
	if len(col) != p.Size() {
		return fmt.Errorf("panel: 列 %s 长度 %d != 面板大小 %d", name, len(col), p.Size())
	}
	p.Cols[name] = col
	return nil
}

// Col 取一列。
func (p *Panel) Col(name string) (Col, bool) {
	c, ok := p.Cols[name]
	return c, ok
}

// MustCol 取一列，不存在时返回 NaN 列（字段缺失按全 NaN 处理，校验层会先行报错）。
func (p *Panel) MustCol(name string) Col {
	c, ok := p.Cols[name]
	if !ok {
		c = make(Col, p.Size())
		for i := range c {
			c[i] = nan()
		}
	}
	return c
}

// Fields 返回可用字段名（排序后）。
func (p *Panel) Fields() []string {
	out := make([]string, 0, len(p.Cols))
	for k := range p.Cols {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// DateLabels 返回 "2006-01-02" 格式的日期标签。
func (p *Panel) DateLabels() []string {
	out := make([]string, p.T())
	for i, d := range p.Dates {
		out[i] = d.Format("2006-01-02")
	}
	return out
}

// Slice 按日期区间 [from, to] 裁剪面板（闭区间，from/to 为零值时表示不限制）。
// 用于评价区间参数：截取后所有算子只在该窗口内计算，样本外/样本内切换靠它实现。
func (p *Panel) Slice(from, to time.Time) (*Panel, error) {
	i0, i1 := 0, p.T()-1
	if !from.IsZero() {
		i0 = sort.Search(len(p.Dates), func(i int) bool { return !p.Dates[i].Before(from) })
	}
	if !to.IsZero() {
		// 找到最后一个不晚于 to 的日期
		i1 = sort.Search(len(p.Dates), func(i int) bool { return p.Dates[i].After(to) }) - 1
	}
	if i0 >= len(p.Dates) || i1 < i0 {
		return nil, fmt.Errorf("panel: 裁剪区间为空（可选日期 %s ~ %s）",
			p.Dates[0].Format("2006-01-02"), p.Dates[len(p.Dates)-1].Format("2006-01-02"))
	}
	dates := p.Dates[i0 : i1+1]
	q := New(dates, p.Codes)
	q.SetNames(p.Names)
	for name, col := range p.Cols {
		sub := make(Col, len(dates)*p.n)
		copy(sub, col[i0*p.n:(i1+1)*p.n])
		q.Cols[name] = sub
	}
	return q, nil
}

// Range 返回面板起止日期（"2006-01-02"）。空面板返回空字符串。
func (p *Panel) Range() (string, string) {
	if p == nil || p.T() == 0 {
		return "", ""
	}
	return p.Dates[0].Format("2006-01-02"), p.Dates[p.T()-1].Format("2006-01-02")
}

func nan() float64 { return math.NaN() }
