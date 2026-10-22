package analyzer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/kubev2v/migration-planner/pkg/opa"
	"go.uber.org/zap"

	v1 "github.com/kubev2v/assisted-migration-agent/analyzer/api/v1"
	analyzerHandlers "github.com/kubev2v/assisted-migration-agent/analyzer/handlers/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/bootstrap"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/handlers"
	"github.com/kubev2v/assisted-migration-agent/internal/server"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

const apiV1 = "/api/v1"

func Run(ctx context.Context, cfg *config.Configuration) error {
	srv, cleanup, err := bootstrapAnalyzer(cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	go func() {
		<-ctx.Done()
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Stop(stopCtx)
	}()

	zap.S().Infof("Starting RVTools Analyzer on port %d", cfg.Server.HTTPPort)
	err = srv.Start(ctx)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("failed to start analyzer server: %w", err)
	}
	zap.S().Info("analyzer server shutdown")
	return nil
}

func bootstrapAnalyzer(cfg *config.Configuration) (*server.Server, func(), error) {
	if err := os.MkdirAll(cfg.Agent.DataFolder, 0750); err != nil {
		return nil, nil, fmt.Errorf("create data folder: %w", err)
	}

	opaValidator, err := opa.NewValidatorFromDir(cfg.Agent.OpaPoliciesFolder)
	if err != nil {
		return nil, nil, fmt.Errorf("initialize OPA validator: %w", err)
	}
	pool, err := bootstrap.InitPool(cfg.Agent.DataFolder)
	if err != nil {
		return nil, nil, err
	}
	cleanupPool := func() { pool.Close() }

	svc, err := newAnalyzerServiceManager(cfg, pool, opaValidator)
	if err != nil {
		cleanupPool()
		return nil, nil, err
	}
	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		handlers.RegisterValidators(v)
	}

	swagger, err := v1.GetSwagger()
	if err != nil {
		cleanupPool()
		return nil, nil, fmt.Errorf("load analyzer API spec: %w", err)
	}
	h := analyzerHandlers.NewHandler(*cfg, svc)
	srv, err := server.NewServerWithConfig(cfg.Server, map[string]server.APIGroup{
		apiV1: {
			Swagger: swagger,
			RegisterFn: func(router *gin.RouterGroup) {
				v1.RegisterHandlers(router, h)
			},
		},
	})
	if err != nil {
		cleanupPool()
		return nil, nil, fmt.Errorf("create analyzer server: %w", err)
	}
	cleanup := func() {
		svc.Stop(context.Background())
		cleanupPool()
	}
	return srv, cleanup, nil
}

func newAnalyzerServiceManager(cfg *config.Configuration, pool *store.Pool, validator *opa.Validator) (*services.ServiceManager, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	if pool == nil {
		return nil, errors.New("pool is required")
	}
	if cfg.Agent.DataFolder == "" {
		return nil, errors.New("data folder is required")
	}
	if validator == nil {
		return nil, errors.New("OPA validator is required")
	}

	return services.NewServiceManager(
		services.WithDataFolder(cfg.Agent.DataFolder),
		services.WithPool(pool),
		services.WithOpaValidator(validator),
		services.WithCollectionService(services.NewCollectionService(pool)),
	), nil
}
