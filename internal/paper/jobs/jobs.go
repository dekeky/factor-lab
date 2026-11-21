package jobs

import (
	"time"

	"github.com/riverqueue/river"
)

const QueueName = "paper"

// GenerateArgs 出计划：用 SignalDate 收盘打分，下一交易日 09:30 成交。
type GenerateArgs struct {
	AccountID  int    `json:"account_id" river:"unique"`
	SignalDate string `json:"signal_date" river:"unique"`
}

func (GenerateArgs) Kind() string { return "paper.generate" }

func (GenerateArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: QueueName,
		UniqueOpts: river.UniqueOpts{
			ByArgs: true,
		},
	}
}

// ExecuteArgs 成交某一份计划。成交时刻只看 Plan.ExecAt。
type ExecuteArgs struct {
	AccountID int   `json:"account_id"`
	PlanID    int64 `json:"plan_id" river:"unique"`
}

func (ExecuteArgs) Kind() string { return "paper.execute" }

func (ExecuteArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: QueueName,
		UniqueOpts: river.UniqueOpts{
			ByArgs: true,
		},
	}
}

const snoozeIncomplete = time.Minute
