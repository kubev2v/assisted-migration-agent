package server

import (
	"context"
	"fmt"
	"net/http"
	"path"

	ginzap "github.com/gin-contrib/zap"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/server/middlewares"
)

const (
	ProductionServer string = "prod"
	DevServer        string = "dev"
	apiV1            string = "/api/v1"
)

type Server struct {
	srv *http.Server
}

func NewServer(cfg *config.Configuration, registerHandlerFn func(router *gin.RouterGroup)) (*Server, error) {
	gin.SetMode(gin.DebugMode)
	if cfg.Server.ServerMode == ProductionServer {
		gin.SetMode(gin.ReleaseMode)
	}
	engine := gin.New()

	if cfg.Server.ServerMode == ProductionServer {
		engine.Static("/static", cfg.Server.StaticsFolder)
		engine.StaticFile("/", path.Join(cfg.Server.StaticsFolder, "index.html"))
		engine.StaticFile("/favicon.ico", path.Join(cfg.Server.StaticsFolder, "favicon.ico"))

		engine.NoRoute(func(c *gin.Context) {
			if c.Request.URL.Path[:4] == "/api" {
				c.JSON(404, gin.H{
					"error": "API endpoint not found",
				})
				return
			}
			c.File(path.Join(cfg.Server.StaticsFolder, "index.html"))
		})
	}

	router := engine.Group(apiV1)

	router.Use(
		middlewares.Logger(),
		ginzap.RecoveryWithZap(zap.S().Desugar(), true),
	)

	registerHandlerFn(router)

	srv := &http.Server{
		Addr:    fmt.Sprintf("0.0.0.0:%d", cfg.Server.HTTPPort),
		Handler: engine,
	}

	return &Server{srv: srv}, nil
}

// Start starts the HTTP server and handles graceful shutdown when the context is cancelled.
func (r *Server) Start(ctx context.Context) error {
	if err := r.srv.ListenAndServe(); err != nil {
		return err
	}

	return nil
}

func (r *Server) Stop(ctx context.Context) {
	if err := r.srv.Shutdown(ctx); err != nil {
		zap.S().Errorw("server shutdown", "error", err)
	}
}
