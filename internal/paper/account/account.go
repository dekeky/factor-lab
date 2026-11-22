package account

import "time"

type Config struct {
	TopN               int     `json:"top_n"`
	RebalanceThreshold int     `json:"rebalance_threshold"`
	Capital            float64 `json:"capital"`
	Cost               float64 `json:"cost"`
	SlippageBps        float64 `json:"slippage_bps"`
	Start              string  `json:"start"`
}

func DefaultConfig() Config {
	return Config{TopN: 10, RebalanceThreshold: 20, Capital: 1_000_000, Cost: 0.003}
}

type Account struct {
	ID         int       `json:"id"`
	Name       string    `json:"name"`
	FactorID   int       `json:"factor_id,omitempty"`
	FactorName string    `json:"factor_name,omitempty"`
	ComboID    int       `json:"combo_id,omitempty"`
	ComboName  string    `json:"combo_name,omitempty"`
	Expr       string    `json:"expr"`
	Config     Config    `json:"config"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
