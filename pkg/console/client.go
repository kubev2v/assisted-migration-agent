package console

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
	apiAgent "github.com/kubev2v/migration-planner/api/v1alpha1/agent"
	agentClient "github.com/kubev2v/migration-planner/pkg/client"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	serviceErrs "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

type Client struct {
	baseURL    string
	httpClient *agentClient.Client
}

func NewConsoleClient(baseURL string, jwt string) (*Client, error) {
	httpClient, err := agentClient.NewClient(baseURL, agentClient.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
		if jwt == "" {
			return nil
		}
		req.Header.Add("Authorization", fmt.Sprintf("Bearer: %s", jwt))
		return nil
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to initialize console client: %v", err)
	}
	return &Client{
		baseURL:    baseURL,
		httpClient: httpClient,
	}, nil
}

// UpdateAgentStatus sends agent status to console.redhat.com
// PUT /api/v1/agents/{id}/status
func (c *Client) UpdateAgentStatus(ctx context.Context, agentID uuid.UUID, sourceID uuid.UUID, version string, collectorStatus models.CollectorStatusType) error {
	body := apiAgent.AgentStatusUpdate{
		Status:     string(collectorStatus),
		StatusInfo: string(collectorStatus),
		SourceId:   sourceID,
		Version:    version,
	}

	resp, err := c.httpClient.UpdateAgentStatus(ctx, agentID, body)
	if err != nil {
		return err
	}
	if resp != nil {
		defer resp.Body.Close()
	}

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusCreated:
		return nil
	case http.StatusGone:
		return serviceErrs.NewSourceGoneError(sourceID)
	case http.StatusUnauthorized:
		return serviceErrs.NewAgentUnauthorized()
	default:
		return fmt.Errorf("failed to update agent status: %s", resp.Status)
	}
}

// UpdateSourceStatus sends source inventory to console.redhat.com
// PUT /api/v1/sources/{id}/status
func (c *Client) UpdateSourceStatus(ctx context.Context, sourceID uuid.UUID, inventory io.Reader) error {
	resp, err := c.httpClient.UpdateSourceInventoryWithBody(ctx, sourceID, "application/json", inventory)
	if err != nil {
		return err
	}
	if resp != nil {
		defer resp.Body.Close()
	}

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent:
		return nil
	case http.StatusUnauthorized:
		return serviceErrs.NewAgentUnauthorized()
	default:
		return fmt.Errorf("failed to update source inventory: %s", resp.Status)
	}
}
