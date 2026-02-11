package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/kubev2v/migration-planner/api/v1alpha1"
	api "github.com/kubev2v/migration-planner/api/v1alpha1"
	. "github.com/kubev2v/migration-planner/test/e2e"
	"go.uber.org/zap"
)

// CreateSource sends a request to create a new source with the given name
func (s *PlannerSvc) CreateSource(name string) (*api.Source, error) {
	zap.S().Infof("[PlannerService] Creating source: %s", name)

	params := &v1alpha1.CreateSourceJSONRequestBody{Name: name}

	if TestOptions.DisconnectedEnvironment { // make the service unreachable

		toStrPtr := func(s string) *string {
			return &s
		}

		params.Proxy = &api.AgentProxy{
			HttpUrl:  toStrPtr("http://127.0.0.1"),
			HttpsUrl: toStrPtr("https://127.0.0.1"),
			NoProxy:  toStrPtr("vcenter.com"),
		}
	}

	reqBody, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}

	res, err := s.api.PostRequest(apiV1SourcesPath, reqBody)
	if err != nil {
		return nil, err
	}

	bodyBytes, err := io.ReadAll(res.Body)
	defer func() { _ = res.Body.Close() }()
	if err != nil {
		return nil, err
	}

	if res.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("create source failed with status code: %d", res.StatusCode)
	}

	if !strings.Contains(res.Header.Get("Content-Type"), "json") {
		return nil, fmt.Errorf("Content-Type isn't json")
	}

	var dest v1alpha1.Source
	if err := json.Unmarshal(bodyBytes, &dest); err != nil {
		return nil, err
	}

	return &dest, nil
}

// GetImageUrl retrieves the image URL for a specific source by UUID
func (s *PlannerSvc) GetImageUrl(id uuid.UUID) (string, error) {
	zap.S().Infof("[PlannerService] Get image url: %s", id)
	getImageUrlPath := path.Join(apiV1SourcesPath, id.String(), "image-url")
	res, err := s.api.GetRequest(getImageUrlPath)
	if err != nil {
		return "", fmt.Errorf("failed to get source url: %w", err)
	}
	defer res.Body.Close()

	var result struct {
		ExpiresAt string `json:"expires_at"`
		URL       string `json:"url"`
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to decod JSON: %w", err)
	}

	return result.URL, nil
}

// GetSource fetches a single source by UUID
func (s *PlannerSvc) GetSource(id uuid.UUID) (*api.Source, error) {
	zap.S().Infof("[PlannerService] Get source: %s", id)
	res, err := s.api.GetRequest(path.Join(apiV1SourcesPath, id.String()))
	if err != nil {
		return nil, err
	}

	bodyBytes, err := io.ReadAll(res.Body)
	defer func() { _ = res.Body.Close() }()
	if err != nil {
		return nil, err
	}

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list sources. response status code: %d", res.StatusCode)
	}

	if !strings.Contains(res.Header.Get("Content-Type"), "json") {
		return nil, fmt.Errorf("Content-Type isn't json")
	}

	var dest v1alpha1.Source
	if err := json.Unmarshal(bodyBytes, &dest); err != nil {
		return nil, err
	}

	return &dest, nil
}

// GetSources retrieves a list of all available sources
func (s *PlannerSvc) GetSources() (*api.SourceList, error) {
	zap.S().Info("[PlannerService] Get sources")
	res, err := s.api.GetRequest(apiV1SourcesPath)
	if err != nil {
		return nil, err
	}

	bodyBytes, err := io.ReadAll(res.Body)
	defer func() { _ = res.Body.Close() }()
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list sources. response status code: %d", res.StatusCode)
	}

	if !strings.Contains(res.Header.Get("Content-Type"), "json") {
		return nil, fmt.Errorf("Content-Type isn't json")
	}

	var dest v1alpha1.SourceList
	if err := json.Unmarshal(bodyBytes, &dest); err != nil {
		return nil, err
	}

	return &dest, nil
}

// RemoveSource deletes a specific source by UUID
func (s *PlannerSvc) RemoveSource(uuid uuid.UUID) error {
	zap.S().Infof("[PlannerService] Delete source: %s", uuid)
	res, err := s.api.DeleteRequest(path.Join(apiV1SourcesPath, uuid.String()))
	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to delete source with uuid: %s. "+
			"response status code: %d", uuid.String(), res.StatusCode)
	}

	return err
}

// RemoveSources deletes all existing sources
func (s *PlannerSvc) RemoveSources() error {
	zap.S().Info("[PlannerService] Delete sources")
	res, err := s.api.DeleteRequest(apiV1SourcesPath)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to delete sources. response status code: %d", res.StatusCode)
	}

	return err
}

// UpdateSource updates the inventory of a specific source
func (s *PlannerSvc) UpdateSource(sourceID, agentID uuid.UUID, inventory *v1alpha1.Inventory) error {
	zap.S().Infof("[PlannerService] Update source: %s with agent: %s", sourceID, agentID)
	update := v1alpha1.UpdateInventoryJSONRequestBody{
		AgentId:   agentID,
		Inventory: *inventory,
	}

	reqBody, err := json.Marshal(update)
	if err != nil {
		return err
	}

	res, err := s.api.PutRequest(path.Join(apiV1SourcesPath, sourceID.String(), "inventory"), reqBody)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to update source with uuid: %s. "+
			"response status code: %d", sourceID.String(), res.StatusCode)
	}

	return err
}
