package handlers

import (
	"dbBackend/models"
	"encoding/json"
	"net/http"
)

// GET /api/protected/messages/{friend_name}
// Retrieves the chat history between the authenticated user and a specific friend.
func (h *Handler) MessagesGetHistory(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	friendNameStr := r.PathValue("friend_name")
	friend, err := h.getUserByUsername(r.Context(), friendNameStr)
	if err != nil {
		http.Error(w, "Invalid friend name", http.StatusBadRequest)
		return
	}

	//get all entries in the messages table, where the corresponding pair matches
	var messages []models.Message
	err = h.DB.NewSelect().
		Model(&messages).
		Where("(sender_id = ? AND recipient_id = ?) OR (sender_id = ? AND recipient_id = ?)",
			userID, friend.ID, friend.ID, userID).
		Order("created_at ASC").
		Limit(100). //dynamic sizing TODO
		Scan(r.Context())

	if err != nil {
		http.Error(w, "Database error fetching messages: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if messages == nil {
		messages = make([]models.Message, 0)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(messages)
}

// POST /api/internal/messages
// Internal endpoint used by the WebSocket microservice to persist chat messages.
func (h *Handler) MessageCreate(w http.ResponseWriter, r *http.Request) {
	input, err := DecodeAndValidate[models.MessageCreateInput](r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	message := &models.Message{
		SenderID:    input.SenderID,
		RecipientID: input.RecipientID,
		Content:     input.Content,
		IsRead:      false,
	}

	err = h.DB.NewInsert().
		Model(message).
		Returning("*").
		Scan(r.Context())

	if err != nil {
		http.Error(w, "Failed to create message: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(message)
}

// PATCH /api/protected/messages/{friend_name}/read
// set all messages from authenticated user to specified friend as Read, up to a message ID specified in request
func (h *Handler) MessageSetRead(w http.ResponseWriter, r *http.Request) {
	//user ID injecteced by middleware
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	//friend ID retrieved by path value
	friendNameStr := r.PathValue("friend_name")
	friend, err := h.getUserByUsername(r.Context(), friendNameStr)
	if err != nil {
		http.Error(w, "Invalid friend name", http.StatusBadRequest)
		return
	}
	input, err := DecodeAndValidate[models.MessageSetReadInput](r) //input now contains ReadUpTo
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	query := h.DB.NewUpdate().
		Model((*models.Message)(nil)).
		Where("recipient_id = ?", userID). //messages to this user
		Where("sender_id = ?", friend.ID). //from this friend
		Where("is_read = FALSE").          //don't bother with already read messages
		Where("id <= ?", input.ReadUpTo).  //update all messages up to the reference one.
		Set("is_read = TRUE")

	_, err = query.Exec(r.Context())
	if err != nil {
		HandleDBError(w, err, "Updating messages to read")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
