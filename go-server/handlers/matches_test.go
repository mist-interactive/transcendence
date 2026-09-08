package handlers_test

import (
	"bytes"
	"context"
	"dbBackend/handlers"
	"dbBackend/internal/testutil"
	"dbBackend/models"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchPatch(t *testing.T) {
	ctx := context.Background()
	user1, cleanup1 := testutil.MakeTestUser(t, testDB)
	defer cleanup1()
	testutil.RegisterUser(t, user1, testDB)

	user2, cleanup2 := testutil.MakeTestUser(t, testDB)
	defer cleanup2()
	testutil.RegisterUser(t, user2, testDB)

	defer func() {
		_, _ = testDB.NewDelete().
			Model((*models.MatchRecord)(nil)).
			Where("player_one IN (?, ?) OR player_two IN (?, ?)", user1.ID, user2.ID, user1.ID, user2.ID).
			Exec(ctx)
	}()

	h := handlers.NewHandler(testDB, nil, nil, "test-api-key")

	t.Run("Success: Unordered scores mapped correctly and player 2 win inferred", func(t *testing.T) {
		match := &models.MatchRecord{
			Player1: user1.ID,
			Player2: user2.ID,
			Status:  models.StatusInProgress,
		}
		_, err := testDB.NewInsert().Model(match).Exec(ctx)
		require.NoError(t, err)

		// Send scores in reversed order: user2 (player 2) has 7, user1 (player 1) has 3
		payload := models.MatchPatchInput{
			Scores: []models.PlayerScoreInput{
				{PlayerID: user2.ID, Score: 7},
				{PlayerID: user1.ID, Score: 3},
			},
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/internal/matches/%d", match.ID), bytes.NewReader(body))
		req.SetPathValue("id", fmt.Sprintf("%d", match.ID))
		w := httptest.NewRecorder()

		h.MatchPatch(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code)

		var updated models.MatchRecord
		err = testDB.NewSelect().Model(&updated).Where("id = ?", match.ID).Scan(ctx)
		require.NoError(t, err)

		assert.Equal(t, models.StatusFinished, updated.Status)
		require.NotNil(t, updated.Result)
		assert.Equal(t, models.ResultPlayer2Win, *updated.Result)
		require.NotNil(t, updated.Player1Score)
		assert.Equal(t, 3, *updated.Player1Score)
		require.NotNil(t, updated.Player2Score)
		assert.Equal(t, 7, *updated.Player2Score)
		assert.NotNil(t, updated.FinishedAt)
	})

	t.Run("Success: Equal scores infer draw", func(t *testing.T) {
		match := &models.MatchRecord{
			Player1: user1.ID,
			Player2: user2.ID,
			Status:  models.StatusInProgress,
		}
		_, err := testDB.NewInsert().Model(match).Exec(ctx)
		require.NoError(t, err)

		payload := models.MatchPatchInput{
			Scores: []models.PlayerScoreInput{
				{PlayerID: user1.ID, Score: 4},
				{PlayerID: user2.ID, Score: 4},
			},
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/internal/matches/%d", match.ID), bytes.NewReader(body))
		req.SetPathValue("id", fmt.Sprintf("%d", match.ID))
		w := httptest.NewRecorder()

		h.MatchPatch(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code)

		var updated models.MatchRecord
		err = testDB.NewSelect().Model(&updated).Where("id = ?", match.ID).Scan(ctx)
		require.NoError(t, err)

		assert.Equal(t, models.StatusFinished, updated.Status)
		require.NotNil(t, updated.Result)
		assert.Equal(t, models.ResultDraw, *updated.Result)
		assert.Equal(t, 4, *updated.Player1Score)
		assert.Equal(t, 4, *updated.Player2Score)
	})

	t.Run("Failure: Duplicate player ID in scores", func(t *testing.T) {
		payload := models.MatchPatchInput{
			Scores: []models.PlayerScoreInput{
				{PlayerID: user1.ID, Score: 5},
				{PlayerID: user1.ID, Score: 2},
			},
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPatch, "/api/internal/matches/999", bytes.NewReader(body))
		req.SetPathValue("id", "999")
		w := httptest.NewRecorder()

		h.MatchPatch(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Failure: Non-participant player ID in scores", func(t *testing.T) {
		match := &models.MatchRecord{
			Player1: user1.ID,
			Player2: user2.ID,
			Status:  models.StatusInProgress,
		}
		_, err := testDB.NewInsert().Model(match).Exec(ctx)
		require.NoError(t, err)

		payload := models.MatchPatchInput{
			Scores: []models.PlayerScoreInput{
				{PlayerID: user1.ID, Score: 5},
				{PlayerID: 999999, Score: 2},
			},
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/internal/matches/%d", match.ID), bytes.NewReader(body))
		req.SetPathValue("id", fmt.Sprintf("%d", match.ID))
		w := httptest.NewRecorder()

		h.MatchPatch(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Failure: Match already finished (conflict)", func(t *testing.T) {
		result := models.ResultPlayer1Win
		match := &models.MatchRecord{
			Player1: user1.ID,
			Player2: user2.ID,
			Status:  models.StatusFinished,
			Result:  &result,
		}
		_, err := testDB.NewInsert().Model(match).Exec(ctx)
		require.NoError(t, err)

		payload := models.MatchPatchInput{
			Scores: []models.PlayerScoreInput{
				{PlayerID: user1.ID, Score: 5},
				{PlayerID: user2.ID, Score: 2},
			},
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/internal/matches/%d", match.ID), bytes.NewReader(body))
		req.SetPathValue("id", fmt.Sprintf("%d", match.ID))
		w := httptest.NewRecorder()

		h.MatchPatch(w, req)
		assert.Equal(t, http.StatusConflict, w.Code)
	})
}
