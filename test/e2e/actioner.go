package main

import (
	"bytes"
	"crypto/rsa"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// BackendActioner performs HTTP requests against the migration-planner backend API.
type BackendActioner struct {
	client     *http.Client
	backendURL string
}

func NewBackendActioner(backendURL string) *BackendActioner {
	return &BackendActioner{
		client: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
		backendURL: backendURL,
	}
}

type SourceCreateRequest struct {
	Name string `json:"name"`
}

type SourceResponse struct {
	Id        string             `json:"id"`
	Name      string             `json:"name"`
	Agent     *AgentResponse     `json:"agent,omitempty"`
	Inventory *InventoryResponse `json:"inventory,omitempty"`
}

type AgentResponse struct {
	Id     string `json:"id"`
	Status string `json:"status"`
}

type InventoryResponse struct {
	VcenterId string                 `json:"vcenter_id"`
	Clusters  map[string]interface{} `json:"clusters,omitempty"`
}

func (b *BackendActioner) CreateSource(name string) (string, error) {
	body := SourceCreateRequest{Name: name}
	data, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, b.backendURL+"/api/v1/sources", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var source SourceResponse
	if err := json.NewDecoder(resp.Body).Decode(&source); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}

	return source.Id, nil
}

func (b *BackendActioner) GetSource(id string) (*SourceResponse, error) {
	req, err := http.NewRequest(http.MethodGet, b.backendURL+"/api/v1/sources/"+id, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var source SourceResponse
	if err := json.NewDecoder(resp.Body).Decode(&source); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &source, nil
}

func (b *BackendActioner) DeleteSource(id string) error {
	req, err := http.NewRequest(http.MethodDelete, b.backendURL+"/api/v1/sources/"+id, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

type Actioner struct {
	client   *http.Client
	agentURL string
}

func NewActioner(agentURL string) *Actioner {
	return &Actioner{
		client: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
		agentURL: agentURL,
	}
}

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

func (a *Actioner) GetAgentStatus() (*AgentStatus, error) {
	req, err := http.NewRequest(http.MethodGet, a.agentURL+"/api/v1/agent", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := a.client.Do(req)
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

func (a *Actioner) WaitForReady(timeout time.Duration) error {
	return WaitForReady(a.client, a.agentURL+"/api/v1/agent", timeout)
}

func WaitForReady(client *http.Client, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("endpoint %s not ready within %v", url, timeout)
}

func (a *Actioner) SetAgentMode(mode string) (*AgentStatus, error) {
	body := AgentModeRequest{Mode: mode}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, a.agentURL+"/api/v1/agent", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
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

func (a *Actioner) StartCollector(vcenterURL, username, password string) (*CollectorStatus, error) {
	body := CollectorStartRequest{
		URL:      vcenterURL,
		Username: username,
		Password: password,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, a.agentURL+"/api/v1/collector", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
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

func (a *Actioner) GetCollectorStatus() (*CollectorStatus, error) {
	req, err := http.NewRequest(http.MethodGet, a.agentURL+"/api/v1/collector", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := a.client.Do(req)
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

func (a *Actioner) GetInventory() (map[string]interface{}, error) {
	req, err := http.NewRequest(http.MethodGet, a.agentURL+"/api/v1/inventory", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := a.client.Do(req)
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

	var inventory map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&inventory); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return inventory, nil
}

func (a *Actioner) GenerateAgentToken(sourceID, kid string, privateKey *rsa.PrivateKey) (string, error) {
	type AgentTokenClaims struct {
		SourceID string `json:"source_id"`
		jwt.RegisteredClaims
	}

	claims := AgentTokenClaims{
		sourceID,
		jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(30 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "assisted-migrations",
			Subject:   sourceID,
			ID:        "1",
			Audience:  []string{"assisted-migrations"},
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	signedToken, err := token.SignedString(privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign agent token: %w", err)
	}

	return signedToken, nil
}
