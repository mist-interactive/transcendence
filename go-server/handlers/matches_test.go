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
)

func TestMatchPatch(t *testing.T) {
	ctx := context.Background()
	user1, cleanup1 := testutil.MakeTestUser(t, testDB)
	t.Cleanup(cleanup1)
	testutil.RegisterUser(t, user1, testDB)

	user2, cleanup2 := testutil.MakeTestUser(t, testDB)
	t.Cleanup(cleanup2)
	testutil.RegisterUser(t, user2, testDB)

	t.Cleanup(func() {
		_, _ = testDB.NewDelete().
			Model((*models.MatchRecord)(nil)).
			Where("player_one IN (?, ?) OR player_two IN (?, ?)", user1.ID, user2.ID, user1.ID, user2.ID).
			Exec(ctx)
	})

	handler := handlers.NewHandler(testDB, nil, nil, "")

	tests := []struct {
		name           string
		setup          func(t *testing.T) (int64, models.MatchPatchInput)
		expectedStatus int
		validate       func(t *testing.T, matchID int64)
	}{
		{
			name: "Success: Unordered scores mapped correctly and player 2 win inferred",
			setup: func(t *testing.T) (int64, models.MatchPatchInput) {
				match := &models.MatchRecord{
					Player1: user1.ID,
					Player2: user2.ID,
					Status:  models.StatusInProgress,
				}
				if _, err := testDB.NewInsert().Model(match).Exec(ctx); err != nil {
					t.Fatalf("failed to insert match: %v", err)
				}
				return match.ID, models.MatchPatchInput{
					Scores: []models.PlayerScoreInput{
						{PlayerID: user2.ID, Score: 7},
						{PlayerID: user1.ID, Score: 3},
					},
				}
			},
			expectedStatus: http.StatusNoContent,
			validate: func(t *testing.T, matchID int64) {
				var m models.MatchRecord
				if err := testDB.NewSelect().Model(&m).Where("id = ?", matchID).Scan(ctx); err != nil {
					t.Fatalf("failed to fetch updated match: %v", err)
				}
				if m.Status != models.StatusFinished {
					t.Errorf("expected status %s, got %s", models.StatusFinished, m.Status)
				}
				if m.Result == nil || *m.Result != models.ResultPlayer2Win {
					t.Errorf("expected result %s, got %v", models.ResultPlayer2Win, m.Result)
				}
				if m.Player1Score == nil || *m.Player1Score != 3 {
					t.Errorf("expected player1_score 3, got %v", m.Player1Score)
				}
				if m.Player2Score == nil || *m.Player2Score != 7 {
					t.Errorf("expected player2_score 7, got %v", m.Player2Score)
				}
				if m.FinishedAt == nil {
					t.Errorf("expected finished_at timestamp to be set")
				}
			},
		},
		{
			name: "Success: Equal scores infer draw",
			setup: func(t *testing.T) (int64, models.MatchPatchInput) {
				match := &models.MatchRecord{
					Player1: user1.ID,
					Player2: user2.ID,
					Status:  models.StatusInProgress,
				}
				if _, err := testDB.NewInsert().Model(match).Exec(ctx); err != nil {
					t.Fatalf("failed to insert match: %v", err)
				}
				return match.ID, models.MatchPatchInput{
					Scores: []models.PlayerScoreInput{
						{PlayerID: user1.ID, Score: 4},
						{PlayerID: user2.ID, Score: 4},
					},
				}
			},
			expectedStatus: http.StatusNoContent,
			validate: func(t *testing.T, matchID int64) {
				var m models.MatchRecord
				if err := testDB.NewSelect().Model(&m).Where("id = ?", matchID).Scan(ctx); err != nil {
					t.Fatalf("failed to fetch updated match: %v", err)
				}
				if m.Status != models.StatusFinished {
					t.Errorf("expected status %s, got %s", models.StatusFinished, m.Status)
				}
				if m.Result == nil || *m.Result != models.ResultDraw {
					t.Errorf("expected result %s, got %v", models.ResultDraw, m.Result)
				}
				if m.Player1Score == nil || *m.Player1Score != 4 {
					t.Errorf("expected player1_score 4, got %v", m.Player1Score)
				}
				if m.Player2Score == nil || *m.Player2Score != 4 {
					t.Errorf("expected player2_score 4, got %v", m.Player2Score)
				}
			},
		},
		{
			name: "Failure: Duplicate player ID in scores",
			setup: func(t *testing.T) (int64, models.MatchPatchInput) {
				return 999, models.MatchPatchInput{
					Scores: []models.PlayerScoreInput{
						{PlayerID: user1.ID, Score: 5},
						{PlayerID: user1.ID, Score: 2},
					},
				}
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "Failure: Non-participant player ID in scores",
			setup: func(t *testing.T) (int64, models.MatchPatchInput) {
				match := &models.MatchRecord{
					Player1: user1.ID,
					Player2: user2.ID,
					Status:  models.StatusInProgress,
				}
				if _, err := testDB.NewInsert().Model(match).Exec(ctx); err != nil {
					t.Fatalf("failed to insert match: %v", err)
				}
				return match.ID, models.MatchPatchInput{
					Scores: []models.PlayerScoreInput{
						{PlayerID: user1.ID, Score: 5},
						{PlayerID: 999999, Score: 2},
					},
				}
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "Failure: Match already finished",
			setup: func(t *testing.T) (int64, models.MatchPatchInput) {
				result := models.ResultPlayer1Win
				match := &models.MatchRecord{
					Player1: user1.ID,
					Player2: user2.ID,
					Status:  models.StatusFinished,
					Result:  &result,
				}
				if _, err := testDB.NewInsert().Model(match).Exec(ctx); err != nil {
					t.Fatalf("failed to insert match: %v", err)
				}
				return match.ID, models.MatchPatchInput{
					Scores: []models.PlayerScoreInput{
						{PlayerID: user1.ID, Score: 5},
						{PlayerID: user2.ID, Score: 2},
					},
				}
			},
			expectedStatus: http.StatusConflict,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			matchID, input := tc.setup(t)
			jsonBytes, _ := json.Marshal(input)
			req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/internal/matches/%d", matchID), bytes.NewReader(jsonBytes))
			req.Header.Set("Content-Type", "application/json")
			req.SetPathValue("id", fmt.Sprintf("%d", matchID))
			rec := httptest.NewRecorder()

			handler.MatchPatch(rec, req)

			if rec.Code != tc.expectedStatus {
				t.Errorf("[%s] expected status %d, got %d. Server response: %q",
					tc.name, tc.expectedStatus, rec.Code, rec.Body.String())
			}

			if tc.validate != nil {
				tc.validate(t, matchID)
			}
		})
	}
}
