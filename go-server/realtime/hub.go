package realtime

import (
	"context"
	"dbBackend/models"
	"encoding/json"
	"fmt"
	"log/slog"
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
		unicast:      make(chan UserMessage, 256),
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
		slog.Info("Disconnecting previous connection for user", "user_id", client.UserID, "username", client.Username)
		oldClient.Conn.Close()
	}
	h.clients[client.UserID] = client
	slog.Info("Client registered in hub", "user_id", client.UserID, "username", client.Username, "total_clients", len(h.clients))
	go h.sendInitialPresence(client) //run the possibly slow DB and messaging in it's own thread
}

func (h *Hub) handleUnregister(client *Client) {
	if currentClient, ok := h.clients[client.UserID]; ok && currentClient == client { //check that this isn't an old instance of client being cleaned up, when a new connection was registered
		delete(h.clients, client.UserID)
		close(client.Send)
		cleanedInvites := h.cleanUpInvites(client.Username)
		slog.Info("Client unregistered from hub",
			"user_id", client.UserID,
			"username", client.Username,
			"total_clients", len(h.clients),
			"cleaned_invites", cleanedInvites,
		)
		go h.broadcastOfflineStatus(client.UserID, client.Username) //run the possibly slow DB and messaging in it's own thread
	}
}

// cleanUpInvites cancels all pending invites involving the disconnected user and notifies the other party.
func (h *Hub) cleanUpInvites(username string) int {
	cleaned := 0
	for key := range h.invites {
		if key.challenger == username {
			delete(h.invites, key)
			cleaned++
			cancelBytes, err := EncodeMessage(TypeInviteCancel, MatchInvitePayload{
				Username: username,
				Status:   "canceled",
			})
			if err == nil {
				h.sendToUsernameDirect(key.target, cancelBytes)
			}
			slog.Info("Canceled pending invite: challenger disconnected",
				"challenger", username,
				"target", key.target,
			)
		} else if key.target == username {
			delete(h.invites, key)
			cleaned++
			cancelBytes, err := EncodeMessage(TypeInviteCancel, MatchInvitePayload{
				Username: username,
				Status:   "canceled",
			})
			if err == nil {
				h.sendToUsernameDirect(key.challenger, cancelBytes)
			}
			slog.Info("Canceled pending invite: target disconnected",
				"challenger", key.challenger,
				"target", username,
			)
		}
	}
	return cleaned
}

// Contain DB access and messaging to it's own function, so it can be run in a thread
func (h *Hub) broadcastOfflineStatus(userID int64, username string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	friendIDs, err := h.store.GetFriendsList(ctx, userID)
	if err != nil {
		slog.Error("Problem with DB connection on unregister", "user_id", userID, "username", username, "error", err)
		return
	}

	// Marshal offline status message
	offlineMessage, err := EncodeMessage(TypePresenceUpdate, PresenceUpdatePayload{username, false})
	if err != nil {
		slog.Error("Failed to encode offline presence update", "user_id", userID, "username", username, "error", err)
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
		slog.Error("Problem with DB connection on initial presence", "user_id", client.UserID, "username", client.Username, "error", err)
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
	slog.Debug("Dispatched presence sync", "user_id", client.UserID, "username", client.Username, "online_friends", len(onlineFriendUsernames))
}

func (h *Hub) findClientByUsername(username string) *Client {
	for _, client := range h.clients {
		if client.Username == username {
			return client
		}
	}
	return nil
}

func (h *Hub) sendToUsernameDirect(username string, data []byte) bool {
	if client := h.findClientByUsername(username); client != nil {
		return client.TrySend(data)
	}
	return false
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
		slog.Warn("Match invite rejected: self challenge", "challenger", sender.Username)
		sender.SendError("Cannot invite yourself to a match")
		return
	}

	targetClient := h.findClientByUsername(target)
	if targetClient == nil {
		slog.Warn("Match invite rejected: target user is offline", "challenger", sender.Username, "target", target)
		sender.SendError(fmt.Sprintf("User '%s' is not online", target))
		return
	}

	key := inviteKey{challenger: sender.Username, target: target}
	alreadyPending := h.invites[key]

	if alreadyPending {
		slog.Warn("Duplicate match invite sent while already pending",
			"challenger", sender.Username,
			"target", target,
			"total_pending_invites", len(h.invites),
		)
	} else {
		slog.Info("Match invite sent",
			"challenger", sender.Username,
			"target", target,
			"total_pending_invites", len(h.invites)+1,
		)
	}

	h.invites[key] = true
	inviteBytes, err := EncodeMessage(TypeInviteRecv, MatchInvitePayload{
		Username: sender.Username,
		Status:   "pending",
	})
	if err == nil {
		targetClient.TrySend(inviteBytes)
	}
}

// onInviteResponse handles an accept or decline from the target player.
// It verifies that a challenge is actively pending in h.invites (anti-spoof protection).
// If accepted, it deletes the invite and launches createAndStartMatch in a separate goroutine.
// If declined, it deletes the invite and forwards the decline to the challenger.
func (h *Hub) onInviteResponse(sender *Client, challenger, status string) {
	key := inviteKey{challenger: challenger, target: sender.Username}
	if !h.invites[key] {
		slog.Warn("Match invite response rejected: invite not found or expired",
			"responder", sender.Username,
			"challenger", challenger,
			"status", status,
			"total_pending_invites", len(h.invites),
		)
		sender.SendError("Invite not found or has expired")
		return
	}
	delete(h.invites, key)

	slog.Info("Match invite response processed",
		"responder", sender.Username,
		"challenger", challenger,
		"status", status,
		"remaining_pending_invites", len(h.invites),
	)

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
			if !h.sendToUsernameDirect(challenger, declineBytes) {
				slog.Warn("Declined invite response could not be delivered to challenger (offline)",
					"challenger", challenger,
					"responder", sender.Username,
				)
			}
		}
	}
}

// onInviteCancel deletes a pending challenge from h.invites and sends "match_invite_cancel"
// to the target player to dismiss the challenge prompt on their client.
func (h *Hub) onInviteCancel(sender *Client, target string) {
	key := inviteKey{challenger: sender.Username, target: target}
	if !h.invites[key] {
		slog.Warn("Match invite cancel rejected: invite not found or expired",
			"challenger", sender.Username,
			"target", target,
			"total_pending_invites", len(h.invites),
		)
		return
	}
	delete(h.invites, key)
	slog.Info("Match invite canceled",
		"challenger", sender.Username,
		"target", target,
		"remaining_pending_invites", len(h.invites),
	)
	cancelBytes, err := EncodeMessage(TypeInviteCancel, MatchInvitePayload{
		Username: sender.Username,
		Status:   "canceled",
	})
	if err == nil {
		if !h.sendToUsernameDirect(target, cancelBytes) {
			slog.Debug("Cancel invite notification not delivered to target (offline)",
				"challenger", sender.Username,
				"target", target,
			)
		}
	}
}

// createAndStartMatch runs asynchronously in a worker goroutine to call the internal REST API
// and insert a match record in PostgreSQL without blocking the Hub's main event loop.
// Once the match ID is returned, it safely delivers "match_started" messages to both players.
func (h *Hub) createAndStartMatch(challenger, responder string) {
	slog.Info("Initiating match creation in database", "challenger", challenger, "responder", responder)
	matchID, err := h.store.CreateMatch(context.Background(), challenger, responder)
	if err != nil {
		slog.Error("Failed to create match between players in DB",
			"challenger", challenger,
			"responder", responder,
			"error", err,
		)
		errMsg, errEnc := EncodeMessage(TypeError, ErrorPayload{Message: "Failed to initialize match in database"})
		if errEnc == nil {
			h.SendToUsername(challenger, errMsg)
			h.SendToUsername(responder, errMsg)
		}
		return
	}

	slog.Info("Match created successfully, notifying players",
		"match_id", matchID,
		"challenger", challenger,
		"responder", responder,
	)

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

// NotifyUser delivers an event payload to a connected user via the thread-safe unicast channel.
// It accepts []byte, string, or any struct (which is marshaled to JSON).
// It is safe for concurrent use by HTTP handler goroutines and does not block if the buffer is full.
func (h *Hub) NotifyUser(userID int64, event any) error {
	var data []byte
	var err error

	switch v := event.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		data, err = json.Marshal(event)
		if err != nil {
			return fmt.Errorf("failed to marshal notification for user %d: %w", userID, err)
		}
	}

	select {
	case h.unicast <- UserMessage{UserID: userID, Data: data}:
		return nil
	default:
		slog.Warn("hub unicast buffer full, notification dropped", "user_id", userID)
		return fmt.Errorf("hub unicast buffer full")
	}
}

// NotifyFriendRequest delivers a real-time notification to a target user when receiving a friend request.
func (h *Hub) NotifyFriendRequest(targetUserID int64, item models.FriendshipItemResponse) error {
	data, err := EncodeMessage(TypeFriendRequestRecv, item)
	if err != nil {
		return fmt.Errorf("failed to encode friend request notification: %w", err)
	}
	return h.NotifyUser(targetUserID, data)
}

// NotifyFriendResponse delivers a real-time notification to a target user when their friend request is answered.
func (h *Hub) NotifyFriendResponse(targetUserID int64, item models.FriendshipItemResponse) error {
	data, err := EncodeMessage(TypeFriendRequestResponse, item)
	if err != nil {
		return fmt.Errorf("failed to encode friend request response notification: %w", err)
	}
	return h.NotifyUser(targetUserID, data)
}

// NotifyFriendDeleted delivers a real-time notification to a target user when a friendship is deleted.
func (h *Hub) NotifyFriendDeleted(targetUserID int64, friendshipID int64) error {
	data, err := EncodeMessage(TypeFriendDeleted, models.FriendDeletePayload{FriendshipID: friendshipID})
	if err != nil {
		return fmt.Errorf("failed to encode friend deleted notification: %w", err)
	}
	return h.NotifyUser(targetUserID, data)
}


