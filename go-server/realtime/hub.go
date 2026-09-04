package realtime

import (
	"context"
	"log"
	"time"
)

// used to communicate a client joining, needed so only the main thread needs to access the clients struct
type PresenceSync struct {
	client    *Client
	friendIDs []int64
}

type Hub struct {
	clients      map[int64]*Client // map of Clients connected to the Hub. key is the userID
	register     chan *Client      // way to add Clients to the Hub
	unregister   chan *Client      // way to remove Clients from the Hub
	unicast      chan UserMessage  // Universal channel to deliver data to any specific user
	presenceSync chan PresenceSync // Channel to update a users friends that they came online
	store        DataStore         // DB connection
}

// create a new Hub using the specified DB connection
func NewHub(store DataStore) *Hub {
	return &Hub{
		clients:      make(map[int64]*Client),
		register:     make(chan *Client),
		unregister:   make(chan *Client),
		unicast:      make(chan UserMessage),
		presenceSync: make(chan PresenceSync),
		store:        store,
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
		case sync := <-h.presenceSync:
			h.handlePresenceSync(sync.client, sync.friendIDs)
		}
	}
}

func (h *Hub) handleRegister(client *Client) {
	if oldClient, alreadyConnected := h.clients[client.UserID]; alreadyConnected {
		oldClient.Conn.Close()
	}
	h.clients[client.UserID] = client
	go h.sendInitialPresence(client) //run the possibly slow DB and messaging in it's own thread
}

func (h *Hub) handleUnregister(client *Client) {
	if currentClient, ok := h.clients[client.UserID]; ok && currentClient == client { //check that this isn't an old instance of client being cleaned up, when a new connection was registered
		delete(h.clients, client.UserID)
		close(client.Send)
		go h.broadcastOfflineStatus(client.UserID, client.Username) //run the possibly slow DB and messaging in it's own thread
	}
}

// Contain DB access and messaging to it's own function, so it can be run in a thread
func (h *Hub) broadcastOfflineStatus(userID int64, username string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	friendIDs, err := h.store.GetFriendsList(ctx, userID)
	if err != nil {
		log.Println("problem with DB connection on unregister:", err)
		return
	}

	// Marshal offline status message
	offlineMessage, err := EncodeMessage(TypePresenceUpdate, PresenceUpdatePayload{username, false})
	if err != nil {
		return
	}

	// Notify all online friends that this user went offline
	for _, friendID := range friendIDs {
		h.SendToUser(friendID, offlineMessage)
	}
}

func (h *Hub) sendInitialPresence(client *Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	friendIDs, err := h.store.GetFriendsList(ctx, client.UserID)
	if err != nil {
		log.Println("problem with DB connection")
		return
	}
	h.presenceSync <- PresenceSync{client: client, friendIDs: friendIDs} //sending presence update requires accessing the clients map, only the main thread is allowed to do that, so put it on a channel for it to read
}

func (h *Hub) handlePresenceSync(client *Client, friendIDs []int64) {
	if currentClient, ok := h.clients[client.UserID]; !ok || currentClient != client { //only run if this is still an active connection on the Hub
		return
	}
	onlineFriendUsernames := make([]string, 0)
	presenceMessage, _ := EncodeMessage(TypePresenceUpdate, PresenceUpdatePayload{client.Username, true})

	for _, friendID := range friendIDs {
		if friend, isOnline := h.clients[friendID]; isOnline {
			onlineFriendUsernames = append(onlineFriendUsernames, friend.Username)
			friend.TrySend(presenceMessage)
		}
	}
	initialMessage, _ := EncodeMessage(TypeInitialPresence, InitialPresencePayload{onlineFriendUsernames})
	client.TrySend(initialMessage)
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
