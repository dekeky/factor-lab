# Daily Parquet Cache Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist full-market Tushare `daily` + `daily_basic` in 5-year parquet shards and make subsequent panel loads read those files.

**Architecture:** New parquet I/O and date-batch fetch in `internal/data`. `TushareSource.Load` syncs missing trading days from 2000, then slices the current index universe out of parquet. Per-API 500/min limiters replace the client-wide QPS cap.

**Tech Stack:** Go 1.26, `github.com/parquet-go/parquet-go` v0.32.1, local `tushare-go-sdk`.

**Spec:** `docs/superpowers/specs/2026-11-22-daily-parquet-design.md`

## Global Constraints

- parquet-go **v0.32.1** only.
- Shard years: 2000-2004, 2005-2009, … path `{DataDir}/parquet/daily/{start}-{end}.parquet`.
- Two trading days per `daily`/`daily_basic` call; `>= 6000` rows or `has_more` → one day per call.
- 500 requests / 60s **per api_name**, independent.
- `daily_basic.close` is not stored; panel `close` stays from `daily`.
- Memory panel remains index-filtered.

## Review Focus

- Two-day window that returns exactly 6000 with `has_more=true` must split, not keep a truncated mix.
- Incremental sync must not rewrite completed historical shards.
- `Load` with empty parquet and a short mocked calendar must not request dates outside the calendar.
- Missing `daily_basic` for a bar leaves those fields as NaN, not zero.
- Windows rename-over-existing for the current shard must not leave a missing file.

---

### Task 1: Shard paths and parquet roundtrip

**Files:**
- Create: `internal/data/parquet.go`
- Test: `internal/data/parquet_test.go`
- Modify: `go.mod` (add `github.com/parquet-go/parquet-go v0.32.1`)

**Interfaces:**
- Produces: `DailyParquetRow`, `func shardRange(year int) (start, end int)`, `func shardPath(dir string, year int) string`, `func WriteDailyShard(path string, rows []DailyParquetRow) error`, `func ReadDailyShard(path string) ([]DailyParquetRow, error)`

- [ ] **Step 1: Write the failing test**

`TestShardRangeAlignsFrom2000`: 2000→(2000,2004), 2004→(2000,2004), 2005→(2005,2009), 2026→(2025,2029).

`TestWriteReadDailyShardRoundtrip`: write two rows (one with daily_basic NaNs) to a temp file via `WriteDailyShard`, read back, equal including NaN.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/data -run "TestShardRangeAlignsFrom2000|TestWriteReadDailyShardRoundtrip" -count=1`

Expected: FAIL, undefined names.

- [ ] **Step 3: Implement schema + I/O in `internal/data/parquet.go`**

`DailyParquetRow` fields: `ts_code`, `trade_date` (YYYYMMDD), daily OHLC/`pre_close`/`vol`/`amount`, then `DailyBasicCols`. Use parquet-go `GenericWriter`/`GenericReader`. Atomic write: `path+".tmp"` then rename.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/data -run "TestShardRangeAlignsFrom2000|TestWriteReadDailyShardRoundtrip" -count=1`

Expected: PASS

---

### Task 2: Two-day batches and 6000 fallback

**Files:**
- Create: `internal/data/daily_fetch.go`
- Test: `internal/data/daily_fetch_test.go`

**Interfaces:**
- Consumes: none
- Produces: `func batchTradeDates(dates []string) [][]string` (pairs, last may be 1), `const dailyRowLimit = 6000`, `func shouldSplitBatch(n int, hasMore bool, days int) bool`

- [ ] **Step 1: Write the failing test**

`TestBatchTradeDatesPairs`: `["d1","d2","d3"]` → `[["d1","d2"],["d3"]]`.

`TestShouldSplitBatch`: 6001 rows → true; 6000 + hasMore → true; 6000 + two days + !hasMore → true; 100 rows two days → false; 6000 one day + hasMore → false (paginate instead of split).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/data -run "TestBatchTradeDatesPairs|TestShouldSplitBatch" -count=1`

Expected: FAIL

- [ ] **Step 3: Implement `batchTradeDates` and `shouldSplitBatch` in `internal/data/daily_fetch.go`**

`shouldSplitBatch(n, hasMore, days)`: if `days <= 1` return false; return `n >= dailyRowLimit || hasMore`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/data -run "TestBatchTradeDatesPairs|TestShouldSplitBatch" -count=1`

Expected: PASS

---

### Task 3: Per-API 500/min limiter

**Files:**
- Modify: `third_party/tushare-go-sdk/ratelimit.go`
- Modify: `third_party/tushare-go-sdk/client.go`
- Modify: `third_party/tushare-go-sdk/options.go`
- Test: `third_party/tushare-go-sdk/ratelimit_test.go`

**Interfaces:**
- Produces: `WithAPIRateLimit(perMin float64)` on Client; `query` waits on the limiter for that `apiName`. Global `WithRateLimit` remains but is unused by factor-lab.

- [ ] **Step 1: Write the failing test**

`TestAPIRateLimitIndependent`: two APIs, perMin=2, burst via sliding window 50ms in test by injecting a tiny window **or** use perMin=2 with a 200ms window via a test helper `newAPILimiter(limit, window)`. After 2 waits on `"daily"`, the 3rd `"daily"` wait with 5ms ctx times out; `"daily_basic"` wait succeeds.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./third_party/tushare-go-sdk -run TestAPIRateLimitIndependent -count=1`

Expected: FAIL

- [ ] **Step 3: Implement per-api sliding window in the SDK**

`Client.apiRPM`, `Client.apiLim map[string]*windowLimiter`. `windowLimiter` keeps timestamps, drops those older than 1 minute (or configured window in tests). `query` calls `waitAPI(ctx, apiName)` instead of only the global limiter. If both global and per-API set, per-API applies (factor-lab will not set global).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./third_party/tushare-go-sdk -run TestAPIRateLimitIndependent -count=1`

Expected: PASS

---

### Task 4: Sync full-market daily+basic into shards

**Files:**
- Modify: `internal/data/daily_fetch.go`
- Modify: `internal/data/parquet.go` (merge/present dates)
- Test: `internal/data/daily_sync_test.go`

**Interfaces:**
- Consumes: Task 1 I/O, Task 2 batching, Task 3 client limiter
- Produces: `func (s *TushareSource) syncDailyParquet(ctx context.Context, cli *tushare.Client) error`, `func joinDailyBasic(bars, basic []map[string]any) []DailyParquetRow`, `func parquetDates(dir, from, to string) (map[string]bool, error)`

- [ ] **Step 1: Write the failing tests**

`TestJoinDailyBasicKeepsDailyClose`: bar close=10, basic close=99, turnover=1.5 → row.Close=10, TurnoverRate=1.5.

`TestSyncDailyParquetTwoDayThenSplit`: mock `trade_cal` three days; mock `daily` for a two-day window returning 6001 rows; assert subsequent calls use single `trade_date`; files exist under temp DataDir for the 2025-2029 shard; second `syncDailyParquet` issues no `daily` calls.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/data -run "TestJoinDailyBasicKeepsDailyClose|TestSyncDailyParquetTwoDayThenSplit" -count=1`

Expected: FAIL

- [ ] **Step 3: Implement join + sync**

`syncDailyParquet`: trade_cal SSE 20000101..End-or-today; skip dates in parquet; batch remaining; for each batch fetch daily and daily_basic in parallel (errgroup); split if `shouldSplitBatch`; join; append to current-shard buffer; on shard change or end, `WriteDailyShard` merged with existing file. `TushareSource.DataDir` required (tests: `t.TempDir()`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/data -run "TestJoinDailyBasicKeepsDailyClose|TestSyncDailyParquetTwoDayThenSplit" -count=1`

Expected: PASS

---

### Task 5: Load panel from parquet

**Files:**
- Modify: `internal/data/tushare.go` (`Load`, `client`)
- Modify: `internal/data/tushare_test.go` (DataDir + trade_cal mock)
- Modify: `cmd/server/main.go` (DataDir, RPM, load timeout)
- Test: existing `internal/data/tushare_test.go` plus `TestLoadReadsDailyFromParquetNotByCode`

**Interfaces:**
- Consumes: `syncDailyParquet`, `ReadDailyShard`
- Produces: `Load` uses parquet rows as `BarRows`/`FactorRows`; `queryByCode` no longer called for `daily`/`daily_basic`.

- [ ] **Step 1: Write the failing test**

`TestLoadReadsDailyFromParquetNotByCode`: mock records `daily` params; after Load, every `daily` call has empty `ts_code` and uses `start_date`/`end_date` or `trade_date`; panel close/turnover still match sample bars. Seed DataDir as temp dir.

- [ ] **Step 2: Run test to verify it fails** (current Load queries by ts_code)

Run: `go test ./internal/data -run TestLoadReadsDailyFromParquetNotByCode -count=1`

Expected: FAIL (ts_code present)

- [ ] **Step 3: Switch `Load` to sync+read parquet; add `DataDir`; `client()` uses `WithAPIRateLimit(500)` (or `s.RPM` default 500), no global QPS. Update `defaultHandle` with `trade_cal`. Set `DataDir: t.TempDir()` in existing Load tests. `main.go`: DataDir from store dir, `-tushare-rpm` default 500, load timeout 3h.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/data ./cmd/server ./third_party/tushare-go-sdk -count=1`

Expected: PASS
