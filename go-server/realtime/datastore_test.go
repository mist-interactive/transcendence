package realtime

import (
	"context"
	"dbBackend/models"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHttpDataStoreGetActiveMatch(t *testing.T) {
	tests := []struct {
		name          string
		statusCode    int
		handler       http.HandlerFunc
		expectedError bool
		validate      func(t *testing.T, resp *models.ActiveMatchResponse)
	}{
		{
			name:       "Success: Returns active match when ongoing match exists (200 OK)",
			statusCode: http.StatusOK,
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-API-Key") != "test-api-key" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if r.URL.Path != "/api/internal/users/42/active-match" {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				resp := models.ActiveMatchResponse{
					MatchID:          100,
					OpponentID:       43,
					OpponentUsername: "charlie",
					StartedAt:        time.Now().Truncate(time.Second),
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(resp)
			},
			expectedError: false,
			validate: func(t *testing.T, resp *models.ActiveMatchResponse) {
				if resp == nil {
					t.Fatalf("expected non-nil response, got nil")
				}
				if resp.MatchID != 100 {
					t.Errorf("expected match_id 100, got %d", resp.MatchID)
				}
				if resp.OpponentUsername != "charlie" {
					t.Errorf("expected opponent 'charlie', got %s", resp.OpponentUsername)
				}
			},
		},
		{
			name:       "Success: Returns nil when no active match in progress (404 Not Found)",
			statusCode: http.StatusNotFound,
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			expectedError: false,
			validate: func(t *testing.T, resp *models.ActiveMatchResponse) {
				if resp != nil {
					t.Errorf("expected nil response on 404, got %+v", resp)
				}
			},
		},
		{
			name:       "Failure: Returns error on server error (500 Internal Server Error)",
			statusCode: http.StatusInternalServerError,
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			expectedError: true,
			validate: func(t *testing.T, resp *models.ActiveMatchResponse) {
				if resp != nil {
					t.Errorf("expected nil response on error, got %+v", resp)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(tc.handler)
			t.Cleanup(func() {
				server.Close()
			})

			store := NewHttpDataStore(server.URL, "test-api-key")
			resp, err := store.GetActiveMatch(context.Background(), 42)

			if (err != nil) != tc.expectedError {
				t.Fatalf("unexpected error status: got err=%v, want expectedError=%v", err, tc.expectedError)
			}
			tc.validate(t, resp)
		})
	}
}
