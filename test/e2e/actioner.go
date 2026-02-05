package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
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
