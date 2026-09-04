package realtime

import (
	"encoding/json"
	"fmt"
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
			c.SendError("Malformed message envelope")
			continue
		}

		if handler, exists := messageRoutes[incoming.Type]; exists {
			if err := handler(c, incoming.Payload); err != nil {
				log.Printf("[WS] Error handling %s from %s: %v", incoming.Type, c.Username, err)
				c.SendError(err.Error())
			}
		} else {
			log.Printf("[WS] Unknown message type: %s", incoming.Type)
			c.SendError(fmt.Sprintf("Unknown message type: %s", incoming.Type))
		}
	}
}

// SendError routes a generic error message through the Hub's safe unicast channel.
func (c *Client) SendError(msg string) {
	errBytes, err := EncodeMessage(TypeError, ErrorPayload{Message: msg})
	if err == nil {
		c.Hub.SendToUser(c.UserID, errBytes)
	}
}

// messageRoutes maps incoming WebSocket message types to their corresponding handler functions.
// Handlers are wrapped with bind[T] to automatically parse and validate the JSON payload.
var messageRoutes = map[MessageType]WSHandlerFunc{
	TypeInviteSend:     bind((*Client).HandleMatchInvite),
	TypeInviteResponse: bind((*Client).HandleMatchInviteResponse),
	TypeInviteCancel:   bind((*Client).HandleMatchInviteCancel),
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
