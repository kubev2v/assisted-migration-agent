package console

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	externalRef0 "github.com/kubev2v/migration-planner/api/v1alpha1"
	apiAgent "github.com/kubev2v/migration-planner/api/v1alpha1/agent"
	agentClient "github.com/kubev2v/migration-planner/pkg/client"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	serviceErrs "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

type Client struct {
	baseURL    string
	httpClient *agentClient.Client
	jwt        string
}

func NewConsoleClient(baseURL string, jwt string) (*Client, error) {
	httpClient, err := agentClient.NewClient(baseURL, agentClient.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
		if jwt == "" {
			return nil
		}
		req.Header.Add("X-Agent-Token", jwt)
		return nil
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to initialize console client: %w", err)
	}
	return &Client{
		baseURL:    baseURL,
		httpClient: httpClient,
		jwt:        jwt,
	}, nil
}

// UpdateAgentStatus sends agent status to console.redhat.com
// PUT /api/v1/agents/{id}/status
func (c *Client) UpdateAgentStatus(ctx context.Context, agentID uuid.UUID, sourceID uuid.UUID, version, status, statusInfo string) error {
	body := apiAgent.AgentStatusUpdate{
		CredentialUrl: "http://10.10.10.1:3443",
		Status:        status,
		StatusInfo:    statusInfo,
		SourceId:      sourceID,
		Version:       version,
	}

	resp, err := c.httpClient.UpdateAgentStatus(ctx, agentID, body)
	if err != nil {
		return err
	}
	if resp != nil {
		defer resp.Body.Close()
	}

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return serviceErrs.NewConsoleClientError(resp.StatusCode, resp.Status)
	default:
		return fmt.Errorf("failed to update agent status: %s", resp.Status)
	}
}

// UpdateSourceStatus sends source inventory to console.redhat.com
// PUT /api/v1/sources/{id}/status
func (c *Client) UpdateSourceStatus(ctx context.Context, sourceID, agentID uuid.UUID, inventory models.Inventory) error {
	inv := externalRef0.Inventory{}
	if err := json.Unmarshal(inventory.Data, &inv); err != nil {
		return fmt.Errorf("failed to unmarshal inventory: %w", err)
	}

	body := apiAgent.SourceStatusUpdate{
		AgentId:   agentID,
		Inventory: inv,
	}

	resp, err := c.httpClient.UpdateSourceInventory(ctx, sourceID, body)
	if err != nil {
		return err
	}
	if resp != nil {
		defer resp.Body.Close()
	}

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return serviceErrs.NewConsoleClientError(resp.StatusCode, resp.Status)
	default:
		return fmt.Errorf("failed to update source inventory: %s", resp.Status)
	}
}
