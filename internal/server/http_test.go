package server_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/server"
)

var _ = Describe("HTTP Server", func() {
	var (
		cfg               *config.Configuration
		registerHandlerFn func(router *gin.RouterGroup)
		tempDir           string
		srv               *server.Server
	)

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "server-test")
		Expect(err).ToNot(HaveOccurred())

		indexPath := filepath.Join(tempDir, "index.html")
		err = os.WriteFile(indexPath, []byte("<html></html>"), 0o644)
		Expect(err).ToNot(HaveOccurred())

		faviconPath := filepath.Join(tempDir, "favicon.ico")
		err = os.WriteFile(faviconPath, []byte(""), 0o644)
		Expect(err).ToNot(HaveOccurred())

		staticDir := filepath.Join(tempDir, "static")
		err = os.MkdirAll(staticDir, 0o755)
		Expect(err).ToNot(HaveOccurred())

		registerHandlerFn = func(router *gin.RouterGroup) {
			router.GET("/health", func(c *gin.Context) {
				c.JSON(200, gin.H{"status": "ok"})
			})
		}
	})

	AfterEach(func() {
		os.RemoveAll(tempDir)
	})

	Context("dev server mode", func() {
		BeforeEach(func() {
			cfg = &config.Configuration{
				Server: config.Server{
					ServerMode:    server.DevServer,
					HTTPPort:      18080,
					StaticsFolder: tempDir,
				},
			}
		})

		AfterEach(func() {
			if srv != nil {
				srv.Stop(context.TODO())
			}
		})

		It("serves over HTTP", func() {
			var err error
			srv, err = server.NewServer(cfg, registerHandlerFn)
			Expect(err).ToNot(HaveOccurred())

			go func() {
				_ = srv.Start(context.TODO())
			}()
			time.Sleep(100 * time.Millisecond)

			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/api/v1/health", cfg.Server.HTTPPort))
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(200))
			resp.Body.Close()
		})
	})

	Context("production server mode", func() {
		BeforeEach(func() {
			cfg = &config.Configuration{
				Server: config.Server{
					ServerMode:    server.ProductionServer,
					HTTPPort:      18443,
					StaticsFolder: tempDir,
				},
			}
		})

		AfterEach(func() {
			if srv != nil {
				srv.Stop(context.TODO())
			}
		})

		It("serves over HTTPS with TLS", func() {
			var err error
			srv, err = server.NewServer(cfg, registerHandlerFn)
			Expect(err).ToNot(HaveOccurred())

			go func() {
				_ = srv.Start(context.TODO())
			}()
			time.Sleep(100 * time.Millisecond)

			client := &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
				},
			}

			resp, err := client.Get(fmt.Sprintf("https://localhost:%d/api/v1/health", cfg.Server.HTTPPort))
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(200))
			resp.Body.Close()
		})
	})
})
