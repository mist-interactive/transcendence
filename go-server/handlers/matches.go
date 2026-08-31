package handlers

import (
	"dbBackend/models"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"
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

// MatchPatch handles PATCH /api/internal/matches/{id}.
// It is called by the Game Server when a match concludes to record the final result and status.
func (h *Handler) MatchPatch(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	matchID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		slog.Warn("match patch rejected: invalid match ID", "id", idStr, "error", err)
		http.Error(w, "Invalid match ID", http.StatusBadRequest)
		return
	}
	input, err := DecodeAndValidate[models.MatchPatchInput](r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	now := time.Now()
	res, err := h.DB.NewUpdate().
		Model((*models.MatchRecord)(nil)).
		Where("id = ?", matchID).
		Where("status = ?", models.StatusInProgress).
		Set("status = ?", input.Status).
		Set("result = ?", input.Result).
		Set("finished_at = ?", now).
		Exec(r.Context())

	if err != nil {
		HandleDBError(w, err, "Updating match history")
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		slog.Warn("match patch failed: no match in progress found", "match_id", matchID)
		http.Error(w, fmt.Sprintf("No match in progress with ID %d was found", matchID), http.StatusConflict)
		return
	}

	slog.Info("match result updated in database", "match_id", matchID, "status", input.Status, "result", input.Result)
	w.WriteHeader(http.StatusNoContent)
}
