package realtime

import (
	"log/slog"
	"net/http"

	"github.com/gorilla/websocket"
)

type TokenValidator func(tokenStr string) (int64, string, error)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for now, close down later
	},
}

// handles listening to incoming WS requests, upgrades connection to WS, and adds to Hub connection map
func (h *Hub) ServeWS(validator TokenValidator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract & validate JWT from query string
		tokenStr := r.URL.Query().Get("token")
		if tokenStr == "" {
			slog.Warn("WebSocket handshake rejected: missing token query param", "remote_ip", r.RemoteAddr)
			http.Error(w, "Missing token query parameter", http.StatusUnauthorized)
			return
		}
		userID, username, err := validator(tokenStr)
		if err != nil {
			slog.Warn("WebSocket handshake rejected: invalid token", "remote_ip", r.RemoteAddr, "error", err)
			http.Error(w, "Unauthorized: Invalid or expired token", http.StatusUnauthorized)
			return
		}

		// upgrade HTTP connection to persistent WebSocket
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("WebSocket upgrade failed", "remote_ip", r.RemoteAddr, "user_id", userID, "username", username, "error", err)
			return
		}

		slog.Info("WebSocket connection established", "user_id", userID, "username", username, "remote_ip", r.RemoteAddr)

		client := NewClient(h, conn, userID, username)
		h.register <- client

		// start the read and write pumps in background goroutines
		go client.writePump()
		go client.readPump()
	}
}
