package service

import (
	"fmt"

	. "github.com/kubev2v/assisted-migration-agent/test/e2e/model"
	. "github.com/kubev2v/assisted-migration-agent/test/e2e/utils"
	"go.uber.org/zap"
)

const (
	apiV1SourcesPath            = "/api/v1/sources"
	apiV1AssessmentsPath        = "/api/v1/assessments"
	apiV1AssessmentsRVToolsPath = "/api/v1/assessments/rvtools"
	apiV1AssessmentsJobsPath    = "/api/v1/assessments/jobs"
)

// PlannerSvc is the concrete implementation of PlannerService
type PlannerSvc struct {
	api         *ServiceApi
	credentials *User
}

// DefaultPlannerService initializes a planner service using default *auth.User credentials
func DefaultPlannerService() (*PlannerSvc, error) {
	return NewPlannerService(DefaultUserAuth())
}

// NewPlannerService initializes the planner service with custom *auth.User credentials
func NewPlannerService(cred *User) (*PlannerSvc, error) {
	zap.S().Info("Initializing PlannerService...")
	serviceApi, err := NewServiceApi(cred)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize planner service API")
	}
	return &PlannerSvc{api: serviceApi, credentials: cred}, nil
}
