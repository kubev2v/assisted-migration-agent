package console

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/tupyy/assisted-migration-agent/internal/models"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

// AgentStatusUpdate matches the remote API schema
type AgentStatusUpdate struct {
	Status        string `json:"status"`
	StatusInfo    string `json:"statusInfo"`
	CredentialUrl string `json:"credentialUrl"`
	Version       string `json:"version"`
	SourceId      string `json:"sourceId"`
}

// SourceStatusUpdate matches the remote API schema
type SourceStatusUpdate struct {
	Inventory any    `json:"inventory"`
	AgentId   string `json:"agentId"`
}

func NewConsoleClient(baseURL string) *Client {
	httpClient := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     false,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
	return &Client{
		baseURL:    baseURL,
		httpClient: httpClient,
	}
}

// UpdateAgentStatus sends agent status to console.redhat.com
// PUT /api/v1/agents/{id}/status
func (c *Client) UpdateAgentStatus(ctx context.Context, agentID string, sourceID string, collectorStatus models.CollectorStatus) error {
	url := fmt.Sprintf("%s/api/v1/agents/%s/status", c.baseURL, agentID)

	body, err := json.Marshal(AgentStatusUpdate{
		Status:     string(collectorStatus),
		StatusInfo: "",
		SourceId:   sourceID,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal agent status: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

// UpdateSourceStatus sends source inventory to console.redhat.com
// PUT /api/v1/sources/{id}/status
func (c *Client) UpdateSourceStatus(ctx context.Context, sourceID string, update SourceStatusUpdate) error {
	url := fmt.Sprintf("%s/api/v1/sources/%s/status", c.baseURL, sourceID)

	body, err := json.Marshal(update)
	if err != nil {
		return fmt.Errorf("failed to marshal source status: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}
