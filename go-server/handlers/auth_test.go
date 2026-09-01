package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"dbBackend/handlers"
	"dbBackend/internal/testutil"
	"dbBackend/models"
)

func TestLogin(t *testing.T) {
	t.Helper()
	testUser, cleanup := testutil.MakeTestUser(t, testDB)
	t.Cleanup(cleanup)
	testutil.RegisterUser(t, testUser, testDB)
	handler := handlers.NewHandler(testDB, nil, nil, "")

	tests := []struct {
		name           string
		requestBody    models.LoginRequest
		expectedStatus int
		expectJSON     bool
		validate       func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{
			name: "Success - Correct credentials",
			requestBody: models.LoginRequest{
				Username: testUser.Username,
				Password: "password123",
			},
			expectedStatus: http.StatusOK,
			expectJSON:     true,
			validate:       validateSuccessfulLogin(testUser.ID),
		},
		{
			name: "Failure - Correct user but incorrect password",
			requestBody: models.LoginRequest{
				Username: testUser.Username,
				Password: "wrongpassword",
			},
			expectedStatus: http.StatusForbidden,
			expectJSON:     false,
			validate:       nil,
		},
		{
			name: "Failure - Nonexistent user",
			requestBody: models.LoginRequest{
				Username: "completely_different_" + testUser.Username,
				Password: "somepassword",
			},
			expectedStatus: http.StatusNotFound,
			expectJSON:     false,
			validate:       nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			jsonBytes, _ := json.Marshal(tc.requestBody)
			req := httptest.NewRequest("POST", "/api/login", bytes.NewBuffer(jsonBytes))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.CheckPassword(rec, req)

			if rec.Code != tc.expectedStatus {
				t.Errorf("[%s] expected status %d, got %d. Server response: %q",
					tc.name, tc.expectedStatus, rec.Code, rec.Body.String())
			}

			if tc.expectJSON {
				contentType := rec.Result().Header.Get("Content-Type")
				if contentType != "application/json" {
					t.Errorf("expected content-type application/json, got %s", contentType)
				}
			}
		})

	}
}

func validateSuccessfulLogin(expectedUserID int64) func(t *testing.T, rec *httptest.ResponseRecorder) {
	return func(t *testing.T, rec *httptest.ResponseRecorder) {
		t.Helper()
		ctx := context.Background()

		cookies := rec.Result().Cookies()
		var sessionCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == "session_id" {
				sessionCookie = c
				break
			}
		}

		if sessionCookie == nil {
			t.Error("expected 'session_id' cookie to be present in response headers")
			return
		}
		if sessionCookie.Value == "" {
			t.Error("expected session cookie token value to be populated")
		}
		if !sessionCookie.HttpOnly {
			t.Error("security breach: expected session cookie to be HttpOnly")
		}
		if !sessionCookie.Secure {
			t.Error("security breach: expected session cookie to have Secure flag")
		}
		if sessionCookie.SameSite != http.SameSiteStrictMode {
			t.Errorf("expected SameSite Strict, got %v", sessionCookie.SameSite)
		}

		dbSession := new(models.Session)
		err := testDB.NewSelect().
			Model(dbSession).
			Where("session_token = ?", sessionCookie.Value).
			Scan(ctx)

		if err != nil {
			t.Errorf("failed to locate registered session in database: %v", err)
			return
		}
		if dbSession.UserID != expectedUserID {
			t.Errorf("session database row mismatched: expected user ID %d, got %d", expectedUserID, dbSession.UserID)
		}
	}
}
