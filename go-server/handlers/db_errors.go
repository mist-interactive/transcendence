package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/uptrace/bun/driver/pgdriver"
)

// HandleDBError inspects the database error and writes the appropriate HTTP response.
func HandleDBError(w http.ResponseWriter, err error, resourceName string) {
	if errors.Is(err, sql.ErrNoRows) {
		slog.Warn("db record not found", "resource", resourceName)
		http.Error(w, fmt.Sprintf("'%s' not found", resourceName), http.StatusNotFound)
		return
	}

	if pgErr, ok := err.(pgdriver.Error); ok {
		errMsg := pgErr.Error()
		code := pgErr.Field('C')
		switch code {
		case "23505": // unique_violation
			slog.Warn("db unique constraint violation", "resource", resourceName, "sqlstate", code, "error", errMsg)
			http.Error(w, fmt.Sprintf("Conflict: '%s' , error: '%s'", resourceName, errMsg), http.StatusConflict)
			return

		case "23503": // foreign_key_violation
			slog.Warn("db foreign key constraint violation", "resource", resourceName, "sqlstate", code, "error", errMsg)
			http.Error(w, fmt.Sprintf("Referenced record does not exist, '%s'", errMsg), http.StatusBadRequest)
			return

		case "23514": // check_violation
			slog.Warn("db check constraint violation", "resource", resourceName, "sqlstate", code, "error", errMsg)
			http.Error(w, fmt.Sprintf("Invalid data violates constraints, '%s'", errMsg), http.StatusBadRequest)
			return
		}
		slog.Error("db postgres error", "resource", resourceName, "sqlstate", code, "error", errMsg)
	} else {
		slog.Error("db unexpected error", "resource", resourceName, "error", err)
	}

	//fallback
	http.Error(w, "Internal Server Error", http.StatusInternalServerError)
}
