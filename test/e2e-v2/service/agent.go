package service

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"go.uber.org/zap"

	v2 "github.com/kubev2v/assisted-migration-agent/api/v2"
)

type AgentSvc struct {
	baseURL    string
	httpClient *http.Client
}

func DefaultAgentSvc(baseURL string) *AgentSvc {
	return &AgentSvc{
		baseURL: baseURL,
		httpClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
	}
}

// --- Agent lifecycle ---

type AgentStatus struct {
	Mode              string
	ConsoleConnection string
	Error             string
}

func (a *AgentSvc) Status() (*AgentStatus, error) {
	resp, err := a.doGet("/api/v2/agent")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var status v2.AgentStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	errStr := ""
	if status.ConsoleConnection.Error != nil {
		errStr = *status.ConsoleConnection.Error
	}

	zap.S().Infof("mode: %s. Console connection: %s. error: %s",
		status.Mode, status.ConsoleConnection.Status, errStr)
	return &AgentStatus{
		Mode:              string(status.Mode),
		ConsoleConnection: string(status.ConsoleConnection.Status),
		Error:             errStr,
	}, nil
}

func (a *AgentSvc) SetAgentMode(mode string) (*AgentStatus, error) {
	body := v2.AgentModeRequest{Mode: v2.AgentModeRequestMode(mode)}
	resp, err := a.doJSON(http.MethodPost, "/api/v2/agent", body)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var status v2.AgentStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	errStr := ""
	if status.ConsoleConnection.Error != nil {
		errStr = *status.ConsoleConnection.Error
	}

	return &AgentStatus{
		Mode:              string(status.Mode),
		ConsoleConnection: string(status.ConsoleConnection.Status),
		Error:             errStr,
	}, nil
}

func (a *AgentSvc) SetAgentModeRaw(body []byte) (int, error) {
	resp, err := a.doRaw(http.MethodPost, "/api/v2/agent", body)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

// CreateGroupRaw sends a raw POST to /groups and returns the status code.
func (a *AgentSvc) CreateGroupRaw(body []byte) (int, error) {
	resp, err := a.doRaw(http.MethodPost, "/api/v2/groups", body)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

// UpdateGroupRaw sends a raw PATCH to /groups/{id} and returns the status code.
func (a *AgentSvc) UpdateGroupRaw(groupID string, body []byte) (int, error) {
	resp, err := a.doRaw(http.MethodPatch, "/api/v2/groups/"+url.PathEscape(groupID), body)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

// GetGroupStatus sends a GET to /groups/{id} and returns only the status code.
func (a *AgentSvc) GetGroupStatus(groupID string) (int, error) {
	resp, err := a.doRequest(http.MethodGet, "/api/v2/groups/"+url.PathEscape(groupID), nil)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

// --- Credentials ---

func (a *AgentSvc) StoreCredentials(vcenterURL, username, password string) (*v2.CredentialStatus, error) {
	skipTls := true
	body := v2.VcenterCredentials{
		Url:      vcenterURL,
		Username: username,
		Password: password,
		SkipTls:  &skipTls,
	}
	resp, err := a.doJSON(http.MethodPut, "/api/v2/credentials", body)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(bodyBytes))
	}

	var result v2.CredentialStatus
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}

func (a *AgentSvc) GetCredentials() (*v2.CredentialStatus, error) {
	resp, err := a.doGet("/api/v2/credentials")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result v2.CredentialStatus
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}

func (a *AgentSvc) DeleteCredentials() error {
	resp, err := a.doRequest(http.MethodDelete, "/api/v2/credentials", nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return nil
}

// --- Collector ---

func (a *AgentSvc) StartCollector() (*v2.CollectorStatus, error) {
	resp, err := a.doRequest(http.MethodPost, "/api/v2/collector", nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusAccepted {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(bodyBytes))
	}

	var result v2.CollectorStatus
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}

func (a *AgentSvc) GetCollectorStatus() (*v2.CollectorStatus, error) {
	resp, err := a.doGet("/api/v2/collector")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result v2.CollectorStatus
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}

func (a *AgentSvc) StopCollector() error {
	resp, err := a.doRequest(http.MethodDelete, "/api/v2/collector", nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// --- Collections ---

func (a *AgentSvc) ListCollections() (*v2.CollectionListResponse, error) {
	resp, err := a.doGet("/api/v2/collections")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result v2.CollectionListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}

func (a *AgentSvc) DeleteCollection(id string) error {
	resp, err := a.doRequest(http.MethodDelete, "/api/v2/collections/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return nil
}

// --- VMs (collection-scoped) ---

type VMListParams struct {
	ByExpression *string
	Sort         []string
	Page         *int
	PageSize     *int
}

func (a *AgentSvc) ListVMs(collectionID string, params *VMListParams) (*v2.VirtualMachineListResponse, error) {
	path := fmt.Sprintf("/api/v2/collections/%s/virtualmachines", url.PathEscape(collectionID))
	req, err := http.NewRequest(http.MethodGet, a.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	if params != nil {
		q := req.URL.Query()
		if params.ByExpression != nil {
			q.Set("byExpression", *params.ByExpression)
		}
		if params.Page != nil {
			q.Set("page", strconv.Itoa(*params.Page))
		}
		if params.PageSize != nil {
			q.Set("pageSize", strconv.Itoa(*params.PageSize))
		}
		for _, s := range params.Sort {
			q.Add("sort", s)
		}
		req.URL.RawQuery = q.Encode()
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result v2.VirtualMachineListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}

func (a *AgentSvc) GetVM(collectionID, vmID string) (*v2.VirtualMachineDetail, error) {
	path := fmt.Sprintf("/api/v2/collections/%s/virtualmachines/%s",
		url.PathEscape(collectionID), url.PathEscape(vmID))
	resp, err := a.doGet(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("VM not found: %s", vmID)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result v2.VirtualMachineDetail
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}

// --- Groups ---
// CRUD operations use /groups (latest-collection shortcut).
// Read operations can use /collections/{id}/groups for collection-scoped queries.

func (a *AgentSvc) CreateGroup(name, filter, description string) (*v2.Group, error) {
	body := v2.CreateGroupRequest{
		Name:   name,
		Filter: filter,
	}
	if description != "" {
		body.Description = &description
	}

	resp, err := a.doJSON(http.MethodPost, "/api/v2/groups", body)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(bodyBytes))
	}

	var result v2.Group
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}

func (a *AgentSvc) ListGroups(byName *string, page, pageSize *int) (*v2.GroupListResponse, error) {
	req, err := http.NewRequest(http.MethodGet, a.baseURL+"/api/v2/groups", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	q := req.URL.Query()
	if byName != nil {
		q.Set("byName", *byName)
	}
	if page != nil {
		q.Set("page", strconv.Itoa(*page))
	}
	if pageSize != nil {
		q.Set("pageSize", strconv.Itoa(*pageSize))
	}
	req.URL.RawQuery = q.Encode()

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result v2.GroupListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}

func (a *AgentSvc) GetGroup(groupID string, sort []string, page, pageSize *int) (*v2.GroupResponse, error) {
	path := "/api/v2/groups/" + url.PathEscape(groupID)
	req, err := http.NewRequest(http.MethodGet, a.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	q := req.URL.Query()
	for _, s := range sort {
		q.Add("sort", s)
	}
	if page != nil {
		q.Set("page", strconv.Itoa(*page))
	}
	if pageSize != nil {
		q.Set("pageSize", strconv.Itoa(*pageSize))
	}
	req.URL.RawQuery = q.Encode()

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("group not found: %s", groupID)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result v2.GroupResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}

func (a *AgentSvc) UpdateGroup(groupID string, body v2.UpdateGroupRequest) (*v2.Group, error) {
	path := "/api/v2/groups/" + url.PathEscape(groupID)
	resp, err := a.doJSON(http.MethodPatch, path, body)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("group not found: %s", groupID)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result v2.Group
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}

func (a *AgentSvc) DeleteGroup(groupID string) (int, error) {
	path := "/api/v2/groups/" + url.PathEscape(groupID)
	resp, err := a.doRequest(http.MethodDelete, path, nil)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

// --- Applications (collection-scoped) ---

func (a *AgentSvc) ListApplications(collectionID string) (*v2.ApplicationListResponse, error) {
	path := fmt.Sprintf("/api/v2/collections/%s/applications", url.PathEscape(collectionID))
	resp, err := a.doGet(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result v2.ApplicationListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}

// --- Latest-collection VM shortcuts ---

func (a *AgentSvc) ListLatestVMs(params *VMListParams) (*v2.VirtualMachineListResponse, error) {
	req, err := http.NewRequest(http.MethodGet, a.baseURL+"/api/v2/virtualmachines", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	if params != nil {
		q := req.URL.Query()
		if params.ByExpression != nil {
			q.Set("byExpression", *params.ByExpression)
		}
		if params.Page != nil {
			q.Set("page", strconv.Itoa(*params.Page))
		}
		if params.PageSize != nil {
			q.Set("pageSize", strconv.Itoa(*params.PageSize))
		}
		for _, s := range params.Sort {
			q.Add("sort", s)
		}
		req.URL.RawQuery = q.Encode()
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result v2.VirtualMachineListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}

func (a *AgentSvc) GetLatestVM(vmID string) (*v2.VirtualMachineDetail, error) {
	path := "/api/v2/virtualmachines/" + url.PathEscape(vmID)
	resp, err := a.doGet(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("VM not found: %s", vmID)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result v2.VirtualMachineDetail
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}

func (a *AgentSvc) UpdateVM(vmID string, body v2.VirtualMachineUpdateRequest) error {
	path := "/api/v2/virtualmachines/" + url.PathEscape(vmID)
	resp, err := a.doJSON(http.MethodPatch, path, body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("VM not found: %s (404)", vmID)
	}
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(bodyBytes))
	}
	return nil
}

func (a *AgentSvc) UpdateVMLabels(vmID string, labels []string) error {
	return a.UpdateVM(vmID, v2.VirtualMachineUpdateRequest{Labels: &labels})
}

func (a *AgentSvc) UpdateVMMigrationExclusion(vmID string, excluded bool) error {
	return a.UpdateVM(vmID, v2.VirtualMachineUpdateRequest{MigrationExcluded: &excluded})
}

func (a *AgentSvc) BatchUpdateVMExclusion(vmIds []string, excluded bool) error {
	body := v2.BatchUpdateExclusionRequest{
		VmIds:             vmIds,
		MigrationExcluded: excluded,
	}
	resp, err := a.doJSON(http.MethodPost, "/api/v2/virtualmachines/batch-update-exclusion", body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(bodyBytes))
	}
	return nil
}

// --- Labels (latest-collection shortcuts) ---

func (a *AgentSvc) GetVMLabels() (*v2.VMLabelsResponse, error) {
	resp, err := a.doGet("/api/v2/virtualmachines/labels")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result v2.VMLabelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}

func (a *AgentSvc) UpdateLabelVMs(label string, add, remove []string) error {
	if label == "" {
		return fmt.Errorf("label cannot be empty")
	}

	reqBody := v2.UpdateLabelVMsRequest{}
	if add != nil {
		reqBody.Add = &add
	}
	if remove != nil {
		reqBody.Remove = &remove
	}

	path := "/api/v2/virtualmachines/labels/" + url.PathEscape(label)
	resp, err := a.doJSON(http.MethodPatch, path, reqBody)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(bodyBytes))
	}
	return nil
}

func (a *AgentSvc) DeleteLabelGlobally(label string) (*v2.DeleteLabelGloballyResponse, error) {
	if label == "" {
		return nil, fmt.Errorf("label cannot be empty")
	}

	path := "/api/v2/virtualmachines/labels/" + url.PathEscape(label)
	resp, err := a.doRequest(http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(bodyBytes))
	}

	var result v2.DeleteLabelGloballyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}

// --- Helpers ---

func (a *AgentSvc) doGet(path string) (*http.Response, error) {
	return a.doRequest(http.MethodGet, path, nil)
}

func (a *AgentSvc) doJSON(method, path string, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}
	req, err := http.NewRequest(method, a.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return a.httpClient.Do(req)
}

func (a *AgentSvc) doRaw(method, path string, body []byte) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, a.baseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return a.httpClient.Do(req)
}

func (a *AgentSvc) doRequest(method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, a.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	return resp, nil
}
