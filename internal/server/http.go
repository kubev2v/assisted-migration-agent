package server

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	ginzap "github.com/gin-contrib/zap"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/server/middlewares"
	"github.com/kubev2v/assisted-migration-agent/pkg/certificates"
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
	engine.MaxMultipartMemory = 64 << 20 // max 64Mb

	srv := &http.Server{
		Addr:    fmt.Sprintf("0.0.0.0:%d", cfg.Server.HTTPPort),
		Handler: engine,
	}

	if cfg.Server.ServerMode == ProductionServer {
		engine.Static("/static", cfg.Server.StaticsFolder)
		// Serve assets at /assets/ to match HTML references
		engine.Static("/assets", path.Join(cfg.Server.StaticsFolder, "assets"))
		engine.StaticFile("/", path.Join(cfg.Server.StaticsFolder, "index.html"))
		engine.StaticFile("/favicon.ico", path.Join(cfg.Server.StaticsFolder, "favicon.ico"))

		engine.NoRoute(func(c *gin.Context) {
			if strings.HasPrefix(c.Request.URL.Path, "/api") {
				c.JSON(404, gin.H{
					"error": "API endpoint not found",
				})
				return
			}
			c.File(path.Join(cfg.Server.StaticsFolder, "index.html"))
		})

		cert, key, err := certificates.GenerateSelfSignedCertificate(time.Now().AddDate(1, 0, 0))
		if err != nil {
			return nil, fmt.Errorf("failed to generate server's certificates: %w", err)
		}

		tlsConfig, err := getTLSConfig(cert, key)
		if err != nil {
			return nil, err
		}

		srv.TLSConfig = tlsConfig
	}

	router := engine.Group(apiV1)

	router.Use(
		middlewares.Logger(),
		ginzap.RecoveryWithZap(zap.S().Desugar(), true),
	)

	registerHandlerFn(router)

	return &Server{srv: srv}, nil
}

// Start starts the HTTP or HTTPS server based on TLS configuration.
func (r *Server) Start(ctx context.Context) error {
	if r.srv.TLSConfig != nil {
		return r.srv.ListenAndServeTLS("", "")
	}
	return r.srv.ListenAndServe()
}

func (r *Server) Stop(ctx context.Context) {
	if err := r.srv.Shutdown(ctx); err != nil {
		zap.S().Errorw("server shutdown", "error", err)
	}
}

func getTLSConfig(cert *x509.Certificate, privateKey *rsa.PrivateKey) (*tls.Config, error) {
	certPEM := new(bytes.Buffer)
	if err := pem.Encode(certPEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	}); err != nil {
		return nil, err
	}

	privKeyPEM := new(bytes.Buffer)
	if err := pem.Encode(privKeyPEM, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}); err != nil {
		return nil, err
	}

	serverCert, err := tls.X509KeyPair(certPEM.Bytes(), privKeyPEM.Bytes())
	if err != nil {
		return nil, err
	}

	return &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}
