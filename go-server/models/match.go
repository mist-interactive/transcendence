package models

import (
	"time"

	"github.com/uptrace/bun"
)

type MatchStatus string
type MatchResult string

const (
	StatusInProgress MatchStatus = "in_progress"
	StatusFinished   MatchStatus = "finished"
	StatusAbandoned  MatchStatus = "abandoned"

	ResultPlayer1Win MatchResult = "player1_win"
	ResultPlayer2Win MatchResult = "player2_win"
	ResultDraw       MatchResult = "draw"
	ResultAborted    MatchResult = "aborted"
)

type MatchRecord struct {
	bun.BaseModel `bun:"table:matches"`

	ID         int64        `json:"id" bun:"id,pk,autoincrement"`
	Player1    int64        `json:"player_one" bun:"player_one,notnull"`
	Player2    int64        `json:"player_two" bun:"player_two,notnull"`
	Status     MatchStatus  `json:"status" bun:"status,notnull"`
	Result     *MatchResult `json:"result" bun:"result"`
	StartedAt  time.Time    `json:"started_at" bun:"started_at,default:current_timestamp"`
	FinishedAt *time.Time   `json:"finished_at" bun:"finished_at"`
}

type MatchCreateInput struct {
	Player1 int64 `json:"player_one" validate:"required"`
	Player2 int64 `json:"player_two" validate:"required"`
}

type MatchPatchInput struct {
	Result string `json:"result" validate:"required,oneof=player1_win player2_win draw aborted"`
	Status string `json:"status" validate:"required,oneof=finished abandoned"`
}
