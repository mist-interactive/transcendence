package handlers

import (
	"dbBackend/models"
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func (h *Handler) TryRegister(w http.ResponseWriter, r *http.Request) {
	request, err := DecodeAndValidate[models.RegisterRequest](r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	hashedBytes, _ := bcrypt.GenerateFromPassword([]byte(request.Password), bcrypt.DefaultCost)
	newUser := models.User{
		Username: request.Username,
		Email:    strings.ToLower(request.Email),
		PWHash:   string(hashedBytes),
	}
	_, err = h.DB.NewInsert().
		Model(&newUser).
		Exec(r.Context())
	if err != nil {
		HandleDBError(w, err, "Insert User")
		return
	}
	w.WriteHeader(http.StatusCreated)
}
