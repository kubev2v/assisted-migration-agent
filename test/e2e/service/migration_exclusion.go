package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// VMListParamsV1 holds parameters for listing VMs with filters (v1 API)
type VMListParamsV1 struct {
	ByExpression *string
	Sort         []string
	Page         *int
	PageSize     *int
}

// VirtualMachineV1 represents a VM in the v1 API response
type VirtualMachineV1 struct {
	ID                     string   `json:"id"`
	Name                   string   `json:"name"`
	VCenterID              string   `json:"vCenterID"`
	VCenterState           string   `json:"vCenterState"`
	Cluster                string   `json:"cluster"`
	Datacenter             string   `json:"datacenter"`
	DiskSize               int64    `json:"diskSize"`
	Memory                 int64    `json:"memory"`
	IssueCount             int      `json:"issueCount"`
	Migratable             bool     `json:"migratable"`
	Template               bool     `json:"template"`
	MigrationExcluded      bool     `json:"migrationExcluded"`
	InspectionConcernCount int      `json:"inspectionConcernCount"`
	Tags                   []string `json:"tags"`
}

// VirtualMachineListResponseV1 represents the v1 API list response
type VirtualMachineListResponseV1 struct {
	Vms       []VirtualMachineV1 `json:"vms"`
	Total     int                `json:"total"`
	Page      int                `json:"page"`
	PageCount int                `json:"pageCount"`
}

// UpdateVMMigrationExclusion updates the migration exclusion status for a VM
func (a *AgentSvc) UpdateVMMigrationExclusion(vmID string, excluded bool) error {
	if vmID == "" {
		return fmt.Errorf("vmID cannot be empty")
	}

	reqBody := map[string]bool{
		"migrationExcluded": excluded,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequest(
		http.MethodPatch,
		a.baseURL+"/api/v1/vms/"+url.PathEscape(vmID),
		bytes.NewReader(data),
	)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := a.request(NewAgentRequest(req).withHeader("Content-Type", "application/json"))
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

// ListVMsV1 retrieves a list of VMs using the v1 API with optional filters
func (a *AgentSvc) ListVMsV1(params *VMListParamsV1) (*VirtualMachineListResponseV1, error) {
	req, err := http.NewRequest(http.MethodGet, a.baseURL+"/api/v1/vms", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	agentReq := NewAgentRequest(req)
	if params != nil {
		if params.ByExpression != nil {
			agentReq.withQueryParam("byExpression", *params.ByExpression)
		}
		if params.Page != nil {
			agentReq.withQueryParam("page", strconv.Itoa(*params.Page))
		}
		if params.PageSize != nil {
			agentReq.withQueryParam("pageSize", strconv.Itoa(*params.PageSize))
		}
		agentReq.withQueryParamSlice("sort", params.Sort)
	}

	resp, err := a.request(agentReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result VirtualMachineListResponseV1
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &result, nil
}
