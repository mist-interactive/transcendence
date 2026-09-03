package handlers

import (
	"dbBackend/models"
	"encoding/json"
	"net/http"
)

func (h *Handler) MatchCreate(w http.ResponseWriter, r *http.Request) {
	input, err := DecodeAndValidate[models.MatchCreateInput](r)
	if err != nil {
		http.Error(w, "Problem validating request", http.StatusBadRequest)
		return
	}
	if input.Player1 == input.Player2 {
		http.Error(w, "Players cannot play themselves", http.StatusConflict)
		return
	}

	p1, err := h.getUserByUsername(r.Context(), input.Player1)
	if err != nil {
		http.Error(w, "Player 1 not found", http.StatusNotFound)
		return
	}
	p2, err := h.getUserByUsername(r.Context(), input.Player2)
	if err != nil {
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"id": match.ID,
	})
}
