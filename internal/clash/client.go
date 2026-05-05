package clash

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// CredsFunc returns the current Mihomo Controller URL and Bearer secret.
// It is consulted on every call so credential rotation through the UI takes
// effect immediately, without restarting the collector.
type CredsFunc func() (baseURL string, secret string)

type Client struct {
	httpClient *http.Client
	creds      CredsFunc
}

func NewClient(creds CredsFunc) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		creds:      creds,
	}
}

func (c *Client) FetchConnections(ctx context.Context) (*ConnectionsResponse, error) {
	base, secret := c.creds()
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return nil, errors.New("mihomo url is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/connections", nil)
	if err != nil {
		return nil, err
	}
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mihomo /connections: status %d", resp.StatusCode)
	}
	var out ConnectionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode /connections: %w", err)
	}
	return &out, nil
}
