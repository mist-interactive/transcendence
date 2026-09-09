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
// It is called by the Game Server when a match concludes to record player scores and result.
// It maps the reported scores to player_one_score and player_two_score based on participant IDs,
// and automatically infers the match result (player1_win, player2_win, draw) and status (finished).
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

	if input.Scores[0].PlayerID == input.Scores[1].PlayerID {
		slog.Warn("match patch rejected: duplicate player ID in scores", "match_id", matchID, "player_id", input.Scores[0].PlayerID)
		http.Error(w, "Scores must be for two distinct players", http.StatusBadRequest)
		return
	}

	var match models.MatchRecord
	err = h.DB.NewSelect().
		Model(&match).
		Where("id = ?", matchID).
		Scan(r.Context())
	if err != nil {
		HandleDBError(w, err, "Fetching match")
		return
	}

	if match.Status != models.StatusInProgress {
		slog.Warn("match patch rejected: match is not in progress", "match_id", matchID, "status", match.Status)
		http.Error(w, fmt.Sprintf("Match with ID %d is already %s", matchID, match.Status), http.StatusConflict)
		return
	}

	// Map reported scores to player 1 and player 2 regardless of order
	var p1Score, p2Score int
	var found1, found2 bool

	for _, s := range input.Scores {
		switch s.PlayerID {
		case match.Player1:
			p1Score = s.Score
			found1 = true
		case match.Player2:
			p2Score = s.Score
			found2 = true
		}
	}

	if !found1 || !found2 {
		slog.Warn("match patch rejected: score player IDs do not match participants",
			"match_id", matchID,
			"expected_p1", match.Player1,
			"expected_p2", match.Player2,
			"received_id1", input.Scores[0].PlayerID,
			"received_id2", input.Scores[1].PlayerID,
		)
		http.Error(w, "Reported scores do not match the registered match participants", http.StatusBadRequest)
		return
	}

	// Infer status (default to finished if omitted)
	status := models.StatusFinished
	if input.Status != nil && *input.Status != "" {
		status = *input.Status
	}

	// Infer result based on scores
	var result models.MatchResult
	if p1Score > p2Score {
		result = models.ResultPlayer1Win
	} else if p2Score > p1Score {
		result = models.ResultPlayer2Win
	} else {
		result = models.ResultDraw
	}

	now := time.Now()
	res, err := h.DB.NewUpdate().
		Model((*models.MatchRecord)(nil)).
		Where("id = ?", matchID).
		Where("status = ?", models.StatusInProgress).
		Set("player_one_score = ?", p1Score).
		Set("player_two_score = ?", p2Score).
		Set("status = ?", status).
		Set("result = ?", result).
		Set("finished_at = ?", now).
		Exec(r.Context())

	if err != nil {
		HandleDBError(w, err, "Updating match history")
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		slog.Warn("match patch failed: match status changed concurrently", "match_id", matchID)
		http.Error(w, fmt.Sprintf("No match in progress with ID %d was found", matchID), http.StatusConflict)
		return
	}

	slog.Info("match result updated in database",
		"match_id", matchID,
		"player_one_id", match.Player1,
		"player_one_score", p1Score,
		"player_two_id", match.Player2,
		"player_two_score", p2Score,
		"status", status,
		"result", result,
	)
	w.WriteHeader(http.StatusNoContent)
}

// MatchHistoryGet handles GET /api/protected/matches.
// It retrieves the match history for the authenticated user, mapping opponent info,
// user-relative scores, and outcome (win, loss, draw, aborted).
// Supports optional query parameters:
//   - status: filter by match status (e.g., 'finished', 'in_progress', 'abandoned')
//   - limit: maximum number of records to return (default 50, max 100)
//   - offset: number of records to skip (default 0)
func (h *Handler) MatchHistoryGet(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok || userID == 0 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	matches := make([]models.MatchHistoryResponse, 0)

	//build the query: get all matches with authenticated user as one party, and fill in opponent details
	q := h.DB.NewSelect().
		TableExpr("matches AS m").
		ColumnExpr("m.id AS id").
		ColumnExpr("u.id AS opponent_id").
		ColumnExpr("u.username AS opponent").
		ColumnExpr("u.avatar_url AS opponent_avatar_url").
		ColumnExpr("CASE WHEN m.player_one = ? THEN m.player_one_score ELSE m.player_two_score END AS user_score", userID).
		ColumnExpr("CASE WHEN m.player_one = ? THEN m.player_two_score ELSE m.player_one_score END AS opponent_score", userID).
		ColumnExpr("m.status AS status").
		ColumnExpr("m.result AS result").
		ColumnExpr(`CASE
			WHEN m.result IS NULL THEN NULL
			WHEN m.result = 'draw' THEN 'draw'
			WHEN m.result = 'aborted' THEN 'aborted'
			WHEN (m.player_one = ? AND m.result = 'player1_win') OR (m.player_two = ? AND m.result = 'player2_win') THEN 'win'
			ELSE 'loss'
		END AS outcome`, userID, userID). //this CASE summarizes the outcome of the match
		ColumnExpr("m.started_at AS started_at").
		ColumnExpr("m.finished_at AS finished_at").
		Join("JOIN users AS u ON (m.player_one = ? AND m.player_two = u.id) OR (m.player_two = ? AND m.player_one = u.id)", userID, userID).
		Where("m.player_one = ? OR m.player_two = ?", userID, userID).
		Order("m.started_at DESC")

	//add status filter if one was provided
	if status := r.URL.Query().Get("status"); status != "" {
		q = q.Where("m.status = ?", status)
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			if parsed > 100 {
				parsed = 100
			}
			limit = parsed
		}
	}
	q = q.Limit(limit) //set a limit on results:default 50, or provided in parameter

	if o := r.URL.Query().Get("offset"); o != "" { //set offset if provided
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			q = q.Offset(parsed)
		}
	}

	err := q.Scan(r.Context(), &matches) //execute query
	if err != nil {
		HandleDBError(w, err, "Match history")
		return
	}

	slog.Debug("match history retrieved", "user_id", userID, "count", len(matches))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(matches)
}
