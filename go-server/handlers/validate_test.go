package handlers_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"dbBackend/handlers"
	"dbBackend/models"
)

func TestDecodeAndValidate_RegisterRequest(t *testing.T) {
	tests := []struct {
		name        string
		jsonBody    string
		expectError bool
	}{
		{
			name:        "Valid Request",
			jsonBody:    `{"username": "paavo", "email": "paavo@pesusieni.fi", "password": "Password123"}`,
			expectError: false,
		},
		{
			name:        "Invalid JSON Syntax",
			jsonBody:    `{"username": "paavo", "email":`,
			expectError: true,
		},
		{
			name:        "Username Too Short",
			jsonBody:    `{"username": "p", "email": "paavo@pesusieni.fi", "password": "Password123"}`,
			expectError: true,
		},
		{
			name:        "Invalid Email Format",
			jsonBody:    `{"username": "paavo", "email": "invalid-email-no-at", "password": "Password123"}`,
			expectError: true,
		},
		{
			name:        "Password Too Short",
			jsonBody:    `{"username": "paavo", "email": "paavo@pesusieni.fi", "password": "P1!"}`,
			expectError: true,
		},
		{
			name:        "Password Complexity Fails (Only Lowercase)",
			jsonBody:    `{"username": "paavo", "email": "paavo@pesusieni.fi", "password": "justlowercase"}`,
			expectError: true,
		},
		{
			name:        "Password Complexity Passes (Lower + Numbers)",
			jsonBody:    `{"username": "paavo", "email": "paavo@pesusieni.fi", "password": "lowercase123"}`,
			expectError: false,
		},
		{
			name:        "Password Complexity Passes (Upper + Special)",
			jsonBody:    `{"username": "paavo", "email": "paavo@pesusieni.fi", "password": "UPPERCASE!!!"}`,
			expectError: false,
		},
		{
			name:        "Username contains invalid characters (only [a-zA-Z0-9_-] allowed)",
			jsonBody:    `{"username": "paavo on paras!", "email": "paavo@pesusieni.fi", "password": "UPPERCASE!!!"}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/register", strings.NewReader(tt.jsonBody))
			req.Header.Set("Content-Type", "application/json")
			_, err := handlers.DecodeAndValidate[models.RegisterRequest](req)
			if tt.expectError && err == nil {
				t.Errorf("expected an error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("did not expect an error but got: %v", err)
			}
		})
	}
}
