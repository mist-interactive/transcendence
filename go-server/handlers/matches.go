package handlers

import (
	"dbBackend/models"
	"encoding/json"
	"log/slog"
	"net/http"
)

// MatchCreate handles POST /api/internal/matches.
// It is called by the WebSocket service when a match challenge is accepted.
// It resolves player usernames to database primary keys and inserts a new match
// with status 'in_progress', returning the newly generated match ID.
func (h *Handler) MatchCreate(w http.ResponseWriter, r *http.Request) {
	input, err := DecodeAndValidate[models.MatchCreateInput](r)
	if err != nil {
		http.Error(w, "Problem validating request", http.StatusBadRequest)
		return
	}
	if input.Player1 == input.Player2 {
		slog.Warn("match create rejected: player cannot play themselves", "player", input.Player1)
		http.Error(w, "Players cannot play themselves", http.StatusConflict)
		return
	}

	p1, err := h.getUserByUsername(r.Context(), input.Player1)
	if err != nil {
		slog.Warn("match create failed: player 1 not found", "player1", input.Player1, "error", err)
		http.Error(w, "Player 1 not found", http.StatusNotFound)
		return
	}
	p2, err := h.getUserByUsername(r.Context(), input.Player2)
	if err != nil {
		slog.Warn("match create failed: player 2 not found", "player2", input.Player2, "error", err)
		http.Error(w, "Player 2 not found", http.StatusNotFound)
		return
	}

	match := &models.MatchRecord{
		Player1: p1.ID,
		Player2: p2.ID,
		Status:  models.StatusInProgress,
	}
	err = h.DB.NewInsert().
		Model(match).
		Scan(r.Context())
	if err != nil {
		HandleDBError(w, err, "Match creation")
		return
	}

	slog.Info("match record created in database", "match_id", match.ID, "player1", input.Player1, "player2", input.Player2)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"id": match.ID,
	})
}
