package testutil

import (
	"context"
	"crypto/rand"
	"dbBackend/models"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/uptrace/bun"
)

func MakeTestUser(t *testing.T, testDB *bun.DB) (*models.User, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var uniqueName string = ""
	maxAttempts := 3
	for range maxAttempts {
		candidate := fmt.Sprintf("test_user_%s", generateRandomString(8))
		exists, err := testDB.NewSelect().
			Model((*models.User)(nil)).
			Where("username = ?", candidate).
			Exists(ctx)
		if err != nil {
			t.Fatalf("Error checking database while finding test username: %v", err)
		}
		if !exists {
			uniqueName = candidate
			break
		}
	}
	if uniqueName == "" {
		t.Fatal("Failed to generate unique username")
	}

	return &models.User{
			Username: uniqueName,
			Email:    uniqueName + "@testing.internal",
			PWHash:   "$2b$12$SX55NTDU0FL4DrpQm5kq.OLKcDrrMnS6siaY3Z80.8ki5zagqx08m",
		}, func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cleanupCancel()
			_, err := testDB.NewDelete().
				Model((*models.User)(nil)).
				Where("username = ?", uniqueName).
				Exec(cleanupCtx)
			if err != nil {
				t.Logf("Warning: Failed to clean up test user %s: %v", uniqueName, err)
			}
		}
}

func RegisterUser(t *testing.T, u *models.User, testDB *bun.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := testDB.NewInsert().Model(u).Exec(ctx)
	if err != nil {
		t.Fatalf("Failed to insert test user: %v", err)
	}
}

// CreateRegisteredUser creates a test user with unique credentials,
// inserts them into the database, and registers an automatic test cleanup.
func CreateRegisteredUser(t *testing.T, testDB *bun.DB) *models.User {
	t.Helper()
	u, cleanup := MakeTestUser(t, testDB)
	t.Cleanup(cleanup)
	RegisterUser(t, u, testDB)
	return u
}


func generateRandomString(length int) string {
	bytes := make([]byte, length/2)
	if _, err := rand.Read(bytes); err != nil {
		return "fallback"
	}
	return hex.EncodeToString(bytes)
}
