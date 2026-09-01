package handlers_test

import (
	"bytes"
	"dbBackend/handlers"
	"dbBackend/internal/testutil"
	"dbBackend/models"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTryRegister_Integration(t *testing.T) {
	h := handlers.NewHandler(testDB, nil, nil, "")
	tests := []struct {
		name           string
		setup          func(t *testing.T) models.RegisterRequest
		expectedStatus int
	}{
		{
			name: "Success: new user was registered ",
			setup: func(t *testing.T) models.RegisterRequest {
				u, cleanup := testutil.MakeTestUser(t, testDB)
				t.Cleanup(cleanup)
				return models.RegisterRequest{
					Username: u.Username,
					Email:    u.Email,
					Password: "password123",
				}
			},
			expectedStatus: http.StatusCreated,
		},
		{
			name: "Failure: try to register a copy of a user",
			setup: func(t *testing.T) models.RegisterRequest {
				u, cleanup := testutil.MakeTestUser(t, testDB)
				testutil.RegisterUser(t, u, testDB)
				t.Cleanup(cleanup)
				return models.RegisterRequest{
					Username: u.Username,
					Email:    u.Email,
					Password: "password123",
				}
			},
			expectedStatus: http.StatusConflict,
		},
		{
			name: "Failure: try to register a user with an email already in use",
			setup: func(t *testing.T) models.RegisterRequest {
				u, cleanup := testutil.MakeTestUser(t, testDB)
				testutil.RegisterUser(t, u, testDB)
				t.Cleanup(cleanup)
				newU, _ := testutil.MakeTestUser(t, testDB)
				return models.RegisterRequest{
					Username: newU.Username,
					Email:    u.Email,
					Password: "password123",
				}
			},
			expectedStatus: http.StatusConflict,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			userRequest := tc.setup(t)
			jsonBytes, _ := json.Marshal(userRequest)
			req := httptest.NewRequest("POST", "/api/register", bytes.NewBuffer(jsonBytes))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			h.TryRegister(rec, req)

			if rec.Code != tc.expectedStatus {
				t.Errorf("[%s] expected status %d, got %d. %s", tc.name, tc.expectedStatus, rec.Code, rec.Body)
			}
		})
	}
}
