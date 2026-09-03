package realtime

import (
	"encoding/json"
	"fmt"

	"github.com/go-playground/validator/v10"
)

// definitions of message types the websocket handles
type MessageType string

const (
	// server->client presence updates
	TypeInitialPresence MessageType = "initial_presence"
	TypePresenceUpdate  MessageType = "presence_update"
)

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

type InitialPresencePayload struct {
	OnlineUsers []string `json:"online_users"`
}

type PresenceUpdatePayload struct {
	Username     string `json:"username"`
	OnlineStatus bool   `json:"online_status"`
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

// EncodeMessage encodes a typed payload into a WebsocketMessage JSON frame
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
