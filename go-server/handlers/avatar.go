package handlers

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"dbBackend/models"
)

const maxUploadSize = 2 << 20 // 2 MB

// AvatarUpload handles POST /api/protected/avatar.
// Accepts multipart/form-data with field "avatar", decodes and re-encodes as PNG,
// writes to disk, and updates the user's avatar_url in the database.
func (h *Handler) AvatarUpload(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok || userID == 0 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// 1. Size limit (2 MB)
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	file, _, err := r.FormFile("avatar")
	if err != nil {
		slog.Warn("avatar upload rejected", "user_id", userID, "error", err)
		http.Error(w, "Problem retrieving uploaded file (max size 2MB)", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// 2. Decode image (verifies valid image data: PNG, JPEG, GIF)
	img, _, err := image.Decode(file)
	if err != nil {
		slog.Warn("avatar upload decode failed", "user_id", userID, "error", err)
		http.Error(w, "Invalid or unsupported image format", http.StatusBadRequest)
		return
	}

	// 3. Ensure uploads directory exists
	uploadDir := h.GetUploadsDir()
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		slog.Error("failed to create uploads directory", "dir", uploadDir, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// 4. Generate safe synthetic filename with .png extension
	filename := fmt.Sprintf("avatar_%d_%d.png", userID, time.Now().UnixNano())
	outPath := filepath.Join(uploadDir, filename)

	outFile, err := os.Create(outPath)
	if err != nil {
		slog.Error("failed to create avatar file", "path", outPath, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer outFile.Close()

	// 5. Re-encode as PNG into outFile
	if err := png.Encode(outFile, img); err != nil {
		slog.Error("failed to encode avatar PNG", "path", outPath, "error", err)
		_ = os.Remove(outPath) // clean up partial file on failure
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// 6. Update user's avatar_url in the database
	avatarURL := "/api/uploads/" + filename
	now := time.Now()
	profile := new(models.UserProfile)
	err = h.DB.NewUpdate().
		Model((*models.User)(nil)).
		Where("id = ?", userID).
		Set("updated_at = ?", now).
		Set("avatar_url = ?", avatarURL).
		Returning("username, email, bio, avatar_url").
		Scan(r.Context(), profile)

	if err != nil {
		slog.Error("failed to update user avatar_url", "user_id", userID, "error", err)
		_ = os.Remove(outPath) // rollback disk write if DB update fails
		HandleDBError(w, err, "User avatar update")
		return
	}

	// 7. Return updated profile (contains username, bio, avatarUrl)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(profile)
}

// ServeUpload handles GET /api/uploads/{filename}.
// Public unauthenticated route to serve uploaded avatar images.
func (h *Handler) ServeUpload(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	if filename == "" {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// Guard against path traversal attacks (e.g. "../../../etc/passwd")
	cleanName := filepath.Base(filename)
	if cleanName != filename || cleanName == "." || cleanName == "/" {
		http.Error(w, "Invalid filename", http.StatusBadRequest)
		return
	}

	filePath := filepath.Join(h.GetUploadsDir(), cleanName)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	http.ServeFile(w, r, filePath)
}
