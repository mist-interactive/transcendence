package realtime

import (
	"context"
	"dbBackend/models"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

type mockDataStore struct {
	mu                 sync.Mutex
	getFriendsListFunc func(ctx context.Context, userID int64) ([]int64, error)
	createMatchFunc    func(ctx context.Context, p1, p2 string) (int64, error)
	getActiveMatchFunc func(ctx context.Context, userID int64) (*models.ActiveMatchResponse, error)
	saveMessageFunc    func(ctx context.Context, userID int64, recipient, content string) (*models.Message, error)
}

func (m *mockDataStore) GetFriendsList(ctx context.Context, userID int64) ([]int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getFriendsListFunc != nil {
		return m.getFriendsListFunc(ctx, userID)
	}
	return nil, nil
}

func (m *mockDataStore) CreateMatch(ctx context.Context, p1, p2 string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createMatchFunc != nil {
		return m.createMatchFunc(ctx, p1, p2)
	}
	return 0, nil
}

func (m *mockDataStore) GetActiveMatch(ctx context.Context, userID int64) (*models.ActiveMatchResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getActiveMatchFunc != nil {
		return m.getActiveMatchFunc(ctx, userID)
	}
	return nil, nil
}

func (m *mockDataStore) SaveMessage(ctx context.Context, userID int64, recipient, content string) (*models.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveMessageFunc != nil {
		return m.saveMessageFunc(ctx, userID, recipient, content)
	}
	return nil, nil
}

func readWSMessage(t *testing.T, ch <-chan []byte, timeout time.Duration) (WebsocketMessage, bool) {
	t.Helper()
	select {
	case data := <-ch:
		var msg WebsocketMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("failed to decode websocket message: %v", err)
		}
		return msg, true
	case <-time.After(timeout):
		return WebsocketMessage{}, false
	}
}

func parsePayload[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var payload T
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	return payload
}

func TestHubMatchReconnections(t *testing.T) {
	tests := []struct {
		name string
		test func(t *testing.T)
	}{
		{
			name: "Match start: registers sessions in memory and broadcasts match_started to both players",
			test: func(t *testing.T) {
				store := &mockDataStore{}
				hub := NewHub(store)

				p1 := &Client{Hub: hub, UserID: 1, Username: "alice", Send: make(chan []byte, 10)}
				p2 := &Client{Hub: hub, UserID: 2, Username: "bob", Send: make(chan []byte, 10)}

				hub.clients[p1.UserID] = p1
				hub.clients[p2.UserID] = p2

				matchID := int64(101)
				hub.onMatchStarted("alice", "bob", matchID)

				// Verify in-memory state
				s1, ok1 := hub.activeMatches["alice"]
				if !ok1 || s1.MatchID != matchID || s1.Opponent != "bob" {
					t.Errorf("alice active match mismatch: got %+v, want match_id %d, opponent bob", s1, matchID)
				}
				s2, ok2 := hub.activeMatches["bob"]
				if !ok2 || s2.MatchID != matchID || s2.Opponent != "alice" {
					t.Errorf("bob active match mismatch: got %+v, want match_id %d, opponent alice", s2, matchID)
				}

				// Verify messages sent
				msg1, ok := readWSMessage(t, p1.Send, 50*time.Millisecond)
				if !ok {
					t.Fatalf("alice did not receive match_started")
				}
				if msg1.Type != TypeMatchStarted {
					t.Errorf("expected alice message type %s, got %s", TypeMatchStarted, msg1.Type)
				}
				p1Data := parsePayload[MatchSessionPayload](t, msg1.Payload)
				if p1Data.MatchID != matchID || p1Data.Opponent != "bob" {
					t.Errorf("alice payload mismatch: got %+v", p1Data)
				}

				msg2, ok := readWSMessage(t, p2.Send, 50*time.Millisecond)
				if !ok {
					t.Fatalf("bob did not receive match_started")
				}
				if msg2.Type != TypeMatchStarted {
					t.Errorf("expected bob message type %s, got %s", TypeMatchStarted, msg2.Type)
				}
				p2Data := parsePayload[MatchSessionPayload](t, msg2.Payload)
				if p2Data.MatchID != matchID || p2Data.Opponent != "alice" {
					t.Errorf("bob payload mismatch: got %+v", p2Data)
				}
			},
		},
		{
			name: "Opponent disconnect: notifies opponent and preserves memory state",
			test: func(t *testing.T) {
				store := &mockDataStore{}
				hub := NewHub(store)

				p1 := &Client{Hub: hub, UserID: 1, Username: "alice", Send: make(chan []byte, 10)}
				p2 := &Client{Hub: hub, UserID: 2, Username: "bob", Send: make(chan []byte, 10)}

				hub.clients[p1.UserID] = p1
				hub.clients[p2.UserID] = p2

				matchID := int64(102)
				hub.activeMatches["alice"] = &MatchSessionPayload{MatchID: matchID, Opponent: "bob"}
				hub.activeMatches["bob"] = &MatchSessionPayload{MatchID: matchID, Opponent: "alice"}

				hub.handleUnregister(p1)

				// Bob should receive opponent_disconnected
				msg, ok := readWSMessage(t, p2.Send, 50*time.Millisecond)
				if !ok {
					t.Fatalf("bob did not receive opponent_disconnected notification")
				}
				if msg.Type != TypeOpponentDisconnected {
					t.Errorf("expected message type %s, got %s", TypeOpponentDisconnected, msg.Type)
				}
				data := parsePayload[MatchSessionPayload](t, msg.Payload)
				if data.MatchID != matchID || data.Opponent != "alice" {
					t.Errorf("payload mismatch: got %+v", data)
				}

				// Alice should still be registered in memory activeMatches (non-destructive)
				if _, exists := hub.activeMatches["alice"]; !exists {
					t.Errorf("alice active match was unexpectedly removed from memory")
				}
			},
		},
		{
			name: "Player reconnect: sends active_match to reconnected player and opponent_reconnected to opponent",
			test: func(t *testing.T) {
				store := &mockDataStore{}
				hub := NewHub(store)

				p2 := &Client{Hub: hub, UserID: 2, Username: "bob", Send: make(chan []byte, 10)}
				hub.clients[p2.UserID] = p2

				matchID := int64(103)
				hub.activeMatches["alice"] = &MatchSessionPayload{MatchID: matchID, Opponent: "bob"}
				hub.activeMatches["bob"] = &MatchSessionPayload{MatchID: matchID, Opponent: "alice"}

				// Alice reconnects
				alice := &Client{Hub: hub, UserID: 1, Username: "alice", Send: make(chan []byte, 10)}
				hub.handleRegister(alice)

				// Alice receives active_match
				msgAlice, ok := readWSMessage(t, alice.Send, 50*time.Millisecond)
				if !ok {
					t.Fatalf("alice did not receive active_match on reconnect")
				}
				if msgAlice.Type != TypeActiveMatch {
					t.Errorf("expected alice message type %s, got %s", TypeActiveMatch, msgAlice.Type)
				}
				aliceData := parsePayload[MatchSessionPayload](t, msgAlice.Payload)
				if aliceData.MatchID != matchID || aliceData.Opponent != "bob" {
					t.Errorf("alice active match payload mismatch: got %+v", aliceData)
				}

				// Bob receives opponent_reconnected
				msgBob, ok := readWSMessage(t, p2.Send, 50*time.Millisecond)
				if !ok {
					t.Fatalf("bob did not receive opponent_reconnected")
				}
				if msgBob.Type != TypeOpponentReconnected {
					t.Errorf("expected bob message type %s, got %s", TypeOpponentReconnected, msgBob.Type)
				}
				bobData := parsePayload[MatchSessionPayload](t, msgBob.Payload)
				if bobData.MatchID != matchID || bobData.Opponent != "alice" {
					t.Errorf("bob payload mismatch: got %+v", bobData)
				}
			},
		},
		{
			name: "DB sync restores missing match into memory after server restart",
			test: func(t *testing.T) {
				store := &mockDataStore{}
				hub := NewHub(store)

				alice := &Client{Hub: hub, UserID: 1, Username: "alice", Send: make(chan []byte, 10)}
				bob := &Client{Hub: hub, UserID: 2, Username: "bob", Send: make(chan []byte, 10)}
				hub.clients[alice.UserID] = alice
				hub.clients[bob.UserID] = bob

				// Memory is empty (like after restart)
				matchID := int64(104)
				resp := &models.ActiveMatchResponse{
					MatchID:          matchID,
					OpponentID:       2,
					OpponentUsername: "bob",
					StartedAt:        time.Now(),
				}

				hub.onActiveMatchSync(alice, resp)

				// Alice active match in memory
				sAlice, okAlice := hub.activeMatches["alice"]
				if !okAlice || sAlice.MatchID != matchID || sAlice.Opponent != "bob" {
					t.Errorf("alice match not restored properly: got %+v", sAlice)
				}

				// Bob active match also populated
				sBob, okBob := hub.activeMatches["bob"]
				if !okBob || sBob.MatchID != matchID || sBob.Opponent != "alice" {
					t.Errorf("bob match not restored properly: got %+v", sBob)
				}

				// Alice receives active_match
				msgAlice, ok := readWSMessage(t, alice.Send, 50*time.Millisecond)
				if !ok {
					t.Fatalf("alice did not receive active_match from DB sync")
				}
				if msgAlice.Type != TypeActiveMatch {
					t.Errorf("expected alice message type %s, got %s", TypeActiveMatch, msgAlice.Type)
				}

				// Bob receives opponent_reconnected
				msgBob, ok := readWSMessage(t, bob.Send, 50*time.Millisecond)
				if !ok {
					t.Fatalf("bob did not receive opponent_reconnected from DB sync")
				}
				if msgBob.Type != TypeOpponentReconnected {
					t.Errorf("expected bob message type %s, got %s", TypeOpponentReconnected, msgBob.Type)
				}
			},
		},
		{
			name: "DB sync prunes memory when match has finished",
			test: func(t *testing.T) {
				store := &mockDataStore{}
				hub := NewHub(store)

				alice := &Client{Hub: hub, UserID: 1, Username: "alice", Send: make(chan []byte, 10)}
				hub.clients[alice.UserID] = alice

				matchID := int64(105)
				hub.activeMatches["alice"] = &MatchSessionPayload{MatchID: matchID, Opponent: "bob"}
				hub.activeMatches["bob"] = &MatchSessionPayload{MatchID: matchID, Opponent: "alice"}

				// DB returns nil (match finished or deleted)
				hub.onActiveMatchSync(alice, nil)

				if _, exists := hub.activeMatches["alice"]; exists {
					t.Errorf("alice active match was not pruned from memory")
				}
				if _, exists := hub.activeMatches["bob"]; exists {
					t.Errorf("bob active match was not pruned from memory")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.test(t)
		})
	}
}
