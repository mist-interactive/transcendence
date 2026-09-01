package models

import (
	"time"

	"github.com/uptrace/bun"
)

type Message struct {
	bun.BaseModel `bun:"table:messages"`

	ID          int64     `json:"id" bun:"id,pk,autoincrement"`
	SenderID    int64     `json:"sender_id" bun:"sender_id,notnull"`
	RecipientID int64     `json:"recipient_id" bun:"recipient_id,notnull"`
	Content     string    `json:"content" bun:"content,notnull" validate:"required,max=2000"`
	IsRead      bool      `json:"is_read" bun:"is_read,notnull,default:false"`
	CreatedAt   time.Time `json:"created_at" bun:"created_at,default:current_timestamp"`
}

type MessageCreateInput struct {
	SenderID    int64  `json:"sender_id" validate:"required"`
	RecipientID int64  `json:"recipient_id" validate:"required"`
	Content     string `json:"content" validate:"required,max=2000"`
}

type MessageSetReadInput struct {
	ReadUpTo int64 `json:"read_up_to" validate:"required"`
}
