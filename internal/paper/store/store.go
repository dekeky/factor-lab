package store

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"factor-lab/internal/paper/account"
	"factor-lab/internal/paper/ledger"
	"factor-lab/internal/paper/matching"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	dsn := path
	if filepath.IsAbs(path) || (len(path) > 0 && path[0] != ':') {
		dsn = "file:" + filepath.ToSlash(path) + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate paper schema: %w", err)
	}
	if _, err := db.Exec(`ALTER TABLE plan ADD COLUMN picks TEXT NOT NULL DEFAULT '[]'`); err != nil {
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "duplicate column") {
			db.Close()
			return nil, fmt.Errorf("migrate paper picks: %w", err)
		}
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) DB() *sql.DB { return s.db }

type queryer interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
	Prepare(query string) (*sql.Stmt, error)
}

func (s *Store) q(tx *sql.Tx) queryer {
	if tx != nil {
		return tx
	}
	return s.db
}

func IsConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "constraint failed")
}

func (s *Store) InsertAccount(acc account.Account) (int, error) {
	return s.InsertAccountTx(nil, acc)
}

func (s *Store) InsertAccountTx(tx *sql.Tx, acc account.Account) (int, error) {
	res, err := s.q(tx).Exec(`INSERT INTO account (
		name, factor_id, factor_name, combo_id, combo_name, expr,
		top_n, rebalance_threshold, capital, cost, slippage_bps, start, created_at, updated_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		acc.Name, acc.FactorID, acc.FactorName, acc.ComboID, acc.ComboName, acc.Expr,
		acc.Config.TopN, acc.Config.RebalanceThreshold, acc.Config.Capital, acc.Config.Cost,
		acc.Config.SlippageBps, acc.Config.Start, fmtTime(acc.CreatedAt), fmtTime(acc.UpdatedAt),
	)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return int(id), err
}

func (s *Store) GetAccount(id int) (account.Account, error) {
	var acc account.Account
	var created, updated string
	err := s.db.QueryRow(`SELECT id, name, factor_id, factor_name, combo_id, combo_name, expr,
		top_n, rebalance_threshold, capital, cost, slippage_bps, start, created_at, updated_at
		FROM account WHERE id=?`, id).Scan(
		&acc.ID, &acc.Name, &acc.FactorID, &acc.FactorName, &acc.ComboID, &acc.ComboName, &acc.Expr,
		&acc.Config.TopN, &acc.Config.RebalanceThreshold, &acc.Config.Capital, &acc.Config.Cost,
		&acc.Config.SlippageBps, &acc.Config.Start, &created, &updated,
	)
	if err != nil {
		return account.Account{}, err
	}
	acc.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	acc.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return acc, nil
}

func (s *Store) ListAccounts() ([]account.Account, error) {
	rows, err := s.db.Query(`SELECT id, name, factor_id, factor_name, combo_id, combo_name, expr,
		top_n, rebalance_threshold, capital, cost, slippage_bps, start, created_at, updated_at
		FROM account ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []account.Account
	for rows.Next() {
		var acc account.Account
		var created, updated string
		if err := rows.Scan(&acc.ID, &acc.Name, &acc.FactorID, &acc.FactorName, &acc.ComboID, &acc.ComboName, &acc.Expr,
			&acc.Config.TopN, &acc.Config.RebalanceThreshold, &acc.Config.Capital, &acc.Config.Cost,
			&acc.Config.SlippageBps, &acc.Config.Start, &created, &updated); err != nil {
			return nil, err
		}
		acc.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		acc.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		out = append(out, acc)
	}
	return out, rows.Err()
}

func (s *Store) InsertSnapshot(snap ledger.Snapshot) error {
	return s.InsertSnapshotTx(nil, snap)
}

func (s *Store) InsertSnapshotTx(tx *sql.Tx, snap ledger.Snapshot) error {
	hold, err := json.Marshal(snap.Holdings)
	if err != nil {
		return err
	}
	if hold == nil {
		hold = []byte("[]")
	}
	_, err = s.q(tx).Exec(`INSERT INTO snapshot (account_id, date, cash, nav, ret, holdings) VALUES (?,?,?,?,?,?)`,
		snap.AccountID, snap.Date, snap.Cash, snap.NAV, snap.Ret, string(hold))
	return err
}

func (s *Store) LatestBook(accountID int) (ledger.Book, error) {
	var date, holdings string
	var cash, nav, ret float64
	err := s.db.QueryRow(`SELECT date, cash, nav, ret, holdings FROM snapshot WHERE account_id=? ORDER BY date DESC LIMIT 1`, accountID).
		Scan(&date, &cash, &nav, &ret, &holdings)
	if err == sql.ErrNoRows {
		return ledger.Book{AccountID: accountID}, nil
	}
	if err != nil {
		return ledger.Book{}, err
	}
	var pos []ledger.Position
	if holdings != "" {
		_ = json.Unmarshal([]byte(holdings), &pos)
	}
	return ledger.Book{AccountID: accountID, Cash: cash, Positions: pos, LastSettled: date}, nil
}

func (s *Store) ListSnapshots(accountID int) ([]ledger.Snapshot, error) {
	rows, err := s.db.Query(`SELECT account_id, date, cash, nav, ret, holdings FROM snapshot WHERE account_id=? ORDER BY date`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ledger.Snapshot
	for rows.Next() {
		var snap ledger.Snapshot
		var holdings string
		if err := rows.Scan(&snap.AccountID, &snap.Date, &snap.Cash, &snap.NAV, &snap.Ret, &holdings); err != nil {
			return nil, err
		}
		if holdings != "" {
			_ = json.Unmarshal([]byte(holdings), &snap.Holdings)
		}
		out = append(out, snap)
	}
	return out, rows.Err()
}

func (s *Store) InsertPlan(p matching.Plan) (int64, error) {
	return s.InsertPlanTx(nil, p)
}

func marshalJSONList(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return "[]", nil
	}
	return string(raw), nil
}

func (s *Store) InsertPlanTx(tx *sql.Tx, p matching.Plan) (int64, error) {
	intents, err := marshalJSONList(p.Intents)
	if err != nil {
		return 0, err
	}
	picks, err := marshalJSONList(p.Picks)
	if err != nil {
		return 0, err
	}
	status := p.Status
	if status == "" {
		status = "pending"
	}
	res, err := s.q(tx).Exec(`INSERT INTO plan (account_id, signal_date, exec_at, intents, picks, status) VALUES (?,?,?,?,?,?)`,
		p.AccountID, p.SignalDate, p.ExecAt, intents, picks, status)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func scanPlan(id, accountID int64, signalDate, execAt, intents, picks, status string) matching.Plan {
	p := matching.Plan{ID: id, AccountID: int(accountID), SignalDate: signalDate, ExecAt: execAt, Status: status}
	if intents != "" {
		_ = json.Unmarshal([]byte(intents), &p.Intents)
	}
	if picks != "" {
		_ = json.Unmarshal([]byte(picks), &p.Picks)
	}
	return p
}

func (s *Store) GetPlan(id int64) (matching.Plan, error) {
	var accountID int64
	var signalDate, execAt, intents, picks, status string
	err := s.db.QueryRow(`SELECT id, account_id, signal_date, exec_at, intents, picks, status FROM plan WHERE id=?`, id).
		Scan(&id, &accountID, &signalDate, &execAt, &intents, &picks, &status)
	if err != nil {
		return matching.Plan{}, err
	}
	return scanPlan(id, accountID, signalDate, execAt, intents, picks, status), nil
}

func (s *Store) ListPlans(accountID int) ([]matching.Plan, error) {
	rows, err := s.db.Query(`SELECT id, account_id, signal_date, exec_at, intents, picks, status FROM plan WHERE account_id=? ORDER BY exec_at`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []matching.Plan
	for rows.Next() {
		var id, accID int64
		var signalDate, execAt, intents, picks, status string
		if err := rows.Scan(&id, &accID, &signalDate, &execAt, &intents, &picks, &status); err != nil {
			return nil, err
		}
		out = append(out, scanPlan(id, accID, signalDate, execAt, intents, picks, status))
	}
	return out, rows.Err()
}

func (s *Store) UpdatePlanPicks(id int64, picks []matching.Pick) error {
	raw, err := marshalJSONList(picks)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE plan SET picks=? WHERE id=?`, raw, id)
	return err
}

func (s *Store) MarkPlanDone(id int64, intents []matching.Intent) (bool, error) {
	return s.MarkPlanDoneTx(nil, id, intents)
}

func (s *Store) MarkPlanDoneTx(tx *sql.Tx, id int64, intents []matching.Intent) (bool, error) {
	raw, err := json.Marshal(intents)
	if err != nil {
		return false, err
	}
	res, err := s.q(tx).Exec(`UPDATE plan SET status='done', intents=? WHERE id=? AND status='pending'`, string(raw), id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (s *Store) InsertFillsTx(tx *sql.Tx, fills []ledger.Fill) error {
	if len(fills) == 0 {
		return nil
	}
	run := func(q queryer) error {
		stmt, err := q.Prepare(`INSERT INTO fill (account_id, plan_id, date, code, side, shares, price, amount, fee, slippage, status)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, f := range fills {
			var planID any
			if f.PlanID != 0 {
				planID = f.PlanID
			}
			if _, err := stmt.Exec(f.AccountID, planID, f.Date, f.Code, f.Side, f.Shares, f.Price, f.Amount, f.Fee, f.Slippage, f.Status); err != nil {
				return err
			}
		}
		return nil
	}
	if tx != nil {
		return run(tx)
	}
	own, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer own.Rollback()
	if err := run(own); err != nil {
		return err
	}
	return own.Commit()
}

func (s *Store) ListFills(accountID int) ([]ledger.Fill, error) {
	rows, err := s.db.Query(`SELECT id, account_id, COALESCE(plan_id, 0), date, code, side, shares, price, amount, fee, slippage, status
		FROM fill WHERE account_id=? ORDER BY id`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ledger.Fill
	for rows.Next() {
		var f ledger.Fill
		if err := rows.Scan(&f.ID, &f.AccountID, &f.PlanID, &f.Date, &f.Code, &f.Side, &f.Shares, &f.Price, &f.Amount, &f.Fee, &f.Slippage, &f.Status); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAccount(id int) error {
	_, err := s.db.Exec(`DELETE FROM account WHERE id=?`, id)
	return err
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		t = time.Now()
	}
	return t.UTC().Format(time.RFC3339Nano)
}
