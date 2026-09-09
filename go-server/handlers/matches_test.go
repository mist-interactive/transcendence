package handlers_test

import (
	"bytes"
	"context"
	"crypto/rsa"
	"dbBackend/handlers"
	"dbBackend/internal/testutil"
	"dbBackend/models"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

	handler := handlers.NewHandler(testDB, nil, nil, "", nil)

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

// setupMatchesTestRouter initializes the handler and returns an http.Handler with JWTGuard mounted.
func setupMatchesTestRouter(t *testing.T) (*handlers.Handler, http.Handler, *rsa.PrivateKey) {
	t.Helper()

	privateKey, publicKey := getTestKeys(t)
	h := handlers.NewHandler(testDB, privateKey, publicKey, "")
	mux := http.NewServeMux()

	protected := handlers.NewGroup(mux, "/api/protected", h.JWTGuard)
	protected.HandleFunc("GET /matches", h.MatchHistoryGet)

	return h, mux, privateKey
}

func TestMatchHistoryGet_Integration(t *testing.T) {
	ctx := context.Background()
	_, router, privKey := setupMatchesTestRouter(t)

	alice := testutil.CreateRegisteredUser(t, testDB)
	bob := testutil.CreateRegisteredUser(t, testDB)
	charlie := testutil.CreateRegisteredUser(t, testDB)

	aliceAuth := makeAuthHeader(t, alice, privKey)
	charlieAuth := makeAuthHeader(t, charlie, privKey)

	t.Cleanup(func() {
		_, _ = testDB.NewDelete().
			Model((*models.MatchRecord)(nil)).
			Where("player_one IN (?, ?, ?) OR player_two IN (?, ?, ?)",
				alice.ID, bob.ID, charlie.ID, alice.ID, bob.ID, charlie.ID).
			Exec(ctx)
	})

	now := time.Now()

	// Seed matches for alice and bob:
	// Match 1: alice (p1) vs bob (p2), finished, alice win (5-2), 30 mins ago
	p1Score1, p2Score1 := 5, 2
	result1 := models.ResultPlayer1Win
	started1 := now.Add(-30 * time.Minute)
	finished1 := now.Add(-15 * time.Minute)
	m1 := &models.MatchRecord{
		Player1:      alice.ID,
		Player2:      bob.ID,
		Player1Score: &p1Score1,
		Player2Score: &p2Score1,
		Status:       models.StatusFinished,
		Result:       &result1,
		StartedAt:    started1,
		FinishedAt:   &finished1,
	}
	if _, err := testDB.NewInsert().Model(m1).Exec(ctx); err != nil {
		t.Fatalf("failed to insert match 1: %v", err)
	}

	// Match 2: bob (p1) vs alice (p2), finished, bob win (7-3) => alice loss, 20 mins ago
	p1Score2, p2Score2 := 7, 3
	result2 := models.ResultPlayer1Win
	started2 := now.Add(-20 * time.Minute)
	finished2 := now.Add(-5 * time.Minute)
	m2 := &models.MatchRecord{
		Player1:      bob.ID,
		Player2:      alice.ID,
		Player1Score: &p1Score2,
		Player2Score: &p2Score2,
		Status:       models.StatusFinished,
		Result:       &result2,
		StartedAt:    started2,
		FinishedAt:   &finished2,
	}
	if _, err := testDB.NewInsert().Model(m2).Exec(ctx); err != nil {
		t.Fatalf("failed to insert match 2: %v", err)
	}

	// Match 3: alice (p1) vs bob (p2), finished, draw (4-4), 10 mins ago
	p1Score3, p2Score3 := 4, 4
	result3 := models.ResultDraw
	started3 := now.Add(-10 * time.Minute)
	finished3 := now.Add(-1 * time.Minute)
	m3 := &models.MatchRecord{
		Player1:      alice.ID,
		Player2:      bob.ID,
		Player1Score: &p1Score3,
		Player2Score: &p2Score3,
		Status:       models.StatusFinished,
		Result:       &result3,
		StartedAt:    started3,
		FinishedAt:   &finished3,
	}
	if _, err := testDB.NewInsert().Model(m3).Exec(ctx); err != nil {
		t.Fatalf("failed to insert match 3: %v", err)
	}

	// Match 4: alice (p1) vs bob (p2), in_progress, started just now
	started4 := now
	m4 := &models.MatchRecord{
		Player1:   alice.ID,
		Player2:   bob.ID,
		Status:    models.StatusInProgress,
		StartedAt: started4,
	}
	if _, err := testDB.NewInsert().Model(m4).Exec(ctx); err != nil {
		t.Fatalf("failed to insert match 4: %v", err)
	}

	tests := []struct {
		name           string
		authHeader     string
		queryURL       string
		expectedStatus int
		validate       func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{
			name:           "Failure: Missing Bearer token returns 401 Unauthorized",
			authHeader:     "",
			queryURL:       "/api/protected/matches",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "Failure: Invalid Bearer token returns 401 Unauthorized",
			authHeader:     "Bearer invalid.jwt.token",
			queryURL:       "/api/protected/matches",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "Success: Empty history returns empty JSON array",
			authHeader:     charlieAuth,
			queryURL:       "/api/protected/matches",
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var history []models.MatchHistoryResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &history); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if len(history) != 0 {
					t.Errorf("expected 0 matches, got %d", len(history))
				}
				if body := rec.Body.String(); body != "[]\n" && body != "[]" {
					t.Errorf("expected raw JSON '[]', got %q", body)
				}
			},
		},
		{
			name:           "Success: All matches ordered by started_at DESC with correct relative perspective",
			authHeader:     aliceAuth,
			queryURL:       "/api/protected/matches",
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var history []models.MatchHistoryResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &history); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if len(history) != 4 {
					t.Fatalf("expected 4 matches, got %d", len(history))
				}

				// Match 4: in_progress
				if history[0].ID != m4.ID {
					t.Errorf("expected match ID %d, got %d", m4.ID, history[0].ID)
				}
				if history[0].Status != models.StatusInProgress {
					t.Errorf("expected status %s, got %s", models.StatusInProgress, history[0].Status)
				}
				if history[0].Outcome != nil {
					t.Errorf("expected outcome nil, got %v", history[0].Outcome)
				}
				if history[0].UserScore != nil || history[0].OpponentScore != nil {
					t.Errorf("expected nil scores for in_progress match")
				}
				if history[0].OpponentUsername != bob.Username {
					t.Errorf("expected opponent %s, got %s", bob.Username, history[0].OpponentUsername)
				}

				// Match 3: draw (4 - 4)
				if history[1].ID != m3.ID {
					t.Errorf("expected match ID %d, got %d", m3.ID, history[1].ID)
				}
				if history[1].Outcome == nil || *history[1].Outcome != models.OutcomeDraw {
					t.Errorf("expected outcome %s, got %v", models.OutcomeDraw, history[1].Outcome)
				}
				if history[1].UserScore == nil || *history[1].UserScore != 4 {
					t.Errorf("expected user_score 4, got %v", history[1].UserScore)
				}
				if history[1].OpponentScore == nil || *history[1].OpponentScore != 4 {
					t.Errorf("expected opponent_score 4, got %v", history[1].OpponentScore)
				}

				// Match 2: bob was player 1 (7), alice was player 2 (3) -> alice lost 3-7
				if history[2].ID != m2.ID {
					t.Errorf("expected match ID %d, got %d", m2.ID, history[2].ID)
				}
				if history[2].Outcome == nil || *history[2].Outcome != models.OutcomeLoss {
					t.Errorf("expected outcome %s, got %v", models.OutcomeLoss, history[2].Outcome)
				}
				if history[2].UserScore == nil || *history[2].UserScore != 3 {
					t.Errorf("expected user_score 3, got %v", history[2].UserScore)
				}
				if history[2].OpponentScore == nil || *history[2].OpponentScore != 7 {
					t.Errorf("expected opponent_score 7, got %v", history[2].OpponentScore)
				}

				// Match 1: alice was player 1 (5), bob was player 2 (2) -> alice won 5-2
				if history[3].ID != m1.ID {
					t.Errorf("expected match ID %d, got %d", m1.ID, history[3].ID)
				}
				if history[3].Outcome == nil || *history[3].Outcome != models.OutcomeWin {
					t.Errorf("expected outcome %s, got %v", models.OutcomeWin, history[3].Outcome)
				}
				if history[3].UserScore == nil || *history[3].UserScore != 5 {
					t.Errorf("expected user_score 5, got %v", history[3].UserScore)
				}
				if history[3].OpponentScore == nil || *history[3].OpponentScore != 2 {
					t.Errorf("expected opponent_score 2, got %v", history[3].OpponentScore)
				}
			},
		},
		{
			name:           "Success: Filter by status finished",
			authHeader:     aliceAuth,
			queryURL:       "/api/protected/matches?status=finished",
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var history []models.MatchHistoryResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &history); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if len(history) != 3 {
					t.Fatalf("expected 3 finished matches, got %d", len(history))
				}
				for _, m := range history {
					if m.Status != models.StatusFinished {
						t.Errorf("expected status %s, got %s", models.StatusFinished, m.Status)
					}
				}
			},
		},
		{
			name:           "Success: Filter by status in_progress",
			authHeader:     aliceAuth,
			queryURL:       "/api/protected/matches?status=in_progress",
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var history []models.MatchHistoryResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &history); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if len(history) != 1 {
					t.Fatalf("expected 1 in_progress match, got %d", len(history))
				}
				if history[0].ID != m4.ID {
					t.Errorf("expected match ID %d, got %d", m4.ID, history[0].ID)
				}
			},
		},
		{
			name:           "Success: Pagination limit and offset",
			authHeader:     aliceAuth,
			queryURL:       "/api/protected/matches?limit=1&offset=1",
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var history []models.MatchHistoryResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &history); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if len(history) != 1 {
					t.Fatalf("expected 1 match with limit=1, got %d", len(history))
				}
				// offset=1 should skip m4 and return m3
				if history[0].ID != m3.ID {
					t.Errorf("expected match ID %d, got %d", m3.ID, history[0].ID)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := doTestRequest(router, http.MethodGet, tc.queryURL, tc.authHeader, nil)

			if rec.Code != tc.expectedStatus {
				t.Errorf("[%s] expected status %d, got %d. Body: %s",
					tc.name, tc.expectedStatus, rec.Code, rec.Body.String())
			}

			if tc.validate != nil {
				tc.validate(t, rec)
			}
		})
	}
}

