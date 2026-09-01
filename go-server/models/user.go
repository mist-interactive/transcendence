package models

import (
	"time"

	"github.com/uptrace/bun"
)

type User struct {
	bun.BaseModel `bun:"table:users"`

	ID        int64     `bun:"id,pk,autoincrement" json:"id"`
	Username  string    `bun:"username,type:varchar(50),unique,notnull" json:"username"`
	PWHash    string    `bun:"password_hash,type:varchar(255),notnull" json:"-"`
	Email     string    `bun:"email,type:varchar(255),unique,notnull" json:"email"`
	Bio       string    `bun:"bio" json:"bio"`
	AvatarURL *string   `bun:"avatar_url" json:"avatarUrl"` //pointer so it can be null
	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updated_at"`
}

type RegisterRequest struct {
	Username string `json:"username" validate:"required,min=3,max=50,username_safety"`
	Password string `json:"password" validate:"required,min=8,max=72,password_complexity"`
	Email    string `json:"email" validate:"required,email,max=255"`
}

type LoginRequest struct {
	Username string `json:"username" validate:"required,min=3,max=50"`
	Password string `json:"password" validate:"required,min=8,max=72"`
}

type ProfilePatchInput struct {
	Bio       *string `json:"bio" validate:"omitempty,max=500"`
	AvatarURL *string `json:"avatarUrl" validate:"omitempty,url,max=2048"`
	Email     *string `json:"email" validate:"omitempty,email,max=255"`
}

type UserProfile struct {
	Username  string  `json:"username"`
	Email     *string `json:"email,omitempty"`
	Bio       string  `json:"bio"`
	AvatarURL *string `json:"avatarUrl"`
}
