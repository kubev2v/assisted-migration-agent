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

		// Given a production server with static files
		// When we request the root path
		// Then it should serve the index.html
		It("serves static index.html at root", func() {
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

			resp, err := client.Get(fmt.Sprintf("https://localhost:%d/", cfg.Server.HTTPPort))
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(200))
			resp.Body.Close()
		})

		// Given a production server
		// When we request a non-existent API route
		// Then it should return 404 with a JSON error
		It("returns 404 JSON for unknown API routes", func() {
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

			resp, err := client.Get(fmt.Sprintf("https://localhost:%d/api/v1/nonexistent", cfg.Server.HTTPPort))
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(404))
			resp.Body.Close()
		})

		// Given a production server
		// When we request a non-existent non-API route
		// Then it should serve index.html (SPA fallback)
		It("serves index.html for non-API routes (SPA fallback)", func() {
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

			resp, err := client.Get(fmt.Sprintf("https://localhost:%d/some/spa/route", cfg.Server.HTTPPort))
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(200))
			resp.Body.Close()
		})

		// Given a running production server
		// When we call Stop
		// Then subsequent requests should fail
		It("stops accepting requests after Stop", func() {
			var err error
			srv, err = server.NewServer(cfg, registerHandlerFn)
			Expect(err).ToNot(HaveOccurred())

			go func() {
				_ = srv.Start(context.TODO())
			}()
			time.Sleep(100 * time.Millisecond)

			// Act
			srv.Stop(context.TODO())
			srv = nil // prevent double stop in AfterEach

			// Assert
			client := &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
				},
			}
			_, err = client.Get(fmt.Sprintf("https://localhost:%d/api/v1/health", cfg.Server.HTTPPort))
			Expect(err).To(HaveOccurred())
		})
	})
})
