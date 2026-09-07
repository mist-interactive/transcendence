package realtime

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-playground/validator/v10"
)

// definitions of message types the websocket handles
type MessageType string

const (
	// server->client presence updates
	TypeInitialPresence MessageType = "initial_presence"
	TypePresenceUpdate  MessageType = "presence_update"

	// Direct messaging protocol
	TypeDMSend MessageType = "direct_message_send"
	TypeDMRecv MessageType = "direct_message_recv"

	// Match invitation protocol
	TypeInviteSend     MessageType = "match_invite_send"
	TypeInviteRecv     MessageType = "match_invite_recv"
	TypeInviteResponse MessageType = "match_invite_response"
	TypeInviteCancel   MessageType = "match_invite_cancel"
	TypeMatchStarted   MessageType = "match_started"

	// Generic error notification
	TypeError MessageType = "error"
)

type ErrorPayload struct {
	Message string `json:"message"`
}

// A unified payload used by all the direct_message_* types.
type DMPayload struct {
	ID        int64      `json:"id,omitempty"`
	Username  string     `json:"username" validate:"required,min=3,max=50"`
	Content   string     `json:"content,omitempty" validate:"omitempty,min=1,max=2000"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
}

type MatchInvitePayload struct {
	Username string `json:"username" validate:"required,min=3,max=50"`
	Status   string `json:"status,omitempty" validate:"omitempty,oneof=pending accepted declined canceled"`
}

type MatchSessionPayload struct {
	MatchID  int64  `json:"match_id"`
	Opponent string `json:"opponent"`
}

type inviteKey struct {
	challenger string
	target     string
}

type MatchActionType int

const (
	ActionInviteSend MatchActionType = iota
	ActionInviteResponse
	ActionInviteCancel
)

type MatchAction struct {
	Type   MatchActionType
	Sender *Client
	Target string
	Status string
}

// UserMessage represents an internal instruction to deliver bytes to a specific user
type UserMessage struct {
	UserID   int64
	Username string
	Data     []byte
}

// WebsocketMessage is the universal envelope for all websocket frames
type WebsocketMessage struct {
	Type    MessageType     `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Presence update payloads
type InitialPresencePayload struct {
	OnlineUsers []string `json:"online_users"`
}

type PresenceUpdatePayload struct {
	Username     string `json:"username"`
	OnlineStatus bool   `json:"online_status"`
}

// WSHandlerFunc is the standard signature for handling an incoming websocket message payload.
type WSHandlerFunc func(*Client, json.RawMessage) error

// bind bridges a strongly-typed handler func(*Client, T) error to the generic WSHandlerFunc signature.
// It automatically unmarshals raw JSON into the target struct T and validates struct tags
// before calling the handler, keeping router definitions declarative and free of repetitive parsing logic.
func bind[T any](fn func(*Client, T) error) WSHandlerFunc {
	return func(c *Client, raw json.RawMessage) error {
		payload, err := UnmarshalAndValidate[T](raw)
		if err != nil {
			return err
		}
		return fn(c, payload)
	}
}

var validate = validator.New()

func UnmarshalAndValidate[T any](raw json.RawMessage) (T, error) {
	var target T
	if err := json.Unmarshal(raw, &target); err != nil {
		return target, fmt.Errorf("malformed JSON payload: %w", err)
	}

	if err := validate.Struct(target); err != nil {
		return target, fmt.Errorf("validation error: %w", err)
	}

	return target, nil
}

// helper to encode a message as a WebsocketMessage struct
func EncodeMessage(msgType MessageType, payload any) ([]byte, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(WebsocketMessage{
		Type:    msgType,
		Payload: payloadBytes,
	})
}
