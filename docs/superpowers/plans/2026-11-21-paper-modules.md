# Paper Modules Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace in-process `Replay` with open-account → `paper.generate` → `paper.execute` on River, freezing buy/sell shares at signal close and filling at next-day open.

**Architecture:** Domain packages under `internal/paper/{account,ledger,matching,jobs}` plus SQLite (`data/paper.db`) shared with River. HTTP only opens accounts, deletes, and reads assembled `Paper`. Workers pull K themselves and snooze 1 minute when data is incomplete.

**Tech Stack:** Go 1.26, SQLite, [River](https://github.com/riverqueue/river) (`riversqlite`), existing `panel` / `engine`.

**Spec:** `docs/superpowers/specs/2026-11-20-paper-modules-design.md`

## Global Constraints

- Queue kind `paper.generate` / `paper.execute`; queue name `paper`; `MaxWorkers: 1`.
- Times Asia/Shanghai, `YYYY-MM-DD HH:MM:SS`; generate at `15:10:00`, execute at next session `09:30:00`.
- Lot size 100. Buy cash check: `shares * open * (1+SlippageBps/1e4) * (1+Cost)`.
- `Reason` is one of `enter` / `exit` / `rebalance` / empty. Fill skip reasons stay on `Fill.Status`.
- `Generate` and `Execute` must not call each other. HTTP must not call them.
- Do not write `panel.Slice` results back onto the process panel.
- Old `paper.json` is not imported. `Replay` leaves the live path; delete it after HTTP no longer calls it.
- Weight drift threshold is **0.03** (not a Config field).

## Review Focus

- Open after panel last date: create must fail with no account row, no snapshot, no job.
- CSV source cannot Reload: generate job is Discarded, not snoozed forever.
- `exec_at` empty or not `YYYY-MM-DD HH:MM:SS`: execute errors (River retry), does not guess from the panel.
- Second execute of a `done` plan: no extra fill, actual shares unchanged.
- Buy with slippage+fee that only fits after dropping lots: Fill.Shares and ActualBuyShares match the reduced lots.

---

### Task 1: Ledger types and Apply

**Files:**
- Create: `internal/paper/ledger/ledger.go`
- Test: `internal/paper/ledger/ledger_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `Position`, `Fill`, `Snapshot`, `Book`; `func Apply(book Book, fills []Fill) (Book, error)`

- [ ] **Step 1: Write failing tests** `TestApplyBuyDebitsCashAndFee`, `TestApplySellCreditsCashMinusFee`, `TestApplySkippedDoesNotChangeBook`

```go
func TestApplyBuyDebitsCashAndFee(t *testing.T) {
    book := Book{Cash: 10000}
    next, err := Apply(book, []Fill{{
        Side: "buy", Shares: 100, Price: 10, Amount: 1000, Fee: 3, Status: "filled",
    }})
    // cash 10000-1000-3=8997; position 100 shares, cost (1000+3)/100=10.03
}
```

- [ ] **Step 2: Run** `go test ./internal/paper/ledger/ -count=1`
Expected: FAIL compile, package does not exist

- [ ] **Step 3: Implement types from spec §3.3 and `Apply`**
Skipped statuses (`skipped_*`) and `Shares==0` do not change cash or positions. Buy cost includes fee. Sell removes shares; leftover average cost unchanged.

- [ ] **Step 4: Run tests** Expected: PASS

- [ ] **Step 5: Commit** `feat(paper): add ledger Apply`

---

### Task 2: matching.Generate

**Files:**
- Create: `internal/paper/account/account.go` (`Config`, `Account` from spec §3.1)
- Create: `internal/paper/matching/generate.go`, `internal/paper/matching/panelutil.go` (close/open helpers)
- Test: `internal/paper/matching/generate_test.go`

**Interfaces:**
- Consumes: `account.Config`/`Account`, `ledger.Book`
- Produces: `Intent`, `Plan`; `const LotSize = 100`; `const WeightDrift = 0.03`; `func Generate(p *panel.Panel, factor panel.Col, acc account.Account, book ledger.Book, signalDate string) (Plan, error)`
  - `ExecAt` left empty (worker fills). `Actual*` always 0.
  - Rank all finite scores (halt/limit/missing still ranked).
  - Held with rank `>` TopN+RebalanceThreshold, or no score: `Side=sell`, `SellShares=current`, `Reason=exit`.
  - Empty book: take TopN by rank as `Side=buy`, `Reason=enter`.
  - Else fill `len(sells)` slots from remaining names in current TopN: `Side=buy`, `Reason=enter`.
  - Remaining held: `Side=hold`.
  - Size buys with `px = qfq_close else close`, denominator `px*(1+SlippageBps/1e4)*(1+Cost)`, floor to 100. Missing close → 0 shares, still emit Intent.
  - Equal-weight NAV = cash + held*px (missing px counts 0). Target weight `1/nTarget`. If `|w-target| > 0.03` on a hold, add lots as buy/sell with `Reason=rebalance`. Enter names already sized to equal weight stay `enter`.

- [ ] **Step 1: Tests** `TestGenerateEmptyBookEntersTopN`, `TestGenerateExitAndEnterReasons`, `TestGenerateBuyLotsReserveFeeAndSlippage`, `TestGenerateMissingCloseZeroShares`, `TestGenerateActualsAreZero`

- [ ] **Step 2: Run** `go test ./internal/paper/matching/ -count=1`
Expected: FAIL

- [ ] **Step 3: Implement `Generate`**

- [ ] **Step 4: Run tests** Expected: PASS

- [ ] **Step 5: Commit** `feat(paper): generate intents with shares and reasons`

---

### Task 3: matching.Execute

**Files:**
- Create: `internal/paper/matching/execute.go`
- Test: `internal/paper/matching/execute_test.go`

**Interfaces:**
- Consumes: Task 2 `Plan`/`Intent`, `ledger.Apply`
- Produces: `func Execute(p *panel.Panel, plan Plan, book ledger.Book, cfg account.Config, execAt string) (fills []ledger.Fill, intents []Intent, next ledger.Book, snap ledger.Snapshot, err error)`
  - Trade px: `qfq_open` else `open`; buy `*(1+bps/1e4)`, sell `*(1-bps/1e4)`. Mark px: `qfq_close` else `close`.
  - Sell all `SellShares>0` first (`min(plan, position)`), then buys. Halt / open limit / missing → skipped fill, Actual*=0.
  - Buy cash loop: while shares>=100 and cash < shares*px*(1+Cost), shares-=100.
  - Do not mutate `BuyShares`/`SellShares`/`Reason`. Return updated intents.
  - Snapshot.Date = calendar date of `execAt`; NAV = cash + holdings at mark px.

- [ ] **Step 1: Tests** `TestExecuteSellsBeforeBuys`, `TestExecuteSkipLimitUp`, `TestExecuteBuyReducesBy100WhenCashShort`, `TestExecuteWritesActualShares`, `TestExecuteDoesNotChangePlanSharesOrReason`

- [ ] **Step 2: Run** `go test ./internal/paper/matching/ -count=1`
Expected: FAIL on Execute tests

- [ ] **Step 3: Implement `Execute`**

- [ ] **Step 4: Run tests** Expected: PASS

- [ ] **Step 5: Commit** `feat(paper): execute planned shares at open`

---

### Task 4: SQLite store

**Files:**
- Create: `internal/paper/store/store.go`, `internal/paper/store/schema.sql` (verbatim spec §5)
- Test: `internal/paper/store/store_test.go`

**Interfaces:**
- Consumes: account/ledger/matching types
- Produces: `func Open(path string) (*Store, error)` running spec SQL + River migrate; methods `InsertAccount`, `LatestBook(accountID)`, `InsertPlan`, `GetPlan`, `MarkPlanDone(id, intents)`, `InsertFills`, `InsertSnapshot` — all with an optional `*sql.Tx`. Unique `plan_account_exec_day` on `substr(exec_at,1,10)`.

- [ ] **Step 1: Tests** `TestInsertAccountAndInitSnapshot`, `TestPlanUniquePerExecDay`, `TestMarkPlanDoneWritesActualShares`

- [ ] **Step 2: Run** `go test ./internal/paper/store/ -count=1`
Expected: FAIL

- [ ] **Step 3: Implement store** (SQLite driver: `modernc.org/sqlite` unless River's sqlite helper requires another)

- [ ] **Step 4: Run tests** Expected: PASS

- [ ] **Step 5: Commit** `feat(paper): sqlite store for account plan fill snapshot`

---

### Task 5: River workers

**Files:**
- Create: `internal/paper/jobs/jobs.go` (`GenerateArgs`, `ExecuteArgs` with Kind/InsertOpts unique keys from spec)
- Create: `internal/paper/jobs/workers.go` (generate worker, execute worker, `OpenAccount`)
- Test: `internal/paper/jobs/workers_test.go` using River insert+work in-process

**Interfaces:**
- Consumes: store, matching, `engine.Run`, `panel.Slice`, `Options.Reload`
- Produces: `func OpenAccount(...) (account.Account, error)` one tx: account + init snapshot (date=Start calendar-1 day) + enqueue generate at Start 15:10. `GenerateArgs.Unique` = account_id+signal_date. `ExecuteArgs.Unique` = plan_id.
  - Generate worker: empty SignalDate errors; incomplete data `JobSnooze(1*time.Minute)` (CSV: Discard); Slice to signal date then `engine.Run`; `matching.Generate`; set ExecAt; insert plan; enqueue execute at ExecAt.
  - Execute worker: missing/invalid ExecAt errors; missing bar snooze; `matching.Execute`; one tx: mark done+intents, fills, snapshot, enqueue next generate at fill date 15:10. Already done → no-op.

- [ ] **Step 1: Tests** covering spec §3.7 bullets: open enqueue, open rollback, empty signal_date, snooze incomplete, generate success+execute job, duplicate signal_date, execute missing bar, execute success+next generate, second execute no-op, second plan unique vs open job.

- [ ] **Step 2: Run** `go test ./internal/paper/jobs/ -count=1 -timeout 60s`
Expected: FAIL

- [ ] **Step 3: Implement workers.** Add River module in `go.mod`. Do not call Generate from the execute worker or Execute from generate.

- [ ] **Step 4: Run tests** Expected: PASS

- [ ] **Step 5: Commit** `feat(paper): river generate and execute workers`

---

### Task 6: HTTP and UI

**Files:**
- Modify: `internal/api/server.go`, `internal/api/paper.go`, `internal/api/paper_test.go`, `cmd/server/main.go`
- Modify: `web/src/types.ts`, `web/src/pages/PaperPage.tsx`, `web/src/components/Charts.tsx`, `web/src/api.ts`
- Delete live use of `paper.Replay` / `papers []paper.Account` / `paper.json`

**Interfaces:**
- Consumes: `jobs.OpenAccount`, store reads
- Produces: `POST /api/paper` opens only (202/200 with account, no fills). `GET /api/paper/{id}` returns spec `Paper` (account, book, plans, snapshots as NAV points). Sync endpoints stay but do not call Generate/Execute — they no-op or 204. NAV x-axis = `Snapshot.Date`. Pending = plan status pending, show `ExecAt`. Drop `fill_date`.

- [ ] **Step 1: API tests** `TestCreatePaperEnqueuesGenerateNotReplay`, `TestGetPaperDoesNotWriteFills`, `TestCreatePaperRollbackOnBadStart`

- [ ] **Step 2: Run** `go test ./internal/api/ -count=1 -run Paper`
Expected: FAIL (old Replay assertions)

- [ ] **Step 3: Wire store+River in `cmd/server`; rewrite handlers; update UI; `npm run build` in `web/`**

- [ ] **Step 4: Run** `go test ./internal/api/ ./internal/paper/... -count=1` Expected: PASS. Then `npm run build` in `web/`.

- [ ] **Step 5: Remove `internal/paper/paper.go` Replay if unreferenced. Commit** `feat(paper): serve plans from sqlite instead of replay`

---

## Self-review notes

- Spec §3.1–3.3 workers, unique keys, snooze, actual shares, cash-100, reasons, slippage in buy notional: Tasks 2–5.
- Spec §3.6 page / drop fill_date: Task 6.
- Spec §5 schema: Task 4.
- Review Focus items mapped into Task 5 and Task 3 tests.
