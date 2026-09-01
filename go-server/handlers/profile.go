package handlers

import (
	"dbBackend/models"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

func (h *Handler) ProfileGet(w http.ResponseWriter, r *http.Request) {
	log.Println("--> ProfileGet hit!")

	userID, ok := UserIDFromContext(r.Context())
	if !ok || userID == 0 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	profile := new(models.UserProfile)
	err := h.DB.NewSelect().
		Table("users").
		Where("id = ?", userID).
		Column("username", "email", "bio", "avatar_url").
		Scan(r.Context(), profile)

	if err != nil {
		HandleDBError(w, err, "User profile get")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(profile)
}

func (h *Handler) ProfilePatch(w http.ResponseWriter, r *http.Request) {
	//first, check what middleware passed and what input contains
	userID, ok := UserIDFromContext(r.Context())
	if !ok || userID == 0 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	input, err := DecodeAndValidate[models.ProfilePatchInput](r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if isProfilePatchEmpty(&input) {
		http.Error(w, "At least one field must be provided", http.StatusBadRequest)
		return
	}
	//everything was ok, start building the update query, basics first
	now := time.Now()
	query := h.DB.NewUpdate().
		Model((*models.User)(nil)).
		Where("id = ?", userID).
		Set("updated_at = ?", now)
	// for each field, if it's not nil, add it to the update
	if input.Bio != nil {
		query = query.Set("bio = ?", *input.Bio)
	}
	if input.Email != nil {
		query = query.Set("email = ?", *input.Email)
	}
	if input.AvatarURL != nil {
		query = query.Set("avatar_url = ?", *input.AvatarURL)
	}
	//Do the update while scanning data to memory
	profile := new(models.UserProfile)
	err = query.Returning("username, email, bio, avatar_url").Scan(r.Context(), profile)
	if err != nil {
		HandleDBError(w, err, "User profile get")
		return
	}
	//Return the updated profile data
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(profile)
}

func isProfilePatchEmpty(p *models.ProfilePatchInput) bool {
	return p.Bio == nil && p.AvatarURL == nil && p.Email == nil
}

func (h *Handler) ProfileGetByUsername(w http.ResponseWriter, r *http.Request) {
	userStr := r.PathValue("username")
	log.Printf("Getting profile with username '%s'\n", userStr)
	if userStr == "" {
		http.Error(w, "No username provided", http.StatusBadRequest)
		return
	}
	profile := new(models.UserProfile)
	err := h.DB.NewSelect().
		Table("users").
		Where("username = ?", userStr).
		Column("username", "email", "bio", "avatar_url").
		Scan(r.Context(), profile)

	if err != nil {
		HandleDBError(w, err, fmt.Sprintf("User profile get by username '%s'", userStr))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(profile)
}

func (h *Handler) ProfileDelete(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok || userID == 0 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	log.Printf("Deleting user '%d'\n", userID)
	ctx := r.Context()

	// Begin a transaction: a connected set of database actions, that can be undone if any of them goes wrong
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback() //register the Rollback to run if we exit befor committing the transaction
	anonymized := fmt.Sprintf("deleted_user_%d", userID)
	now := time.Now()

	// Add things to the transaction
	// 1. anonymize the users table entry
	updateUserQuery := tx.NewUpdate().
		Table("users").
		Where("id = ?", userID).
		Set("username = ?", anonymized).
		Set("email = ?", anonymized+"@internal").
		Set("password_hash = ?", "deleted").
		Set("bio = ''").
		Set("avatar_url = NULL").
		Set("updated_at = ?", now)
	// 2. delete any active sessions
	deleteSessionsQuery := tx.NewDelete().
		Table("sessions").
		Where("user_id = ?", userID)

	// Execute transactions
	if _, err := updateUserQuery.Exec(ctx); err != nil {
		HandleDBError(w, err, fmt.Sprintf("User deletion '%d'", userID))
		return
	}
	if _, err := deleteSessionsQuery.Exec(ctx); err != nil {
		HandleDBError(w, err, fmt.Sprintf("Session deletion for user '%d'", userID))
		return
	}

	//everything went through, so we commit all actions
	if err := tx.Commit(); err != nil {
		HandleDBError(w, err, fmt.Sprintf("Committing profile deletion transaction for user '%d'", userID))
		return
	}

	//Set a non-valid Cookie to replace the old one
	ClearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}
