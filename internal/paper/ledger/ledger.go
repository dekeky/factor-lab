// Package ledger 模拟盘账本：持仓、成交、快照。Apply 只改内存，不写库。
package ledger

import (
	"fmt"
)

type Position struct {
	Code   string  `json:"code"`
	Name   string  `json:"name,omitempty"`
	Label  string  `json:"label,omitempty"`
	Shares float64 `json:"shares"`
	Cost   float64 `json:"cost"`
	Price  float64 `json:"-"`
	Value  float64 `json:"-"`
	Weight float64 `json:"-"`
	PnL    float64 `json:"-"`
}

type Fill struct {
	ID        int64   `json:"id,omitempty"`
	AccountID int     `json:"account_id"`
	PlanID    int64   `json:"plan_id,omitempty"`
	Date      string  `json:"date"`
	Code      string  `json:"code"`
	Side      string  `json:"side"`
	Shares    float64 `json:"shares"`
	Price     float64 `json:"price"`
	Amount    float64 `json:"amount"`
	Fee       float64 `json:"fee"`
	Slippage  float64 `json:"slippage"`
	Status    string  `json:"status"`
}

type Snapshot struct {
	AccountID int        `json:"account_id"`
	Date      string     `json:"date"`
	NAV       float64    `json:"nav"`
	Cash      float64    `json:"cash"`
	Ret       float64    `json:"ret"`
	Holdings  []Position `json:"holdings"`
}

type Book struct {
	AccountID   int        `json:"account_id"`
	Cash        float64    `json:"cash"`
	Positions   []Position `json:"positions"`
	LastSettled string     `json:"last_settled,omitempty"`
}

func Apply(book Book, fills []Fill) (Book, error) {
	next := Book{
		AccountID:   book.AccountID,
		Cash:        book.Cash,
		LastSettled: book.LastSettled,
		Positions:   clonePositions(book.Positions),
	}
	for _, f := range fills {
		if f.Status != "filled" || f.Shares == 0 {
			continue
		}
		switch f.Side {
		case "buy":
			if err := applyBuy(&next, f); err != nil {
				return Book{}, err
			}
		case "sell":
			if err := applySell(&next, f); err != nil {
				return Book{}, err
			}
		default:
			return Book{}, fmt.Errorf("未知成交方向 %q", f.Side)
		}
	}
	return next, nil
}

func applyBuy(book *Book, f Fill) error {
	book.Cash -= f.Amount + f.Fee
	i := indexOf(book.Positions, f.Code)
	if i < 0 {
		book.Positions = append(book.Positions, Position{
			Code: f.Code, Shares: f.Shares, Cost: (f.Amount + f.Fee) / f.Shares,
		})
		return nil
	}
	p := book.Positions[i]
	old := p.Shares
	p.Shares += f.Shares
	p.Cost = (old*p.Cost + f.Amount + f.Fee) / p.Shares
	book.Positions[i] = p
	return nil
}

func applySell(book *Book, f Fill) error {
	i := indexOf(book.Positions, f.Code)
	if i < 0 {
		return fmt.Errorf("卖出 %s 但账上没有持仓", f.Code)
	}
	p := book.Positions[i]
	if f.Shares-p.Shares > 1e-9 {
		return fmt.Errorf("卖出 %s %v 股超过持仓 %v", f.Code, f.Shares, p.Shares)
	}
	book.Cash += f.Amount - f.Fee
	p.Shares -= f.Shares
	if p.Shares < 1e-12 {
		book.Positions = append(book.Positions[:i], book.Positions[i+1:]...)
		return nil
	}
	book.Positions[i] = p
	return nil
}

func clonePositions(in []Position) []Position {
	if len(in) == 0 {
		return nil
	}
	out := make([]Position, len(in))
	copy(out, in)
	return out
}

func indexOf(ps []Position, code string) int {
	for i := range ps {
		if ps[i].Code == code {
			return i
		}
	}
	return -1
}
