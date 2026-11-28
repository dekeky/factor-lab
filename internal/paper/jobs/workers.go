package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"factor-lab/internal/engine"
	"factor-lab/internal/panel"
	"factor-lab/internal/paper/account"
	"factor-lab/internal/paper/ledger"
	"factor-lab/internal/paper/matching"
	"factor-lab/internal/paper/store"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riversqlite"
	"github.com/riverqueue/river/rivermigrate"

	_ "time/tzdata"
)

var shanghai = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}()

// Deps is shared by OpenAccount and both workers.
type Deps struct {
	Store  *store.Store
	Client *river.Client[*sql.Tx]
	Panel  func() *panel.Panel
	Reload func(context.Context) (*panel.Panel, error)
	// OpenDates 已公布的开市日（YYYY-MM-DD）。空则退回面板日期轴（测试 / CSV）。
	OpenDates func() []string
}

type GenerateWorker struct {
	river.WorkerDefaults[GenerateArgs]
	Deps *Deps
}

type ExecuteWorker struct {
	river.WorkerDefaults[ExecuteArgs]
	Deps *Deps
}

func MigrateRiver(ctx context.Context, db *sql.DB) error {
	migrator, err := rivermigrate.New(riversqlite.New(db), &rivermigrate.Config{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		return err
	}
	_, err = migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	return err
}

func NewClient(db *sql.DB, deps *Deps) (*river.Client[*sql.Tx], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, &GenerateWorker{Deps: deps})
	river.AddWorker(workers, &ExecuteWorker{Deps: deps})
	return river.NewClient(riversqlite.New(db), &river.Config{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Queues: map[string]river.QueueConfig{
			QueueName: {MaxWorkers: 1},
		},
		Workers: workers,
	})
}

func OpenAccount(ctx context.Context, deps *Deps, acc account.Account) (account.Account, error) {
	if deps == nil || deps.Store == nil || deps.Client == nil {
		return account.Account{}, fmt.Errorf("模拟盘依赖未初始化")
	}
	p := currentPanel(deps)
	start, err := resolveStart(p, acc.Config.Start)
	if err != nil {
		return account.Account{}, err
	}
	if strings.TrimSpace(acc.Expr) == "" {
		return account.Account{}, fmt.Errorf("公式为空")
	}
	cfg := acc.Config
	if cfg.TopN < 1 {
		cfg.TopN = account.DefaultConfig().TopN
	}
	if cfg.Capital <= 0 {
		cfg.Capital = account.DefaultConfig().Capital
	}
	cfg.Start = start
	acc.Config = cfg
	now := time.Now()
	if acc.CreatedAt.IsZero() {
		acc.CreatedAt = now
	}
	if acc.UpdatedAt.IsZero() {
		acc.UpdatedAt = now
	}
	at, err := shanghaiAt(start, "15:10:00")
	if err != nil {
		return account.Account{}, err
	}

	tx, err := deps.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return account.Account{}, err
	}
	defer tx.Rollback()

	id, err := deps.Store.InsertAccountTx(tx, acc)
	if err != nil {
		return account.Account{}, err
	}
	acc.ID = id
	if _, err := deps.Client.InsertTx(ctx, tx, GenerateArgs{AccountID: id, SignalDate: start}, insertWhenDue(at)); err != nil {
		return account.Account{}, err
	}
	if err := tx.Commit(); err != nil {
		return account.Account{}, err
	}
	return acc, nil
}

func (w *GenerateWorker) Work(ctx context.Context, job *river.Job[GenerateArgs]) error {
	if w.Deps == nil || w.Deps.Store == nil {
		return fmt.Errorf("模拟盘依赖未初始化")
	}
	args := job.Args
	if strings.TrimSpace(args.SignalDate) == "" {
		return fmt.Errorf("signal_date 为空")
	}
	acc, err := w.Deps.Store.GetAccount(args.AccountID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}

	p, err := w.loadForGenerate(ctx, acc.Expr, args.SignalDate)
	if err != nil {
		return err
	}

	to, ok := panelDate(p, args.SignalDate)
	if !ok {
		return river.JobSnooze(snoozeIncomplete)
	}
	slice, err := p.Slice(time.Time{}, to)
	if err != nil {
		return river.JobSnooze(snoozeIncomplete)
	}
	col, errs := engine.Run(acc.Expr, slice)
	if len(errs) > 0 {
		return fmt.Errorf("因子无法计算：%s", joinErrs(errs))
	}

	book, err := w.Deps.Store.LatestBook(acc.ID)
	if err != nil {
		return err
	}
	book, seed := seedCash(acc, book)

	plan, err := matching.Generate(slice, col, acc, book, args.SignalDate)
	if err != nil {
		return err
	}
	nextDay, ok := nextSession(w.Deps, p, args.SignalDate)
	if !ok {
		return river.JobSnooze(snoozeIncomplete)
	}
	plan.ExecAt = nextDay + " 09:30:00"
	plan.Status = "pending"
	plan.AccountID = acc.ID

	execAt, err := shanghaiAt(nextDay, "09:30:00")
	if err != nil {
		return err
	}

	if w.Deps.Client == nil {
		return fmt.Errorf("队列未初始化")
	}
	tx, err := w.Deps.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if seed {
		snap := ledger.Snapshot{
			AccountID: acc.ID, Date: args.SignalDate,
			Cash: book.Cash, NAV: book.Cash, Ret: 0,
			Holdings: []ledger.Position{},
		}
		if err := w.Deps.Store.InsertSnapshotTx(tx, snap); err != nil && !store.IsConflict(err) {
			return err
		}
	}

	pid, err := w.Deps.Store.InsertPlanTx(tx, plan)
	if store.IsConflict(err) {
		if seed {
			return tx.Commit()
		}
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := w.Deps.Client.InsertTx(ctx, tx, ExecuteArgs{AccountID: acc.ID, PlanID: pid}, insertWhenDue(execAt)); err != nil {
		return err
	}
	return tx.Commit()
}

func (w *GenerateWorker) loadForGenerate(ctx context.Context, expr, signalDate string) (*panel.Panel, error) {
	p := currentPanel(w.Deps)
	if generateReady(w.Deps, p, expr, signalDate) {
		return p, nil
	}
	if w.Deps.Reload == nil {
		return nil, river.JobCancel(fmt.Errorf("数据不全且无法 Reload"))
	}
	np, err := w.Deps.Reload(ctx)
	if err != nil {
		return nil, river.JobSnooze(snoozeIncomplete)
	}
	if !generateReady(w.Deps, np, expr, signalDate) {
		return nil, river.JobSnooze(snoozeIncomplete)
	}
	return np, nil
}

func (w *ExecuteWorker) Work(ctx context.Context, job *river.Job[ExecuteArgs]) error {
	if w.Deps == nil || w.Deps.Store == nil {
		return fmt.Errorf("模拟盘依赖未初始化")
	}
	plan, err := w.Deps.Store.GetPlan(job.Args.PlanID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if plan.Status == "done" {
		return nil
	}
	if _, err := parseExecAt(plan.ExecAt); err != nil {
		return err
	}
	day := plan.ExecAt[:10]

	acc, err := w.Deps.Store.GetAccount(plan.AccountID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}

	p, err := w.loadForExecute(ctx, day)
	if err != nil {
		return err
	}

	to, ok := panelDate(p, day)
	if !ok {
		return river.JobSnooze(snoozeIncomplete)
	}
	slice, err := p.Slice(time.Time{}, to)
	if err != nil {
		return river.JobSnooze(snoozeIncomplete)
	}

	book, err := w.Deps.Store.LatestBook(acc.ID)
	if err != nil {
		return err
	}
	book, _ = seedCash(acc, book)

	fills, intents, _, snap, err := matching.Execute(slice, plan, book, acc.Config, plan.ExecAt)
	if err != nil {
		return err
	}
	snap.AccountID = acc.ID
	if prev, err := w.Deps.Store.ListSnapshots(acc.ID); err == nil && len(prev) > 0 && prev[len(prev)-1].NAV > 0 {
		snap.Ret = snap.NAV/prev[len(prev)-1].NAV - 1
	}

	at, err := shanghaiAt(day, "15:10:00")
	if err != nil {
		return err
	}
	if w.Deps.Client == nil {
		return fmt.Errorf("队列未初始化")
	}

	tx, err := w.Deps.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	marked, err := w.Deps.Store.MarkPlanDoneTx(tx, plan.ID, intents)
	if err != nil {
		return err
	}
	if !marked {
		return nil
	}
	if err := w.Deps.Store.InsertFillsTx(tx, fills); err != nil {
		return err
	}
	if err := w.Deps.Store.InsertSnapshotTx(tx, snap); err != nil {
		if store.IsConflict(err) {
			return nil
		}
		return err
	}
	if _, err := w.Deps.Client.InsertTx(ctx, tx, GenerateArgs{AccountID: acc.ID, SignalDate: day}, insertWhenDue(at)); err != nil {
		return err
	}
	return tx.Commit()
}

func (w *ExecuteWorker) loadForExecute(ctx context.Context, day string) (*panel.Panel, error) {
	p := currentPanel(w.Deps)
	if executeReady(p, day) {
		return p, nil
	}
	if w.Deps.Reload == nil {
		return nil, river.JobCancel(fmt.Errorf("数据不全且无法 Reload"))
	}
	np, err := w.Deps.Reload(ctx)
	if err != nil {
		return nil, river.JobSnooze(snoozeIncomplete)
	}
	if !executeReady(np, day) {
		return nil, river.JobSnooze(snoozeIncomplete)
	}
	return np, nil
}

func currentPanel(deps *Deps) *panel.Panel {
	if deps == nil || deps.Panel == nil {
		return nil
	}
	return deps.Panel()
}

func resolveStart(p *panel.Panel, start string) (string, error) {
	if p == nil || p.T() == 0 {
		return "", fmt.Errorf("尚未加载行情")
	}
	labels := p.DateLabels()
	start = strings.TrimSpace(start)
	if start == "" {
		return labels[0], nil
	}
	for _, d := range labels {
		if d >= start {
			return d, nil
		}
	}
	return "", fmt.Errorf("起始日晚于面板最后一天")
}

func seedCash(acc account.Account, book ledger.Book) (ledger.Book, bool) {
	if book.LastSettled != "" {
		book.AccountID = acc.ID
		return book, false
	}
	book.Cash = acc.Config.Capital
	book.AccountID = acc.ID
	return book, true
}

func shanghaiAt(ymd, hms string) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", ymd+" "+hms, shanghai)
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}

// insertWhenDue 时刻已过则立刻可跑（available），未到则 ScheduledAt 等到那一秒。
func insertWhenDue(at time.Time) *river.InsertOpts {
	opts := &river.InsertOpts{}
	if at.After(time.Now()) {
		opts.ScheduledAt = at
	}
	return opts
}

func parseExecAt(execAt string) (time.Time, error) {
	s := strings.TrimSpace(execAt)
	if s == "" || len(s) != 19 {
		return time.Time{}, fmt.Errorf("exec_at 为空或不是 YYYY-MM-DD HH:MM:SS")
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", s, shanghai)
	if err != nil {
		return time.Time{}, fmt.Errorf("exec_at 不是 YYYY-MM-DD HH:MM:SS")
	}
	return t, nil
}

func sessionDates(deps *Deps, p *panel.Panel) []string {
	if deps != nil && deps.OpenDates != nil {
		if d := deps.OpenDates(); len(d) > 0 {
			return d
		}
	}
	if p == nil {
		return nil
	}
	return p.DateLabels()
}

func nextSession(deps *Deps, p *panel.Panel, ymd string) (string, bool) {
	for _, d := range sessionDates(deps, p) {
		if d > ymd {
			return d, true
		}
	}
	return "", false
}

func hasClose(p *panel.Panel) bool {
	if p == nil {
		return false
	}
	if _, ok := p.Col("qfq_close"); ok {
		return true
	}
	_, ok := p.Col("close")
	return ok
}

func hasOpen(p *panel.Panel) bool {
	if p == nil {
		return false
	}
	if _, ok := p.Col("qfq_open"); ok {
		return true
	}
	_, ok := p.Col("open")
	return ok
}

func dateOnAxis(p *panel.Panel, ymd string) bool {
	_, ok := panelDate(p, ymd)
	return ok
}

func panelDate(p *panel.Panel, ymd string) (time.Time, bool) {
	if p == nil {
		return time.Time{}, false
	}
	for _, d := range p.Dates {
		if d.Format("2006-01-02") == ymd {
			return d, true
		}
	}
	return time.Time{}, false
}

func generateReady(deps *Deps, p *panel.Panel, expr, signalDate string) bool {
	if p == nil || p.N() == 0 || p.T() == 0 || !hasClose(p) {
		return false
	}
	if !dateOnAxis(p, signalDate) {
		return false
	}
	if _, ok := nextSession(deps, p, signalDate); !ok {
		return false
	}
	_, errs := engine.Run(expr, p)
	if len(errs) > 0 {
		// 缺列算没齐；公式本身写错留给 Work 报错。这里只在面板上试跑。
		joined := joinErrs(errs)
		if strings.Contains(joined, "未知字段") || strings.Contains(joined, "没有") || strings.Contains(joined, "未定义") {
			return false
		}
	}
	return true
}

func executeReady(p *panel.Panel, day string) bool {
	return p != nil && p.N() > 0 && hasOpen(p) && hasClose(p) && dateOnAxis(p, day)
}

func joinErrs(errs []error) string {
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		if e != nil {
			parts = append(parts, e.Error())
		}
	}
	return strings.Join(parts, "; ")
}
