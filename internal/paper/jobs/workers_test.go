package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"factor-lab/internal/panel"
	"factor-lab/internal/paper/account"
	"factor-lab/internal/paper/matching"
	"factor-lab/internal/paper/store"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

func TestOpenAccountEnqueuesGenerate(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t, testPanel(t, 3, 2))
	acc, err := OpenAccount(ctx, env.deps, account.Account{
		Name: "t", Expr: "alpha = close",
		Config: account.Config{TopN: 1, Capital: 10000, Start: "2024-01-01"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if acc.ID < 1 || acc.Config.Start != "2024-01-01" {
		t.Fatalf("%+v", acc)
	}
	snaps, err := env.st.ListSnapshots(acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 0 {
		t.Fatalf("open must not write snapshot: %+v", snaps)
	}
	plans, _ := env.st.ListPlans(acc.ID)
	if len(plans) != 0 {
		t.Fatalf("open must not write plan: %d", len(plans))
	}
	fills, _ := env.st.ListFills(acc.ID)
	if len(fills) != 0 {
		t.Fatal("open must not write fill")
	}
	jobs := listKind(t, env.client, GenerateArgs{}.Kind())
	if len(jobs) != 1 {
		t.Fatalf("generate jobs %d", len(jobs))
	}
	var args GenerateArgs
	if err := json.Unmarshal(jobs[0].EncodedArgs, &args); err != nil {
		t.Fatal(err)
	}
	if args.AccountID != acc.ID || args.SignalDate != "2024-01-01" {
		t.Fatalf("args %+v", args)
	}
	if jobs[0].State != rivertype.JobStateAvailable {
		t.Fatalf("past 15:10 must enqueue available, got %s", jobs[0].State)
	}
}

func TestOpenAccountSchedulesGenerateWhenNotYetDue(t *testing.T) {
	start := time.Now().In(shanghai).AddDate(0, 0, 2)
	ymd := start.Format("2006-01-02")
	env := newEnv(t, testPanelFrom(t, start, 3, 2))
	if _, err := OpenAccount(context.Background(), env.deps, account.Account{
		Name: "t", Expr: "alpha = close",
		Config: account.Config{TopN: 1, Capital: 10000, Start: ymd},
	}); err != nil {
		t.Fatal(err)
	}
	jobs := listKind(t, env.client, GenerateArgs{}.Kind())
	if len(jobs) != 1 {
		t.Fatalf("generate jobs %d", len(jobs))
	}
	want, _ := shanghaiAt(ymd, "15:10:00")
	if jobs[0].State != rivertype.JobStateScheduled {
		t.Fatalf("future 15:10 must stay scheduled, got %s", jobs[0].State)
	}
	if !jobs[0].ScheduledAt.Equal(want) {
		t.Fatalf("scheduled %v want %v", jobs[0].ScheduledAt, want)
	}
}

func TestOpenAccountRollbackOnBadStart(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t, testPanel(t, 3, 2))
	_, err := OpenAccount(ctx, env.deps, account.Account{
		Name: "t", Expr: "alpha = close",
		Config: account.Config{TopN: 1, Capital: 10000, Start: "2024-02-01"},
	})
	if err == nil {
		t.Fatal("expected start after last day to fail")
	}
	accs, err := env.st.ListAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(accs) != 0 {
		t.Fatalf("rolled back accounts %d", len(accs))
	}
	if n := len(listKind(t, env.client, GenerateArgs{}.Kind())); n != 0 {
		t.Fatalf("generate jobs %d", n)
	}
}

func TestGenerateEmptySignalDateErrors(t *testing.T) {
	env := newEnv(t, testPanel(t, 3, 2))
	acc := mustOpen(t, env, "2024-01-01")
	err := env.gen.Work(context.Background(), &river.Job[GenerateArgs]{
		Args: GenerateArgs{AccountID: acc.ID, SignalDate: ""},
	})
	if err == nil {
		t.Fatal("expected empty signal_date error")
	}
	var snooze *rivertype.JobSnoozeError
	if errors.As(err, &snooze) {
		t.Fatal("must not snooze empty signal_date")
	}
	plans, _ := env.st.ListPlans(acc.ID)
	if len(plans) != 0 {
		t.Fatal("must not write plan")
	}
	snaps, _ := env.st.ListSnapshots(acc.ID)
	if len(snaps) != 0 {
		t.Fatal("must not write snapshot")
	}
}

func TestGenerateIncompleteSnoozes(t *testing.T) {
	env := newEnv(t, testPanel(t, 1, 2))
	env.deps.Reload = func(context.Context) (*panel.Panel, error) {
		return testPanel(t, 1, 2), nil
	}
	acc := mustOpen(t, env, "2024-01-01")
	err := env.gen.Work(context.Background(), &river.Job[GenerateArgs]{
		Args: GenerateArgs{AccountID: acc.ID, SignalDate: "2024-01-01"},
	})
	var snooze *rivertype.JobSnoozeError
	if !errors.As(err, &snooze) || snooze.Duration != time.Minute {
		t.Fatalf("want snooze 1m, got %v", err)
	}
	plans, _ := env.st.ListPlans(acc.ID)
	if len(plans) != 0 {
		t.Fatal("incomplete must not write plan")
	}
	snaps, _ := env.st.ListSnapshots(acc.ID)
	if len(snaps) != 0 {
		t.Fatal("incomplete must not write snapshot")
	}
}

func TestGenerateCSVDiscard(t *testing.T) {
	env := newEnv(t, testPanel(t, 1, 2))
	env.deps.Reload = nil
	acc := mustOpen(t, env, "2024-01-01")
	err := env.gen.Work(context.Background(), &river.Job[GenerateArgs]{
		Args: GenerateArgs{AccountID: acc.ID, SignalDate: "2024-01-01"},
	})
	var cancel *rivertype.JobCancelError
	if !errors.As(err, &cancel) {
		t.Fatalf("want discard/cancel, got %v", err)
	}
}

func TestGenerateSuccessInsertsPlanAndExecuteJob(t *testing.T) {
	p := testPanel(t, 3, 2)
	lastCloseBefore := p.Cols["close"][len(p.Cols["close"])-1]
	env := newEnv(t, p)
	acc := mustOpen(t, env, "2024-01-01")
	if err := env.gen.Work(context.Background(), &river.Job[GenerateArgs]{
		Args: GenerateArgs{AccountID: acc.ID, SignalDate: "2024-01-01"},
	}); err != nil {
		t.Fatal(err)
	}
	if p.T() != 3 || p.Cols["close"][len(p.Cols["close"])-1] != lastCloseBefore {
		t.Fatal("generate must not mutate process panel")
	}
	plans, err := env.st.ListPlans(acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 {
		t.Fatalf("plans %d", len(plans))
	}
	plan := plans[0]
	if plan.SignalDate != "2024-01-01" || plan.ExecAt != "2024-01-02 09:30:00" || plan.Status != "pending" {
		t.Fatalf("%+v", plan)
	}
	if len(plan.Intents) == 0 {
		t.Fatal("empty intents")
	}
	if len(plan.Picks) != 1 || plan.Picks[0].Code != "A" || plan.Picks[0].Rank != 1 {
		t.Fatalf("picks %+v", plan.Picks)
	}
	for _, in := range plan.Intents {
		if in.ActualShares != 0 {
			t.Fatalf("actuals must be 0 %+v", in)
		}
		if in.Shares != 0 && int(in.Shares)%100 != 0 {
			t.Fatalf("lots %+v", in)
		}
		switch in.Side {
		case matching.SideOpen, matching.SideExit, matching.SideAdd, matching.SideReduce, matching.SideHold:
		default:
			t.Fatalf("bad side %q", in.Side)
		}
	}
	a, ok := findIntent(plan, "A")
	if !ok || a.Side != matching.SideOpen || a.Shares != 1000 {
		t.Fatalf("A %+v", a)
	}
	snaps, err := env.st.ListSnapshots(acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 1 || snaps[0].Date != "2024-01-01" || snaps[0].Cash != 10000 || snaps[0].NAV != 10000 {
		t.Fatalf("seed snapshot %+v", snaps)
	}
	jobs := listKind(t, env.client, ExecuteArgs{}.Kind())
	if len(jobs) != 1 {
		t.Fatalf("execute jobs %d", len(jobs))
	}
	var args ExecuteArgs
	if err := json.Unmarshal(jobs[0].EncodedArgs, &args); err != nil {
		t.Fatal(err)
	}
	if args.PlanID != plan.ID || args.AccountID != acc.ID {
		t.Fatalf("execute args %+v", args)
	}
	if jobs[0].State != rivertype.JobStateAvailable {
		t.Fatalf("past 09:30 must enqueue available, got %s", jobs[0].State)
	}
}

func TestGenerateSchedulesExecuteWhenNotYetDue(t *testing.T) {
	start := time.Now().In(shanghai).AddDate(0, 0, 2)
	ymd := start.Format("2006-01-02")
	env := newEnv(t, testPanelFrom(t, start, 3, 2))
	acc := mustOpen(t, env, ymd)
	if err := env.gen.Work(context.Background(), &river.Job[GenerateArgs]{
		Args: GenerateArgs{AccountID: acc.ID, SignalDate: ymd},
	}); err != nil {
		t.Fatal(err)
	}
	jobs := listKind(t, env.client, ExecuteArgs{}.Kind())
	if len(jobs) != 1 {
		t.Fatalf("execute jobs %d", len(jobs))
	}
	next := start.AddDate(0, 0, 1).Format("2006-01-02")
	want, _ := shanghaiAt(next, "09:30:00")
	if jobs[0].State != rivertype.JobStateScheduled {
		t.Fatalf("future 09:30 must stay scheduled, got %s", jobs[0].State)
	}
	if !jobs[0].ScheduledAt.Equal(want) {
		t.Fatalf("scheduled %v want %v", jobs[0].ScheduledAt, want)
	}
}

func TestGenerateDuplicateSignalDateNoSecondPlan(t *testing.T) {
	env := newEnv(t, testPanel(t, 3, 2))
	acc := mustOpen(t, env, "2024-01-01")
	args := GenerateArgs{AccountID: acc.ID, SignalDate: "2024-01-01"}
	if err := env.gen.Work(context.Background(), &river.Job[GenerateArgs]{Args: args}); err != nil {
		t.Fatal(err)
	}
	if err := env.gen.Work(context.Background(), &river.Job[GenerateArgs]{Args: args}); err != nil {
		t.Fatal(err)
	}
	plans, _ := env.st.ListPlans(acc.ID)
	if len(plans) != 1 {
		t.Fatalf("plans %d", len(plans))
	}
}

func TestExecuteInvalidExecAtErrors(t *testing.T) {
	env := newEnv(t, testPanel(t, 3, 2))
	acc := mustOpen(t, env, "2024-01-01")
	pid, err := env.st.InsertPlan(matching.Plan{
		AccountID: acc.ID, SignalDate: "2024-01-01", ExecAt: "2024-01-02",
		Status: "pending", Intents: []matching.Intent{{Code: "A", Side: matching.SideOpen, Shares: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = env.exec.Work(context.Background(), &river.Job[ExecuteArgs]{
		Args: ExecuteArgs{AccountID: acc.ID, PlanID: pid},
	})
	if err == nil {
		t.Fatal("expected exec_at format error")
	}
	plan, _ := env.st.GetPlan(pid)
	if plan.Status != "pending" {
		t.Fatalf("status %s", plan.Status)
	}
}

func TestExecuteMissingBarSnoozes(t *testing.T) {
	env := newEnv(t, testPanel(t, 1, 2))
	env.deps.Reload = func(context.Context) (*panel.Panel, error) {
		return testPanel(t, 1, 2), nil
	}
	acc := mustOpen(t, env, "2024-01-01")
	pid, err := env.st.InsertPlan(matching.Plan{
		AccountID: acc.ID, SignalDate: "2024-01-01", ExecAt: "2024-01-02 09:30:00",
		Status: "pending", Intents: []matching.Intent{{Code: "A", Side: matching.SideOpen, Shares: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = env.exec.Work(context.Background(), &river.Job[ExecuteArgs]{
		Args: ExecuteArgs{AccountID: acc.ID, PlanID: pid},
	})
	var snooze *rivertype.JobSnoozeError
	if !errors.As(err, &snooze) {
		t.Fatalf("want snooze, got %v", err)
	}
	fills, _ := env.st.ListFills(acc.ID)
	if len(fills) != 0 {
		t.Fatal("must not write fill")
	}
	plan, _ := env.st.GetPlan(pid)
	if plan.Status != "pending" || plan.Intents[0].ActualShares != 0 {
		t.Fatalf("%+v", plan)
	}
}

func TestExecuteSuccessWritesFillsAndNextGenerate(t *testing.T) {
	env := newEnv(t, testPanel(t, 3, 2))
	acc := mustOpen(t, env, "2024-01-01")
	if err := env.gen.Work(context.Background(), &river.Job[GenerateArgs]{
		Args: GenerateArgs{AccountID: acc.ID, SignalDate: "2024-01-01"},
	}); err != nil {
		t.Fatal(err)
	}
	plans, err := env.st.ListPlans(acc.ID)
	if err != nil || len(plans) != 1 {
		t.Fatalf("plans %v %v", plans, err)
	}
	plan := plans[0]
	if err := env.exec.Work(context.Background(), &river.Job[ExecuteArgs]{
		Args: ExecuteArgs{AccountID: acc.ID, PlanID: plan.ID},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := env.st.GetPlan(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "done" {
		t.Fatalf("status %s", got.Status)
	}
	orig := plan.Intents[0]
	done := got.Intents[0]
	if done.Shares != orig.Shares || done.Side != orig.Side {
		t.Fatalf("plan shares/side mutated %+v vs %+v", orig, done)
	}
	fills, err := env.st.ListFills(acc.ID)
	if err != nil || len(fills) == 0 {
		t.Fatalf("fills %v %v", fills, err)
	}
	for _, in := range got.Intents {
		var buy, sell float64
		for _, f := range fills {
			if f.Code != in.Code {
				continue
			}
			if f.Side == "buy" && f.Status == "filled" {
				buy = f.Shares
			}
			if f.Side == "sell" && f.Status == "filled" {
				sell = f.Shares
			}
		}
		if in.ActualShares != buy+sell {
			t.Fatalf("actuals %+v buy=%v sell=%v", in, buy, sell)
		}
	}
	snaps, _ := env.st.ListSnapshots(acc.ID)
	var tradeSnap bool
	for _, s := range snaps {
		if s.Date == "2024-01-02" {
			tradeSnap = true
		}
	}
	if !tradeSnap {
		t.Fatalf("missing trade snapshot %+v", snaps)
	}

	gens := listKind(t, env.client, GenerateArgs{}.Kind())
	var next GenerateArgs
	found := false
	for _, j := range gens {
		var a GenerateArgs
		_ = json.Unmarshal(j.EncodedArgs, &a)
		if a.SignalDate == "2024-01-02" && a.AccountID == acc.ID {
			found = true
			next = a
			if j.State != rivertype.JobStateAvailable {
				t.Fatalf("past 15:10 must enqueue available, got %s", j.State)
			}
		}
	}
	if !found {
		t.Fatal("missing next generate on fill date")
	}

	if err := env.gen.Work(context.Background(), &river.Job[GenerateArgs]{Args: next}); err != nil {
		t.Fatal(err)
	}
	plans, _ = env.st.ListPlans(acc.ID)
	if len(plans) != 2 {
		t.Fatalf("second plan missing: %d", len(plans))
	}
	var second matching.Plan
	for _, p := range plans {
		if p.ID != plan.ID {
			second = p
		}
	}
	if second.SignalDate != "2024-01-02" || second.ExecAt != "2024-01-03 09:30:00" {
		t.Fatalf("second plan %+v", second)
	}
}

func TestExecuteSchedulesNextGenerateWhenNotYetDue(t *testing.T) {
	start := time.Now().In(shanghai).AddDate(0, 0, 2)
	ymd := start.Format("2006-01-02")
	env := newEnv(t, testPanelFrom(t, start, 3, 2))
	acc := mustOpen(t, env, ymd)
	if err := env.gen.Work(context.Background(), &river.Job[GenerateArgs]{
		Args: GenerateArgs{AccountID: acc.ID, SignalDate: ymd},
	}); err != nil {
		t.Fatal(err)
	}
	plans, err := env.st.ListPlans(acc.ID)
	if err != nil || len(plans) != 1 {
		t.Fatalf("plans %v %v", plans, err)
	}
	if err := env.exec.Work(context.Background(), &river.Job[ExecuteArgs]{
		Args: ExecuteArgs{AccountID: acc.ID, PlanID: plans[0].ID},
	}); err != nil {
		t.Fatal(err)
	}
	fillDay := start.AddDate(0, 0, 1).Format("2006-01-02")
	want, _ := shanghaiAt(fillDay, "15:10:00")
	var got *rivertype.JobRow
	for _, j := range listKind(t, env.client, GenerateArgs{}.Kind()) {
		var a GenerateArgs
		_ = json.Unmarshal(j.EncodedArgs, &a)
		if a.SignalDate == fillDay && a.AccountID == acc.ID {
			got = j
		}
	}
	if got == nil {
		t.Fatal("missing next generate on fill date")
	}
	if got.State != rivertype.JobStateScheduled {
		t.Fatalf("future 15:10 must stay scheduled, got %s", got.State)
	}
	if !got.ScheduledAt.Equal(want) {
		t.Fatalf("scheduled %v want %v", got.ScheduledAt, want)
	}
}

func TestExecuteSecondTimeNoOp(t *testing.T) {
	env := newEnv(t, testPanel(t, 3, 2))
	acc := mustOpen(t, env, "2024-01-01")
	if err := env.gen.Work(context.Background(), &river.Job[GenerateArgs]{
		Args: GenerateArgs{AccountID: acc.ID, SignalDate: "2024-01-01"},
	}); err != nil {
		t.Fatal(err)
	}
	plans, _ := env.st.ListPlans(acc.ID)
	args := ExecuteArgs{AccountID: acc.ID, PlanID: plans[0].ID}
	if err := env.exec.Work(context.Background(), &river.Job[ExecuteArgs]{Args: args}); err != nil {
		t.Fatal(err)
	}
	book1, _ := env.st.LatestBook(acc.ID)
	plan1, _ := env.st.GetPlan(plans[0].ID)
	fills1, _ := env.st.ListFills(acc.ID)
	if err := env.exec.Work(context.Background(), &river.Job[ExecuteArgs]{Args: args}); err != nil {
		t.Fatal(err)
	}
	book2, _ := env.st.LatestBook(acc.ID)
	plan2, _ := env.st.GetPlan(plans[0].ID)
	fills2, _ := env.st.ListFills(acc.ID)
	if book2.Cash != book1.Cash {
		t.Fatalf("cash %v -> %v", book1.Cash, book2.Cash)
	}
	if len(fills2) != len(fills1) {
		t.Fatalf("fills %d -> %d", len(fills1), len(fills2))
	}
	if plan2.Intents[0].ActualShares != plan1.Intents[0].ActualShares {
		t.Fatal("actuals changed")
	}
}

type testEnv struct {
	st     *store.Store
	deps   *Deps
	client *river.Client[*sql.Tx]
	gen    *GenerateWorker
	exec   *ExecuteWorker
}

func newEnv(t *testing.T, p *panel.Panel) *testEnv {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "paper.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := MigrateRiver(context.Background(), st.DB()); err != nil {
		t.Fatal(err)
	}
	hold := p
	deps := &Deps{
		Store: st,
		Panel: func() *panel.Panel { return hold },
		Reload: func(context.Context) (*panel.Panel, error) {
			return hold, nil
		},
	}
	client, err := NewClient(st.DB(), deps)
	if err != nil {
		t.Fatal(err)
	}
	deps.Client = client
	return &testEnv{
		st: st, deps: deps, client: client,
		gen:  &GenerateWorker{Deps: deps},
		exec: &ExecuteWorker{Deps: deps},
	}
}

func mustOpen(t *testing.T, env *testEnv, start string) account.Account {
	t.Helper()
	acc, err := OpenAccount(context.Background(), env.deps, account.Account{
		Name: "t", Expr: "alpha = close",
		Config: account.Config{TopN: 1, Capital: 10000, Cost: 0, Start: start},
	})
	if err != nil {
		t.Fatal(err)
	}
	return acc
}

func testPanel(t *testing.T, T, N int) *panel.Panel {
	t.Helper()
	return testPanelFrom(t, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), T, N)
}

func testPanelFrom(t *testing.T, start time.Time, T, N int) *panel.Panel {
	t.Helper()
	dates := make([]time.Time, T)
	for i := range dates {
		dates[i] = start.AddDate(0, 0, i)
	}
	codes := make([]string, N)
	for j := range codes {
		codes[j] = string(rune('A' + j))
	}
	p := panel.New(dates, codes)
	close := make(panel.Col, T*N)
	open := make(panel.Col, T*N)
	for i := range close {
		close[i] = 10
		open[i] = 10
	}
	if err := p.SetCol("close", close); err != nil {
		t.Fatal(err)
	}
	if err := p.SetCol("open", open); err != nil {
		t.Fatal(err)
	}
	return p
}

func listKind(t *testing.T, client *river.Client[*sql.Tx], kind string) []*rivertype.JobRow {
	t.Helper()
	res, err := client.JobList(context.Background(), river.NewJobListParams().Kinds(kind))
	if err != nil {
		t.Fatal(err)
	}
	return res.Jobs
}

func findIntent(plan matching.Plan, code string) (matching.Intent, bool) {
	for _, in := range plan.Intents {
		if in.Code == code {
			return in, true
		}
	}
	return matching.Intent{}, false
}
