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
	clients      map[int64]*Client  // map of Clients connected to the Hub. key is the userID
	register     chan *Client       // way to add Clients to the Hub
	unregister   chan *Client       // way to remove Clients from the Hub
	unicast      chan UserMessage   // Universal channel to deliver data to any specific user
	presenceSync chan PresenceSync  // Channel to update a users friends that they came online
	invites      map[inviteKey]bool // In-memory pending challenges
	matchAction  chan MatchAction   // Match invitation events dispatched to the Hub
	store        DataStore          // DB connection
}

// create a new Hub using the specified DB connection
func NewHub(store DataStore) *Hub {
	return &Hub{
		clients:      make(map[int64]*Client),
		register:     make(chan *Client),
		unregister:   make(chan *Client),
		unicast:      make(chan UserMessage),
		presenceSync: make(chan PresenceSync),
		invites:      make(map[inviteKey]bool),
		matchAction:  make(chan MatchAction),
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
		case action := <-h.matchAction:
			h.handleMatchAction(action)
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
		for key := range h.invites {
			if key.challenger == client.Username || key.target == client.Username {
				delete(h.invites, key)
			}
		}
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

// handleMatchAction serves as a thin dispatcher on the Hub's single-threaded event loop.
// By running these actions sequentially in Hub.Run(), access to h.invites is completely lock-free.
func (h *Hub) handleMatchAction(action MatchAction) {
	switch action.Type {
	case ActionInviteSend:
		h.onInviteSend(action.Sender, action.Target)
	case ActionInviteResponse:
		h.onInviteResponse(action.Sender, action.Target, action.Status)
	case ActionInviteCancel:
		h.onInviteCancel(action.Sender, action.Target)
	}
}

// onInviteSend validates an outgoing challenge, registers it in h.invites, and delivers
// a "match_invite_recv" notification to the target player if they are currently connected.
func (h *Hub) onInviteSend(sender *Client, target string) {
	if sender.Username == target {
		return
	}
	h.invites[inviteKey{challenger: sender.Username, target: target}] = true
	inviteBytes, err := EncodeMessage(TypeInviteRecv, MatchInvitePayload{
		Username: sender.Username,
		Status:   "pending",
	})
	if err == nil {
		h.sendToUsernameDirect(target, inviteBytes)
	}
}

// onInviteResponse handles an accept or decline from the target player.
// It verifies that a challenge is actively pending in h.invites (anti-spoof protection).
// If accepted, it deletes the invite and launches createAndStartMatch in a separate goroutine.
// If declined, it deletes the invite and forwards the decline to the challenger.
func (h *Hub) onInviteResponse(sender *Client, challenger, status string) {
	key := inviteKey{challenger: challenger, target: sender.Username}
	if !h.invites[key] {
		log.Printf("[WS] %s tried to respond to non-existent invite from %s", sender.Username, challenger)
		return
	}
	delete(h.invites, key)

	switch status {
	case "accepted":
		// Run DB match creation in background so the Hub event loop never blocks on DB I/O
		go h.createAndStartMatch(challenger, sender.Username)
	case "declined":
		declineBytes, err := EncodeMessage(TypeInviteResponse, MatchInvitePayload{
			Username: sender.Username,
			Status:   "declined",
		})
		if err == nil {
			h.sendToUsernameDirect(challenger, declineBytes)
		}
	}
}

// onInviteCancel deletes a pending challenge from h.invites and sends "match_invite_cancel"
// to the target player to dismiss the challenge prompt on their client.
func (h *Hub) onInviteCancel(sender *Client, target string) {
	key := inviteKey{challenger: sender.Username, target: target}
	if h.invites[key] {
		delete(h.invites, key)
		cancelBytes, err := EncodeMessage(TypeInviteCancel, MatchInvitePayload{
			Username: sender.Username,
			Status:   "canceled",
		})
		if err == nil {
			h.sendToUsernameDirect(target, cancelBytes)
		}
	}
}

// createAndStartMatch runs asynchronously in a worker goroutine to call the internal REST API
// and insert a match record in PostgreSQL without blocking the Hub's main event loop.
// Once the match ID is returned, it safely delivers "match_started" messages to both players.
func (h *Hub) createAndStartMatch(challenger, responder string) {
	matchID, err := h.store.CreateMatch(context.Background(), challenger, responder)
	if err != nil {
		log.Printf("[WS] Failed to create match between %s and %s: %v", challenger, responder, err)
		return
	}

	challengerMsg, err := EncodeMessage(TypeMatchStarted, MatchSessionPayload{
		MatchID:  matchID,
		Opponent: responder,
	})
	if err == nil {
		h.SendToUsername(challenger, challengerMsg)
	}

	responderMsg, err := EncodeMessage(TypeMatchStarted, MatchSessionPayload{
		MatchID:  matchID,
		Opponent: challenger,
	})
	if err == nil {
		h.SendToUsername(responder, responderMsg)
	}
}

func (h *Hub) SendToUser(userID int64, data []byte) {
	h.unicast <- UserMessage{UserID: userID, Data: data}
}

func (h *Hub) SendToUsername(username string, data []byte) {
	h.unicast <- UserMessage{Username: username, Data: data}
}
