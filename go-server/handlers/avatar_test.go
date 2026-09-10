package handlers_test

import (
	"bytes"
	"context"
	"dbBackend/handlers"
	"dbBackend/internal/testutil"
	"dbBackend/models"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func buildMultipartRequest(t *testing.T, fieldName, filename string, data []byte) *http.Request {
	t.Helper()
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	if fieldName != "" {
		part, err := writer.CreateFormFile(fieldName, filename)
		if err != nil {
			t.Fatalf("failed to create form file: %v", err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatalf("failed to write form file data: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/protected/avatar", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func TestAvatarUpload(t *testing.T) {
	ctx := context.Background()
	user, cleanupUser := testutil.MakeTestUser(t, testDB)
	t.Cleanup(cleanupUser)
	testutil.RegisterUser(t, user, testDB)

	tempUploads := t.TempDir()
	handler := handlers.NewHandler(testDB, nil, nil, "", nil)
	handler.UploadsDir = tempUploads

	pngData, err := os.ReadFile("testdata/avatar.png")
	if err != nil {
		t.Fatalf("failed to read test PNG fixture: %v", err)
	}
	jpegData, err := os.ReadFile("testdata/avatar.jpg")
	if err != nil {
		t.Fatalf("failed to read test JPEG fixture: %v", err)
	}

	tests := []struct {
		name           string
		userID         int64
		prepareReq     func(t *testing.T) *http.Request
		expectedStatus int
		checkBody      func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{
			name:   "Success: Valid PNG avatar upload",
			userID: user.ID,
			prepareReq: func(t *testing.T) *http.Request {
				return buildMultipartRequest(t, "avatar", "test.png", pngData)
			},
			expectedStatus: http.StatusOK,
			checkBody: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var profile models.UserProfile
				if err := json.Unmarshal(rec.Body.Bytes(), &profile); err != nil {
					t.Fatalf("failed to decode response profile: %v", err)
				}
				if profile.AvatarURL == nil || !strings.HasPrefix(*profile.AvatarURL, "/api/uploads/avatar_") {
					t.Errorf("expected avatarUrl with /api/uploads/avatar_ prefix, got %v", profile.AvatarURL)
				}
				if !strings.HasSuffix(*profile.AvatarURL, ".png") {
					t.Errorf("expected avatarUrl ending with .png, got %v", profile.AvatarURL)
				}

				// Check file exists on disk in uploads directory
				filename := filepath.Base(*profile.AvatarURL)
				diskPath := filepath.Join(tempUploads, filename)
				if _, err := os.Stat(diskPath); err != nil {
					t.Errorf("uploaded file does not exist on disk at %s: %v", diskPath, err)
				}

				// Check DB user profile was updated
				var dbUser models.User
				if err := testDB.NewSelect().Model(&dbUser).Where("id = ?", user.ID).Scan(ctx); err != nil {
					t.Fatalf("failed to query user from db: %v", err)
				}
				if dbUser.AvatarURL == nil || *dbUser.AvatarURL != *profile.AvatarURL {
					t.Errorf("db avatar_url mismatch: got %v, want %v", dbUser.AvatarURL, *profile.AvatarURL)
				}
			},
		},
		{
			name:   "Success: Valid JPEG converted to PNG",
			userID: user.ID,
			prepareReq: func(t *testing.T) *http.Request {
				return buildMultipartRequest(t, "avatar", "photo.jpg", jpegData)
			},
			expectedStatus: http.StatusOK,
			checkBody: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var profile models.UserProfile
				if err := json.Unmarshal(rec.Body.Bytes(), &profile); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if profile.AvatarURL == nil || !strings.HasSuffix(*profile.AvatarURL, ".png") {
					t.Errorf("expected JPEG to be re-encoded as .png, got %v", profile.AvatarURL)
				}
			},
		},
		{
			name:   "Failure: Unauthorized when user ID missing from context",
			userID: 0,
			prepareReq: func(t *testing.T) *http.Request {
				return buildMultipartRequest(t, "avatar", "test.png", pngData)
			},
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:   "Failure: Missing avatar form field",
			userID: user.ID,
			prepareReq: func(t *testing.T) *http.Request {
				return buildMultipartRequest(t, "wrong_field", "test.png", pngData)
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:   "Failure: Corrupt / non-image data",
			userID: user.ID,
			prepareReq: func(t *testing.T) *http.Request {
				return buildMultipartRequest(t, "avatar", "corrupt.png", []byte("not a real image"))
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:   "Failure: Payload exceeds 2MB limit",
			userID: user.ID,
			prepareReq: func(t *testing.T) *http.Request {
				largeData := make([]byte, (2<<20)+1024) // 2MB + 1KB
				return buildMultipartRequest(t, "avatar", "huge.png", largeData)
			},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := tc.prepareReq(t)
			if tc.userID != 0 {
				req = req.WithContext(handlers.ContextWithUserID(req.Context(), tc.userID))
			}
			rec := httptest.NewRecorder()
			handler.AvatarUpload(rec, req)

			if rec.Code != tc.expectedStatus {
				t.Errorf("got status %d, want %d; body: %s", rec.Code, tc.expectedStatus, rec.Body.String())
			}

			if tc.checkBody != nil {
				tc.checkBody(t, rec)
			}
		})
	}
}

func TestServeUpload(t *testing.T) {
	tempUploads := t.TempDir()
	handler := handlers.NewHandler(testDB, nil, nil, "", nil)
	handler.UploadsDir = tempUploads

	sampleFilename := "avatar_123_456.png"
	sampleData, err := os.ReadFile("testdata/avatar.png")
	if err != nil {
		t.Fatalf("failed to read test PNG fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempUploads, sampleFilename), sampleData, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	tests := []struct {
		name           string
		filenameParam  string
		expectedStatus int
		expectHeader   string
	}{
		{
			name:           "Success: Serve existing PNG avatar",
			filenameParam:  sampleFilename,
			expectedStatus: http.StatusOK,
			expectHeader:   "image/png",
		},
		{
			name:           "Failure: File not found",
			filenameParam:  "does_not_exist.png",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "Failure: Path traversal attempt",
			filenameParam:  "../secret.txt",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/uploads/"+tc.filenameParam, nil)
			req.SetPathValue("filename", tc.filenameParam)
			rec := httptest.NewRecorder()

			handler.ServeUpload(rec, req)

			if rec.Code != tc.expectedStatus {
				t.Errorf("got status %d, want %d; body: %s", rec.Code, tc.expectedStatus, rec.Body.String())
			}
			if tc.expectHeader != "" {
				contentType := rec.Header().Get("Content-Type")
				if !strings.HasPrefix(contentType, tc.expectHeader) {
					t.Errorf("got Content-Type %s, want prefix %s", contentType, tc.expectHeader)
				}
			}
		})
	}
}
