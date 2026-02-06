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

// TokenGenerator is a function that generates a JWT token for a given user.
type TokenGenerator func(username, orgID, email string) (string, error)

// PlannerSvc is the concrete implementation of PlannerService
type PlannerSvc struct {
	api      *ServiceApi
	tokenGen TokenGenerator
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
	return &PlannerSvc{api: serviceApi}, nil
}

// NewPlannerServiceWithOIDC creates a PlannerSvc backed by an OIDC token generator.
// The tokenGen function is typically infraManager.GenerateToken.
func NewPlannerServiceWithOIDC(baseURL string, tokenGen TokenGenerator) *PlannerSvc {
	return &PlannerSvc{
		api:      NewServiceApiWithToken(baseURL, ""),
		tokenGen: tokenGen,
	}
}

// WithAuthUser generates a token for the given user and returns a new PlannerSvc
// that injects that token into all subsequent requests.
// Usage: plannerSvc.WithAuthUser("user", "org", "user@example.com").GetSource(id)
func (s *PlannerSvc) WithAuthUser(username, orgID, email string) *PlannerSvc {
	if s.tokenGen == nil {
		zap.S().Warn("WithAuthUser called without a token generator; requests will have no auth token")
		return s
	}
	token, err := s.tokenGen(username, orgID, email)
	if err != nil {
		zap.S().Errorf("WithAuthUser: failed to generate token: %v", err)
		return s
	}
	return &PlannerSvc{
		api:      NewServiceApiWithToken(s.api.baseURL, token),
		tokenGen: s.tokenGen,
	}
}
