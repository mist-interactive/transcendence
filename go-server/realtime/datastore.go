package realtime

import (
	"bytes"
	"context"
	"dbBackend/models"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DataStore defines the interface for persistence operations needed by the WebSocket service.
type DataStore interface {
	GetFriendsList(ctx context.Context, userID int64) ([]int64, error)
}

type HttpDataStore struct {
	BaseURL string
	APIKey  string
	Client  *http.Client
}

func NewHttpDataStore(baseURL, apiKey string) *HttpDataStore {
	return &HttpDataStore{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Client:  &http.Client{Timeout: 5 * time.Second},
	}
}

// doRequest is a generic helper that sends an authenticated HTTP request, checks the status code, and decodes the JSON response
func doRequest[T any](ctx context.Context, s *HttpDataStore, method, path string, body any, expectedStatus int) (*T, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonBytes, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewBuffer(jsonBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, s.BaseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-API-Key", s.APIKey)

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("DB service unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != expectedStatus {
		return nil, fmt.Errorf("request to %s failed with status %d", path, resp.StatusCode)
	}

	var result T
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

func (s *HttpDataStore) GetFriendsList(ctx context.Context, userID int64) ([]int64, error) {
	path := fmt.Sprintf("/api/internal/friends/%d", userID)
	items, err := doRequest[[]models.FriendshipItemResponse](ctx, s, http.MethodGet, path, nil, http.StatusOK)
	if err != nil {
		return nil, err
	}

	var friendIDs []int64
	for _, item := range *items {
		if item.Status == models.StatusAccepted {
			friendIDs = append(friendIDs, item.UserID)
		}
	}
	return friendIDs, nil
}
