package service

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/kubev2v/migration-planner/api/v1alpha1"
	"go.uber.org/zap"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
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

type AgentReq struct {
	req              *http.Request
	queryParams      map[string]string
	queryParamSlices map[string][]string
	Headers          map[string]string
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

func NewAgentRequest(req *http.Request) *AgentReq {
	return &AgentReq{
		req:              req,
		queryParams:      make(map[string]string),
		queryParamSlices: make(map[string][]string),
		Headers:          make(map[string]string),
	}
}

// Status retrieves the current status of the agent
func (a *AgentSvc) Status() (*AgentStatus, error) {
	req, err := http.NewRequest(http.MethodGet, a.baseURL+"/api/v1/agent", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := a.request(NewAgentRequest(req))
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

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

	resp, err := a.request(NewAgentRequest(req).withHeader("Content-Type", "application/json"))
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

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

	resp, err := a.request(NewAgentRequest(req).withHeader("Content-Type", "application/json"))
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

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

	resp, err := a.request(NewAgentRequest(req))
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

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
func (a *AgentSvc) Inventory() (*v1alpha1.UpdateInventory, error) {
	req, err := http.NewRequest(http.MethodGet, a.baseURL+"/api/v1/inventory", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := a.request(NewAgentRequest(req).withQueryParam("withAgentId", "true"))
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var inventory v1alpha1.UpdateInventory
	if err := json.NewDecoder(resp.Body).Decode(&inventory); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &inventory, nil
}

// VMListParams holds parameters for listing VMs with filters
type VMListParams struct {
	MinIssues     *int
	Clusters      []string
	Status        []string
	DiskSizeMin   *int64
	DiskSizeMax   *int64
	MemorySizeMin *int64
	MemorySizeMax *int64
	Sort          []string
	Page          *int
	PageSize      *int
}

// ListVMs retrieves a list of VMs with optional filters
func (a *AgentSvc) ListVMs(params *VMListParams) (*v1.VirtualMachineListResponse, error) {
	req, err := http.NewRequest(http.MethodGet, a.baseURL+"/api/v1/vms", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	agentReq := NewAgentRequest(req)
	if params != nil {
		if params.MinIssues != nil {
			agentReq.withQueryParam("minIssues", strconv.Itoa(*params.MinIssues))
		}
		if params.DiskSizeMin != nil {
			agentReq.withQueryParam("diskSizeMin", strconv.FormatInt(*params.DiskSizeMin, 10))
		}
		if params.DiskSizeMax != nil {
			agentReq.withQueryParam("diskSizeMax", strconv.FormatInt(*params.DiskSizeMax, 10))
		}
		if params.MemorySizeMin != nil {
			agentReq.withQueryParam("memorySizeMin", strconv.FormatInt(*params.MemorySizeMin, 10))
		}
		if params.MemorySizeMax != nil {
			agentReq.withQueryParam("memorySizeMax", strconv.FormatInt(*params.MemorySizeMax, 10))
		}
		if params.Page != nil {
			agentReq.withQueryParam("page", strconv.Itoa(*params.Page))
		}
		if params.PageSize != nil {
			agentReq.withQueryParam("pageSize", strconv.Itoa(*params.PageSize))
		}
		agentReq.withQueryParamSlice("clusters", params.Clusters).
			withQueryParamSlice("status", params.Status).
			withQueryParamSlice("sort", params.Sort)
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

	var result v1.VirtualMachineListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &result, nil
}

// GetVM retrieves details for a specific VM by ID
func (a *AgentSvc) GetVM(id string) (*v1.VirtualMachineDetail, error) {
	req, err := http.NewRequest(http.MethodGet, a.baseURL+"/api/v1/vms/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := a.request(NewAgentRequest(req))
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("VM not found: %s", id)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var vm v1.VirtualMachineDetail
	if err := json.NewDecoder(resp.Body).Decode(&vm); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &vm, nil
}

// GroupGetParams holds parameters for getting a group's VMs.
type GroupGetParams struct {
	Sort     []string
	Page     *int
	PageSize *int
}

// CreateGroup creates a new group with the given name, filter, and description.
func (a *AgentSvc) CreateGroup(name, filter, description string) (*v1.Group, error) {
	body := v1.CreateGroupRequest{
		Name:   name,
		Filter: filter,
	}
	if description != "" {
		body.Description = &description
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, a.baseURL+"/api/v1/vms/groups", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := a.request(NewAgentRequest(req).withHeader("Content-Type", "application/json"))
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var group v1.Group
	if err := json.NewDecoder(resp.Body).Decode(&group); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &group, nil
}

// CreateGroupRaw sends a raw POST to /vms/groups and returns the status code.
func (a *AgentSvc) CreateGroupRaw(body []byte) (int, error) {
	req, err := http.NewRequest(http.MethodPost, a.baseURL+"/api/v1/vms/groups", bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("creating request: %w", err)
	}

	resp, err := a.request(NewAgentRequest(req).withHeader("Content-Type", "application/json"))
	if err != nil {
		return 0, fmt.Errorf("sending request: %w", err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

// ListGroups lists all groups.
func (a *AgentSvc) ListGroups() (*v1.GroupListResponse, error) {
	req, err := http.NewRequest(http.MethodGet, a.baseURL+"/api/v1/vms/groups", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := a.request(NewAgentRequest(req))
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result v1.GroupListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}

// GetGroup retrieves a group by ID with its filtered VMs.
func (a *AgentSvc) GetGroup(id string, params *GroupGetParams) (*v1.GroupResponse, error) {
	req, err := http.NewRequest(http.MethodGet, a.baseURL+"/api/v1/vms/groups/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	agentReq := NewAgentRequest(req)
	if params != nil {
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
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("group not found: %s", id)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result v1.GroupResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}

// GetGroupStatus sends a GET to /vms/groups/{id} and returns only the status code.
func (a *AgentSvc) GetGroupStatus(id string) (int, error) {
	req, err := http.NewRequest(http.MethodGet, a.baseURL+"/api/v1/vms/groups/"+url.PathEscape(id), nil)
	if err != nil {
		return 0, fmt.Errorf("creating request: %w", err)
	}

	resp, err := a.request(NewAgentRequest(req))
	if err != nil {
		return 0, fmt.Errorf("sending request: %w", err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

// UpdateGroup partially updates a group via PATCH.
func (a *AgentSvc) UpdateGroup(id string, body v1.UpdateGroupRequest) (*v1.Group, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPatch, a.baseURL+"/api/v1/vms/groups/"+url.PathEscape(id), bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := a.request(NewAgentRequest(req).withHeader("Content-Type", "application/json"))
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("group not found: %s", id)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var group v1.Group
	if err := json.NewDecoder(resp.Body).Decode(&group); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &group, nil
}

// UpdateGroupRaw sends a raw PATCH to /vms/groups/{id} and returns the status code.
func (a *AgentSvc) UpdateGroupRaw(id string, body []byte) (int, error) {
	req, err := http.NewRequest(http.MethodPatch, a.baseURL+"/api/v1/vms/groups/"+url.PathEscape(id), bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("creating request: %w", err)
	}

	resp, err := a.request(NewAgentRequest(req).withHeader("Content-Type", "application/json"))
	if err != nil {
		return 0, fmt.Errorf("sending request: %w", err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

// DeleteGroup deletes a group by ID.
func (a *AgentSvc) DeleteGroup(id string) (int, error) {
	req, err := http.NewRequest(http.MethodDelete, a.baseURL+"/api/v1/vms/groups/"+url.PathEscape(id), nil)
	if err != nil {
		return 0, fmt.Errorf("creating request: %w", err)
	}

	resp, err := a.request(NewAgentRequest(req))
	if err != nil {
		return 0, fmt.Errorf("sending request: %w", err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

func (a *AgentSvc) request(r *AgentReq) (*http.Response, error) {
	if len(r.queryParams) > 0 || len(r.queryParamSlices) > 0 {
		q := r.req.URL.Query()
		for key, value := range r.queryParams {
			q.Set(key, value)
		}
		for key, values := range r.queryParamSlices {
			for _, value := range values {
				q.Add(key, value)
			}
		}
		r.req.URL.RawQuery = q.Encode()
	}

	if len(r.Headers) > 0 {
		for key, value := range r.Headers {
			r.req.Header.Set(key, value)
		}
	}

	return a.httpClient.Do(r.req)
}

func (r *AgentReq) withQueryParam(key, value string) *AgentReq {
	r.queryParams[key] = value
	return r
}

func (r *AgentReq) withQueryParamSlice(key string, values []string) *AgentReq {
	if len(values) > 0 {
		r.queryParamSlices[key] = values
	}
	return r
}

func (r *AgentReq) withHeader(k, v string) *AgentReq {
	r.req.Header.Set(k, v)
	return r
}
