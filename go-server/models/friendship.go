package models

import (
	"time"

	"github.com/uptrace/bun"
)

type FriendshipStatus string

// these are the allowed options for status column
const (
	StatusPending  FriendshipStatus = "pending"
	StatusAccepted FriendshipStatus = "accepted"
	StatusBlocked  FriendshipStatus = "blocked"
)

type Friendship struct {
	bun.BaseModel `bun:"table:friendships"`
	ID            int64            `json:"friendship_id" bun:"id,pk,autoincrement"`
	UserID        int64            `json:"user_id" bun:"user_id,notnull"`
	FriendID      int64            `json:"friend_id" bun:"friend_id,notnull"`
	Status        FriendshipStatus `json:"status" bun:"status,notnull"`
	CreatedAt     time.Time        `json:"created_at" bun:"created_at,default:current_timestamp"`
	UpdatedAt     time.Time        `json:"updated_at" bun:"updated_at,default:current_timestamp"`
}

type FriendRequest struct {
	Target string `json:"target" validate:"required,min=3,max=50"`
}

type FriendRequestAnswer struct {
	Status FriendshipStatus `json:"status" validate:"required,oneof=accepted blocked"`
}

type FriendshipItemResponse struct {
	FriendshipID int64            `json:"friendship_id"`
	UserID       int64            `json:"user_id"`
	Username     string           `json:"username"`
	AvatarURL    *string          `json:"avatar_url"`
	Status       FriendshipStatus `json:"status"`
	IsIncoming   bool             `json:"is_incoming"`
	UnreadCount  int64            `json:"unread_count"`
}
