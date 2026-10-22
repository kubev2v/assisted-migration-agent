package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ecordell/optgen/helpers"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/kubev2v/migration-planner/pkg/opa"
	"go.uber.org/zap"

	v2 "github.com/kubev2v/assisted-migration-agent/agent/api/v2"
	v2Handlers "github.com/kubev2v/assisted-migration-agent/agent/handlers/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/bootstrap"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/handlers"
	"github.com/kubev2v/assisted-migration-agent/internal/server"
	service "github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/pkg/console"
	"github.com/kubev2v/assisted-migration-agent/pkg/crypto"
)

const (
	apiV2 string = "/api/v2"
)

func Run(cfg *config.Configuration) error {
	zap.S().Infow("using configuration",
		"agent", helpers.Flatten(cfg.Agent.DebugMap()),
		"server", helpers.Flatten(cfg.Server.DebugMap()),
		"console", helpers.Flatten(cfg.Console.DebugMap()),
		"auth", helpers.Flatten(cfg.Auth.DebugMap()),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGQUIT)
	wg := sync.WaitGroup{}
	wg.Add(1)

	srv, cleanup, err := initV2(cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	go func() {
		defer func() {
			wg.Done()
			cancel()
		}()
		zap.S().Infof("Starting HTTP server on port %d", cfg.Server.HTTPPort)

		if err := srv.Start(ctx); err != nil {
			if !errors.Is(err, http.ErrServerClosed) {
				zap.S().Fatalw("failed to start http server", "error", err)
			}
		}
	}()

	go func() {
		<-ctx.Done()
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Stop(stopCtx)
	}()

	<-ctx.Done()
	wg.Wait()

	zap.S().Info("server shutdown")

	return nil
}

func initV2(cfg *config.Configuration) (*server.Server, func(), error) {
	opaValidator, err := opa.NewValidatorFromDir(cfg.Agent.OpaPoliciesFolder)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize OPA validator: %w", err)
	}

	pool, err := bootstrap.InitPool(cfg.Agent.DataFolder)
	if err != nil {
		return nil, nil, err
	}

	consoleClient, km, err := initShared(cfg)
	if err != nil {
		return nil, nil, err
	}

	v2SvcMgr, err := NewAgentServiceManager(cfg, pool, consoleClient, km, service.WithOpaValidator(opaValidator))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize v2 service: %w", err)
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
		return nil, nil, fmt.Errorf("failed to load v2 swagger spec: %w", err)
	}

	srv, err := server.NewServer(cfg, map[string]server.APIGroup{
		apiV2: {
			Swagger: swagger,
			RegisterFn: func(router *gin.RouterGroup) {
				v2.RegisterHandlers(router, v2H)
			},
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create http server: %w", err)
	}

	cleanup := func() {
		v2SvcMgr.Stop(context.Background())
		pool.Close()
		zap.S().Info("v2 service and pool closed")
	}

	return srv, cleanup, nil
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
		service.WithDataFolder(cfg.Agent.DataFolder),
		service.WithPool(pool),
		service.WithCollectionService(service.NewCollectionService(pool)),
		service.WithCredentialsService(credentials),
		service.WithVddkService(service.NewVddkService(cfg.Agent.DataFolder, pool)),
		service.WithForecasterService(service.NewForecasterService(pool, credentials)),
	)
	manager := service.NewServiceManager(managerOpts...)

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
