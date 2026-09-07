package realtime

import (
	"encoding/json"
	"fmt"
	"log/slog"

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
			slog.Debug("WebSocket write pump closed", "username", c.Username, "user_id", c.UserID, "error", err)
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
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Info("WebSocket read pump closed with error", "username", c.Username, "user_id", c.UserID, "error", err)
			} else {
				slog.Debug("WebSocket client disconnected", "username", c.Username, "user_id", c.UserID, "error", err)
			}
			break
		}
		if messageType != websocket.TextMessage {
			continue
		}

		var incoming WebsocketMessage
		if err := json.Unmarshal(p, &incoming); err != nil {
			slog.Warn("WebSocket malformed message envelope", "username", c.Username, "user_id", c.UserID, "raw", string(p), "error", err)
			c.SendError("Malformed message envelope")
			continue
		}

		slog.Debug("WebSocket message received", "type", incoming.Type, "username", c.Username, "user_id", c.UserID)

		if handler, exists := messageRoutes[incoming.Type]; exists {
			if err := handler(c, incoming.Payload); err != nil {
				slog.Warn("WebSocket message handling error", "type", incoming.Type, "username", c.Username, "user_id", c.UserID, "error", err)
				c.SendError(err.Error())
			}
		} else {
			slog.Warn("WebSocket unknown message type", "type", incoming.Type, "username", c.Username, "user_id", c.UserID)
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
	TypeDMSend:         bind((*Client).HandleSendMsg),
}

// helper to send messages without blocking
func (c *Client) TrySend(msg []byte) bool {
	select {
	case c.Send <- msg:
		return true
	default:
		slog.Warn("WebSocket send buffer full, dropped message", "username", c.Username, "user_id", c.UserID)
		return false
	}
}
