package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/kubev2v/migration-planner/pkg/opa"
	"go.uber.org/zap"

	v2 "github.com/kubev2v/assisted-migration-agent/agent/api/v2"
	v2Handlers "github.com/kubev2v/assisted-migration-agent/agent/handlers/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/bootstrap"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/datalock"
	"github.com/kubev2v/assisted-migration-agent/internal/handlers"
	"github.com/kubev2v/assisted-migration-agent/internal/server"
	service "github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/upgrade"
	"github.com/kubev2v/assisted-migration-agent/pkg/console"
	"github.com/kubev2v/assisted-migration-agent/pkg/crypto"
)

const (
	apiV2 string = "/api/v2"
)

func Run(cfg *config.Configuration) error {
	return RunManaged(cfg, upgrade.Options{})
}

// RunManaged holds the data lock across probation release and normal startup.
func RunManaged(cfg *config.Configuration, opts upgrade.Options) error {
	lock, err := datalock.Acquire(cfg.Agent.DataFolder)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGQUIT)
	defer cancel()
	for {
		srv, cleanup, lifecycle, err := initV2Managed(cfg, opts)
		if err != nil {
			return err
		}
		serverDone := make(chan error, 1)
		go func() { serverDone <- srv.Start(ctx) }()
		restart := false
		select {
		case <-ctx.Done():
		case opts = <-lifecycle.Restart():
			restart = true
		case err = <-serverDone:
			lifecycle.Close()
			cleanup()
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		}
		srv.Stop(context.WithoutCancel(ctx))
		// Finish every request before closing services or releasing the data lock.
		err = <-serverDone
		lifecycle.Close()
		cleanup()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		if !restart {
			return nil
		}
		cfg.Agent.AutoMigrate = false
	}
}

func initV2Managed(cfg *config.Configuration, opts upgrade.Options) (*server.Server, func(), *upgrade.Service, error) {
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 300 * time.Second
	}
	if err := upgrade.Authorize(cfg.Agent.DataFolder, opts, false); err != nil {
		return nil, nil, nil, err
	}
	if opts.AttemptID != "" {
		lifecycle, err := upgrade.NewService(cfg.Agent.DataFolder, opts, nil, timeout)
		if err != nil {
			return nil, nil, nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if err := lifecycle.ValidateProbation(ctx); err != nil {
			return nil, nil, nil, err
		}
		handler := v2Handlers.NewUpgradeHandler(v2Handlers.NewHandler(*cfg, nil), lifecycle)
		swagger, err := v2.GetSwagger()
		if err != nil {
			return nil, nil, nil, err
		}
		srv, err := server.NewServer(cfg.Server, map[string]server.APIGroup{apiV2: {Swagger: swagger, RegisterFn: func(router *gin.RouterGroup) { router.Use(handler.Admission); v2.RegisterHandlers(router, handler) }}})
		if err != nil {
			return nil, nil, nil, err
		}
		return srv, func() {}, lifecycle, nil
	}

	// Probation skips ordinary CLI validation. Validate identities before a
	// released process opens writable stores or constructs the console service.
	if cfg.Agent.RVToolsMode {
		cfg.Agent.Mode = "disconnected"
		if cfg.Agent.ID == "" {
			cfg.Agent.ID = uuid.NewString()
		}
		if cfg.Agent.SourceID == "" {
			cfg.Agent.SourceID = uuid.NewString()
		}
	}
	if _, err := uuid.Parse(cfg.Agent.ID); err != nil {
		return nil, nil, nil, fmt.Errorf("invalid agent-id: %w", err)
	}
	if _, err := uuid.Parse(cfg.Agent.SourceID); err != nil {
		return nil, nil, nil, fmt.Errorf("invalid source-id: %w", err)
	}

	opaValidator, err := opa.NewValidatorFromDir(cfg.Agent.OpaPoliciesFolder)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to initialize OPA validator: %w", err)
	}

	pool, err := bootstrap.InitPool(cfg.Agent.DataFolder, cfg.Agent.AutoMigrate)
	if err != nil {
		return nil, nil, nil, err
	}

	initialized := false
	defer func() {
		if !initialized {
			pool.Close()
		}
	}()
	consoleClient, km, err := initShared(cfg)
	if err != nil {
		return nil, nil, nil, err
	}

	v2SvcMgr, err := NewAgentServiceManager(cfg, pool, consoleClient, km, service.WithOpaValidator(opaValidator))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to initialize v2 service: %w", err)
	}

	defer func() {
		if !initialized {
			v2SvcMgr.ConsoleService().Stop()
			v2SvcMgr.Stop(context.Background())
		}
	}()
	lifecycle, err := upgrade.NewService(cfg.Agent.DataFolder, opts, v2SvcMgr, timeout)
	if err != nil {
		v2SvcMgr.ConsoleService().Stop()
		return nil, nil, nil, err
	}
	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		handlers.RegisterValidators(v)
	}

	var v2H v2.ServerInterface
	if cfg.Agent.RVToolsMode {
		zap.S().Info("rvtools mode enabled: credentials, inspector, and vCenter collector endpoints are disabled")
		v2H = v2Handlers.NewRVToolsHandler(*cfg, v2SvcMgr)
	} else {
		v2H = v2Handlers.NewHandler(*cfg, v2SvcMgr)
	}

	swagger, err := v2.GetSwagger()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to load v2 swagger spec: %w", err)
	}

	handler := v2Handlers.NewUpgradeHandler(v2H, lifecycle)
	srv, err := server.NewServer(cfg.Server, map[string]server.APIGroup{
		apiV2: {
			Swagger: swagger,
			RegisterFn: func(router *gin.RouterGroup) {
				router.Use(handler.Admission)
				v2.RegisterHandlers(router, handler)
			},
		},
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create http server: %w", err)
	}

	cleanup := func() {
		v2SvcMgr.ConsoleService().Stop()
		v2SvcMgr.Stop(context.Background())
		pool.Close()
		zap.S().Info("v2 service and pool closed")
	}

	initialized = true
	return srv, cleanup, lifecycle, nil
}

// NewAgentServiceManager assembles the services required by the Agent.
func NewAgentServiceManager(cfg *config.Configuration, pool *store.Pool, consoleClient *console.Client, keyMgr *crypto.KeyManager, opts ...service.ServiceManagerOption) (*service.ServiceManager, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	if pool == nil {
		return nil, errors.New("pool is required")
	}
	if keyMgr == nil {
		return nil, errors.New("key manager is required")
	}

	mainDB, err := pool.Get(store.MainDatabaseID)
	if err != nil {
		return nil, err
	}
	mainStore, err := mainDB.Store()
	if err != nil {
		return nil, err
	}

	credentials := service.NewCredentialsService(mainStore)
	credentials.WithKeyManager(keyMgr)
	managerOpts := append([]service.ServiceManagerOption{}, opts...)
	managerOpts = append(managerOpts,
		service.WithPool(pool),
		service.WithCollectionService(service.NewCollectionService(pool)),
		service.WithCredentialsService(credentials),
		service.WithVddkService(service.NewVddkService(cfg.Agent.DataFolder, pool)),
		service.WithForecasterService(service.NewForecasterService(pool, credentials)),
	)
	manager := service.NewServiceManager(cfg.Agent.DataFolder, managerOpts...)

	// The console needs the manager for status reporting, so it is attached last.
	consoleService, err := service.NewConsoleService(cfg.Agent, consoleClient, manager, mainStore)
	if err != nil {
		return nil, err
	}
	manager.SetConsoleService(consoleService)
	return manager, nil
}

func initShared(cfg *config.Configuration) (*console.Client, *crypto.KeyManager, error) {
	jwt := ""
	if cfg.Auth.Enabled {
		data, err := os.ReadFile(cfg.Auth.JWTFilePath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read agent's jwt: %w", err)
		}
		if len(data) == 0 {
			return nil, nil, errors.New("failed to read agent's jwt. the JWT is empty")
		}
		jwt = strings.TrimSpace(string(data))
	}

	consoleClient, err := console.NewConsoleClient(cfg.Console.URL, jwt)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create console client: %w", err)
	}

	keyManager, err := crypto.NewKeyManager(cfg.Agent.DataFolder)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize key manager: %w", err)
	}

	return consoleClient, keyManager, nil
}
