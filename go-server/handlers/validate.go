package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"unicode"

	"github.com/go-playground/validator/v10"
)

var validate = validator.New()

func init() {
	validate.RegisterValidation("password_complexity", validatePasswordComplexity)
	validate.RegisterValidation("username_safety", validateUsername)
}

func DecodeAndValidate[T any](r *http.Request) (T, error) {
	var request T
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		slog.Warn("payload json decode error", "path", r.URL.Path, "error", err)
		return request, fmt.Errorf("JSON decoder error: %v", err)
	}
	if err := validate.Struct(request); err != nil {
		slog.Warn("payload validation error", "path", r.URL.Path, "error", err)
		return request, fmt.Errorf("Validation error: %v", err)
	}
	return request, nil
}

func validatePasswordComplexity(fl validator.FieldLevel) bool {
	password := fl.Field().String()

	types := make([]bool, 4)
	for _, char := range password {
		switch {
		case unicode.IsLower(char):
			types[0] = true
		case unicode.IsUpper(char):
			types[1] = true
		case unicode.IsDigit(char):
			types[2] = true
		case unicode.IsPunct(char) || unicode.IsSymbol(char):
			types[3] = true
		}
	}

	groupsMatched := 0
	for _, t := range types {
		if t {
			groupsMatched++
		}
	}
	return groupsMatched >= 2
}

var validUsernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func validateUsername(fl validator.FieldLevel) bool {
	return validUsernameRegex.MatchString(fl.Field().String())
}
