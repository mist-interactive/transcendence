package handlers

import (
	"crypto/rsa"
	"dbBackend/models"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/uptrace/bun"
)

type Group struct {
	mux        *http.ServeMux
	prefix     string
	middleware func(http.Handler) http.Handler
}

func NewGroup(mux *http.ServeMux, prefix string, middleware func(http.Handler) http.Handler) *Group {
	return &Group{
		mux:        mux,
		prefix:     strings.TrimSuffix(prefix, "/"),
		middleware: middleware,
	}
}

// helper to wrap functions in middleware
func (g *Group) HandleFunc(pattern string, handler http.HandlerFunc) {
	parts := strings.SplitN(pattern, " ", 2)
	var fullPattern string

	if len(parts) == 2 {
		method := parts[0]
		path := "/" + strings.TrimPrefix(parts[1], "/")
		fullPattern = method + " " + g.prefix + path
	} else {
		path := "/" + strings.TrimPrefix(parts[0], "/")
		fullPattern = g.prefix + path
	}
	g.mux.Handle(fullPattern, g.middleware(handler))
}

// EventNotifier defines an interface for delivering real-time events to users.
type EventNotifier interface {
	NotifyFriendRequest(targetUserID int64, item models.FriendshipItemResponse) error
	NotifyFriendResponse(targetUserID int64, item models.FriendshipItemResponse) error
}

type Handler struct {
	DB         *bun.DB
	PrivateKey *rsa.PrivateKey
	PublicKey  *rsa.PublicKey
	APIKey     string
	Notifier   EventNotifier
	UploadsDir string
}

func (h *Handler) GetUploadsDir() string {
	if h.UploadsDir != "" {
		return h.UploadsDir
	}
	return "./uploads"
}

func NewHandler(db *bun.DB, privKey *rsa.PrivateKey, pubKey *rsa.PublicKey, apiKey string, notifier EventNotifier) *Handler {
	return &Handler{
		DB:         db,
		PrivateKey: privKey,
		PublicKey:  pubKey,
		APIKey:     apiKey,
		Notifier:   notifier,
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/login", h.CheckPassword)
	mux.HandleFunc("POST /api/register", h.TryRegister)
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy"}`))
	})
	//requires session token
	mux.Handle("POST /api/renew", h.SessionGuard(http.HandlerFunc(h.IssueToken)))

	mux.HandleFunc("GET /api/uploads/{filename}", h.ServeUpload)

	// all /api/protected/* routes require a valid JWT
	protected := NewGroup(mux, "/api/protected", h.JWTGuard)
	protected.HandleFunc("GET /profile", h.ProfileGet)
	protected.HandleFunc("PATCH /profile", h.ProfilePatch)
	protected.HandleFunc("GET /profile/{username}", h.ProfileGetByUsername)
	protected.HandleFunc("DELETE /profile", h.ProfileDelete)

	protected.HandleFunc("POST /avatar", h.AvatarUpload)

	protected.HandleFunc("POST /friends", h.FriendRequestPost)
	protected.HandleFunc("GET /friends", h.FriendsListGet)
	protected.HandleFunc("PATCH /friends/{id}", h.FriendRequestAnswer)
	protected.HandleFunc("DELETE /friends/{id}", h.FriendDelete)

	protected.HandleFunc("GET /messages/{friend_name}", h.MessagesGetHistory)
	protected.HandleFunc("PATCH /messages/{friend_name}/read", h.MessageSetRead)

	// Internal routes protected by APIGuard

	internal := NewGroup(mux, "/api/internal", h.APIGuard)

	internal.HandleFunc("POST /matches", h.MatchCreate)
	internal.HandleFunc("PATCH /matches/{id}", h.MatchPatch)

	// WS service internal endpoints
	internal.HandleFunc("GET /friends/{id}", InjectPathIDContext(h.FriendsListGet))
	internal.HandleFunc("POST /messages", h.MessageCreate)
}

func GetAPIKey() (string, error) {
	keyPath := os.Getenv("GAMESERVER_API_KEY_PATH")
	if keyPath == "" {
		return "", fmt.Errorf("Critical: GAMESERVER_API_KEY_PATH is not set")
	}
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return "", fmt.Errorf("Critical: Failed to read API key file at %s: %v", keyPath, err)
	}

	key := strings.TrimSpace(string(keyBytes))
	if len(key) < 32 {
		return "", fmt.Errorf("Critical: Unexpected length of key : %d, expected >=32", len(key))
	}
	return key, nil
}

func GetPrivateKey() (*rsa.PrivateKey, error) {
	keyPath := os.Getenv("JWT_PRIVATE_KEY_PATH")
	pemBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("Critical: Failed to read JWT private key file at %s: %v", keyPath, err)
	}
	signKey, err := jwt.ParseRSAPrivateKeyFromPEM(pemBytes)
	if err != nil {
		return nil, fmt.Errorf("Critical: Failed to parse JWT private key layout: %v", err)
	}
	return signKey, nil
}

func GetPublicKey() (*rsa.PublicKey, error) {
	pubPath := os.Getenv("JWT_PUBLIC_KEY_PATH")
	pubBytes, err := os.ReadFile(pubPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open public key verify file: %v", err)
	}
	publicKey, err := jwt.ParseRSAPublicKeyFromPEM(pubBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to cryptographically compile RSA public key: %v", err)
	}
	return publicKey, nil
}

func (h *Handler) TokenValidator(tokenStr string) (int64, string, error) {
	claims, err := ValidateToken(tokenStr, h.PublicKey)
	if err != nil {
		return 0, "", err
	}
	return claims.UserID, claims.Username, nil
}
