package service

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/kubev2v/migration-planner/api/v1alpha1"
	"go.uber.org/zap"
)

// --- Agent API request/response types ---

type AgentModeRequest struct {
	Mode string `json:"mode"`
}

type AgentStatus struct {
	Mode              string `json:"mode"`
	ConsoleConnection string `json:"console_connection"`
	Error             string `json:"error,omitempty"`
}

type CollectorStartRequest struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type CollectorStatus struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// --- AgentSvc client ---

// AgentSvc provides a client to interact with the Planner Agent API
type AgentSvc struct {
	baseURL    string
	httpClient *http.Client
}

// DefaultAgentSvc creates an AgentSvc client with a default HTTP client that skips TLS verification
func DefaultAgentSvc(agentApiBaseUrl string) *AgentSvc {
	return NewAgentSvc(agentApiBaseUrl, &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	})
}

// NewAgentSvc creates an AgentSvc client with a custom HTTP client
func NewAgentSvc(agentApiBaseUrl string, customHttpClient *http.Client) *AgentSvc {
	return &AgentSvc{
		baseURL:    agentApiBaseUrl,
		httpClient: customHttpClient,
	}
}

// Status retrieves the current status of the agent
func (a *AgentSvc) Status() (*AgentStatus, error) {
	req, err := http.NewRequest(http.MethodGet, a.baseURL+"/api/v1/agent", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var status AgentStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	zap.S().Infof("mode: %s. Console connection: %s. error: %s", status.Mode, status.ConsoleConnection, status.Error)
	return &status, nil
}

// SetAgentMode sets the agent mode (connected/disconnected)
func (a *AgentSvc) SetAgentMode(mode string) (*AgentStatus, error) {
	body := AgentModeRequest{Mode: mode}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, a.baseURL+"/api/v1/agent", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var status AgentStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &status, nil
}

// StartCollector starts the collector with the given vCenter credentials
func (a *AgentSvc) StartCollector(vcenterURL, username, password string) (*CollectorStatus, error) {
	body := CollectorStartRequest{
		URL:      vcenterURL,
		Username: username,
		Password: password,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, a.baseURL+"/api/v1/collector", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var status CollectorStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &status, nil
}

// GetCollectorStatus retrieves the current collector status
func (a *AgentSvc) GetCollectorStatus() (*CollectorStatus, error) {
	req, err := http.NewRequest(http.MethodGet, a.baseURL+"/api/v1/collector", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var status CollectorStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &status, nil
}

// Inventory retrieves the inventory data collected by the agent
func (a *AgentSvc) Inventory() (*v1alpha1.Inventory, error) {
	req, err := http.NewRequest(http.MethodGet, a.baseURL+"/api/v1/inventory", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var inventory v1alpha1.Inventory
	if err := json.NewDecoder(resp.Body).Decode(&inventory); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &inventory, nil
}
