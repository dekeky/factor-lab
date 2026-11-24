PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;

CREATE TABLE IF NOT EXISTS account (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    name                TEXT    NOT NULL,
    factor_id           INTEGER NOT NULL DEFAULT 0,
    factor_name         TEXT    NOT NULL DEFAULT '',
    combo_id            INTEGER NOT NULL DEFAULT 0,
    combo_name          TEXT    NOT NULL DEFAULT '',
    expr                TEXT    NOT NULL,
    top_n               INTEGER NOT NULL,
    rebalance_threshold INTEGER NOT NULL DEFAULT 0,
    capital             REAL    NOT NULL,
    cost                REAL    NOT NULL,
    slippage_bps        REAL    NOT NULL DEFAULT 0,
    start               TEXT    NOT NULL DEFAULT '',
    created_at          TEXT    NOT NULL,
    updated_at          TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS plan (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id  INTEGER NOT NULL REFERENCES account(id) ON DELETE CASCADE,
    signal_date TEXT    NOT NULL,
    exec_at     TEXT    NOT NULL,
    intents     TEXT    NOT NULL,
    picks       TEXT    NOT NULL DEFAULT '[]',
    status      TEXT    NOT NULL CHECK (status IN ('pending','done'))
);

CREATE UNIQUE INDEX IF NOT EXISTS plan_account_exec_day
    ON plan (account_id, substr(exec_at, 1, 10));

CREATE TABLE IF NOT EXISTS fill (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id     INTEGER NOT NULL REFERENCES account(id) ON DELETE CASCADE,
    plan_id        INTEGER REFERENCES plan(id) ON DELETE SET NULL,
    date           TEXT    NOT NULL,
    code           TEXT    NOT NULL,
    side           TEXT    NOT NULL,
    shares         REAL    NOT NULL,
    price          REAL    NOT NULL,
    amount         REAL    NOT NULL,
    fee            REAL    NOT NULL,
    slippage       REAL    NOT NULL DEFAULT 0,
    status         TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS fill_account_date ON fill (account_id, date);
CREATE INDEX IF NOT EXISTS fill_plan ON fill (plan_id);

CREATE TABLE IF NOT EXISTS snapshot (
    account_id INTEGER NOT NULL REFERENCES account(id) ON DELETE CASCADE,
    date       TEXT    NOT NULL,
    cash       REAL    NOT NULL,
    nav        REAL    NOT NULL,
    ret        REAL    NOT NULL,
    holdings   TEXT    NOT NULL,
    PRIMARY KEY (account_id, date)
);
