package realtime

import (
	"context"
	"log"
)

type Hub struct {
	clients    map[int64]*Client // map of Clients connected to the Hub. key is the userID
	register   chan *Client      // way to add Clients to the Hub
	unregister chan *Client      // way to remove Clients from the Hub
	unicast    chan UserMessage  // Universal channel to deliver data to any specific user
	store      DataStore         // DB connection
}

// create a new Hub using the specified DB connection
func NewHub(store DataStore) *Hub {
	return &Hub{
		clients:    make(map[int64]*Client),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		unicast:    make(chan UserMessage),
		store:      store,
	}
}

// main loop of the service: notice when clients come and go, and when messages need to be sent
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.handleRegister(client)
		case client := <-h.unregister:
			h.handleUnregister(client)
		case msg := <-h.unicast:
			if msg.UserID != 0 {
				if recipient, isOnline := h.clients[msg.UserID]; isOnline {
					recipient.TrySend(msg.Data)
				}
			} else if msg.Username != "" {
				h.sendToUsernameDirect(msg.Username, msg.Data)
			}
		}
	}
}

func (h *Hub) handleRegister(client *Client) {
	if oldClient, alreadyConnected := h.clients[client.UserID]; alreadyConnected {
		oldClient.Conn.Close()
	}
	h.clients[client.UserID] = client
	friendIDs, err := h.store.GetFriendsList(context.Background(), client.UserID)
	if err != nil {
		log.Println("problem with DB connection")
		return
	}
	onlineFriendUsernames := make([]string, 0)
	presenceMessage, err := EncodeMessage(TypePresenceUpdate, PresenceUpdatePayload{client.Username, true})
	if err != nil {
		log.Println("problem marshalling presence update json")
		return
	}
	for _, friendID := range friendIDs {
		if friend, isOnline := h.clients[friendID]; isOnline {
			onlineFriendUsernames = append(onlineFriendUsernames, friend.Username)
			friend.TrySend(presenceMessage)
		}
	}
	initialMessage, err := EncodeMessage(TypeInitialPresence, InitialPresencePayload{onlineFriendUsernames})
	if err != nil {
		log.Println("problem marshalling initial presence json")
		return
	}
	client.TrySend(initialMessage)
}

func (h *Hub) handleUnregister(client *Client) {
	if _, ok := h.clients[client.UserID]; ok {
		delete(h.clients, client.UserID)
		close(client.Send)

		friendIDs, err := h.store.GetFriendsList(context.Background(), client.UserID)
		if err != nil {
			log.Println("problem with DB connection on unregister:", err)
			return
		}

		// Marshal offline status message
		offlineMessage, err := EncodeMessage(TypePresenceUpdate, PresenceUpdatePayload{client.Username, false})
		if err != nil {
			return
		}

		// Notify all online friends that this user went offline
		for _, friendID := range friendIDs {
			if friend, isOnline := h.clients[friendID]; isOnline {
				friend.TrySend(offlineMessage)
			}
		}
	}
}

func (h *Hub) sendToUsernameDirect(username string, data []byte) {
	for _, client := range h.clients {
		if client.Username == username {
			client.TrySend(data)
			break
		}
	}
}

func (h *Hub) SendToUser(userID int64, data []byte) {
	h.unicast <- UserMessage{UserID: userID, Data: data}
}

func (h *Hub) SendToUsername(username string, data []byte) {
	h.unicast <- UserMessage{Username: username, Data: data}
}
