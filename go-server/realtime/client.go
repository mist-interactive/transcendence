package realtime

import (
	"encoding/json"
	"log"

	"github.com/gorilla/websocket"
)

const sendBufferSize = 256

type Client struct {
	Hub      *Hub
	Conn     *websocket.Conn //handles network traffic
	UserID   int64
	Username string
	Send     chan []byte // Buffered channel of data waiting to be sent to client
}

func NewClient(hub *Hub, conn *websocket.Conn, userID int64, username string) *Client {
	return &Client{
		Hub:      hub,
		Conn:     conn,
		UserID:   userID,
		Username: username,
		Send:     make(chan []byte, sendBufferSize),
	}
}

// listens on the Send channel, pushes messages over websocket when channel gets data
func (c *Client) writePump() {
	defer func() {
		c.Conn.Close()
	}()

	for message := range c.Send {
		err := c.Conn.WriteMessage(websocket.TextMessage, message)
		if err != nil {
			return
		}
	}
}

// reads incoming data from the WebSocket until the user disconnects
func (c *Client) readPump() {
	defer func() {
		c.Hub.unregister <- c
		c.Conn.Close()
	}()

	for {
		messageType, p, err := c.Conn.ReadMessage()
		if err != nil {
			break
		}
		if messageType != websocket.TextMessage {
			continue
		}

		var incoming WebsocketMessage
		if err := json.Unmarshal(p, &incoming); err != nil {
			log.Printf("[WS] Malformed JSON from %s: %v", c.Username, err)
			continue
		}

		switch incoming.Type {
		case TypeInviteSend:
			payload, err := UnmarshalAndValidate[MatchInvitePayload](incoming.Payload)
			if err == nil {
				c.HandleMatchInvite(payload)
			}
		case TypeInviteResponse:
			payload, err := UnmarshalAndValidate[MatchInvitePayload](incoming.Payload)
			if err == nil {
				c.HandleMatchInviteResponse(payload)
			}
		case TypeInviteCancel:
			payload, err := UnmarshalAndValidate[MatchInvitePayload](incoming.Payload)
			if err == nil {
				c.HandleMatchInviteCancel(payload)
			}
		default:
			log.Printf("[WS] Unknown message type: %s", incoming.Type)
		}
	}
}

// helper to send messages without blocking
func (c *Client) TrySend(msg []byte) bool {
	select {
	case c.Send <- msg:
		return true
	default:
		log.Printf("[WS] Send buffer full for %s (id: %d), dropped message", c.Username, c.UserID)
		return false
	}
}
