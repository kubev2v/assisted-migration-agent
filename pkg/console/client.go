package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/google/uuid"
	v1 "github.com/kubev2v/migration-planner/api/v1alpha1"
	agentAPI "github.com/kubev2v/migration-planner/api/v1alpha1/agent"
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
func (c *Client) UpdateAgentStatus(ctx context.Context, agentID uuid.UUID, sourceID uuid.UUID, version string, collectorStatus models.CollectorStatus) error {
	status := toSaaSStatus(collectorStatus.State)
	statusInfo := status
	if collectorStatus.State == models.CollectorStateError && collectorStatus.Error != nil {
		statusInfo = collectorStatus.Error.Error()
	}

	body := agentAPI.AgentStatusUpdate{
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
		defer func() {
			_ = resp.Body.Close()
		}()
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
func (c *Client) UpdateSourceStatus(ctx context.Context, sourceID, agentID uuid.UUID, data []byte) error {
	inv := v1.Inventory{}
	if err := json.Unmarshal(data, &inv); err != nil {
		return fmt.Errorf("failed to unmarshal inventory: %w", err)
	}

	body := agentAPI.SourceStatusUpdate{
		AgentId:   agentID,
		Inventory: inv,
	}

	resp, err := c.httpClient.UpdateSourceInventory(ctx, sourceID, body)
	if err != nil {
		return err
	}
	if resp != nil {
		defer func() {
			_ = resp.Body.Close()
		}()
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

// UpdateSource sends complete source inventory to migration-planner
// PUT /api/v1/sources/{id}
// Replaces the deprecated UpdateSourceStatus method
func (c *Client) UpdateSource(ctx context.Context, sourceID, agentID uuid.UUID, data []byte) error {
	var inv v1.Inventory
	if err := json.Unmarshal(data, &inv); err != nil {
		return fmt.Errorf("failed to unmarshal inventory: %w", err)
	}

	// Extract vCenter ID from inventory
	var vcenterID *string
	if inv.VcenterId != "" {
		vcenterID = &inv.VcenterId
	}

	body := agentAPI.SourceUpdate{
		VcenterId: vcenterID,
		Inventory: inv,
	}

	resp, err := c.httpClient.UpdateSource(ctx, sourceID, body)
	if err != nil {
		return err
	}
	if resp != nil {
		defer func() {
			_ = resp.Body.Close()
		}()
	}

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return serviceErrs.NewConsoleClientError(resp.StatusCode, resp.Status)
	default:
		return fmt.Errorf("failed to update source: %s", resp.Status)
	}
}

// UpdateSourceSubset creates or updates a subset inventory
// PUT /api/v1/sources/{id}/subset/{subsetId}
func (c *Client) UpdateSourceSubset(ctx context.Context, sourceID, subsetID uuid.UUID, name string, inv v1.Inventory) error {
	if inv.Vcenter == nil {
		return fmt.Errorf("cannot update source subset: vcenter inventory is missing")
	}

	var vcenterID *string
	if inv.VcenterId != "" {
		vcenterID = &inv.VcenterId
	}
	vmsCount := inv.Vcenter.Vms.Total
	body := agentAPI.SourceSubsetUpdate{
		VcenterId: vcenterID,
		Name:      name,
		VmsCount:  &vmsCount,
		Inventory: inv,
	}

	resp, err := c.httpClient.UpdateSourceSubset(ctx, sourceID, subsetID, body)
	if err != nil {
		return err
	}
	if resp != nil {
		defer func() {
			_ = resp.Body.Close()
		}()
	}

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return serviceErrs.NewConsoleClientError(resp.StatusCode, resp.Status)
	default:
		return fmt.Errorf("failed to update source subset: %s", resp.Status)
	}
}

// GetImageUpgradeStatus asks the planner whether a newer appliance image is available
// GET /api/v1/sources/{id}/image-upgrade-status
func (c *Client) GetImageUpgradeStatus(ctx context.Context, sourceID uuid.UUID) (*agentAPI.ImageUpgradeStatus, error) {
	resp, err := c.httpClient.GetImageUpgradeStatus(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		var status agentAPI.ImageUpgradeStatus
		if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
			return nil, fmt.Errorf("failed to decode image upgrade status: %w", err)
		}
		return &status, nil
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return nil, serviceErrs.NewConsoleClientError(resp.StatusCode, resp.Status)
	default:
		return nil, fmt.Errorf("failed to get image upgrade status: %s", resp.Status)
	}
}

// DownloadImage opens the presigned appliance image URL returned by GetImageUpgradeStatus.
// The URL carries its own token, so no agent JWT is sent. The caller closes the body.
func (c *Client) DownloadImage(ctx context.Context, imageURL string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, errors.New("invalid image URL")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// Drop the URL from the error: it contains the download token.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return nil, fmt.Errorf("failed to download image: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("failed to download image: %s", resp.Status)
	}
	return resp.Body, nil
}

func toSaaSStatus(state models.CollectorStateType) string {
	switch state {
	case models.CollectorStateReady:
		return "waiting-for-credentials"
	case models.CollectorStateConnecting,
		models.CollectorStateCollecting,
		models.CollectorStateParsing,
		models.CollectorStateMetricsCollecting:
		return "gathering-initial-inventory"
	case models.CollectorStateCollected:
		return "up-to-date"
	case models.CollectorStateError:
		return "error"
	default:
		return "unknown state"
	}
}

// DeleteSourceSubset deletes a subset inventory
// DELETE /api/v1/sources/{id}/subset/{subsetId}
func (c *Client) DeleteSourceSubset(ctx context.Context, sourceID, subsetID uuid.UUID) error {
	resp, err := c.httpClient.DeleteSourceSubset(ctx, sourceID, subsetID)
	if err != nil {
		return err
	}
	if resp != nil {
		defer func() {
			_ = resp.Body.Close()
		}()
	}

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == 404:
		// Already deleted, consider success (idempotent delete)
		return nil
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return serviceErrs.NewConsoleClientError(resp.StatusCode, resp.Status)
	default:
		return fmt.Errorf("failed to delete source subset: %s", resp.Status)
	}
}
