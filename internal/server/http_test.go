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
		cfg     *config.Configuration
		groups  map[string]server.APIGroup
		tempDir string
		srv     *server.Server
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

		groups = map[string]server.APIGroup{
			"/api/v1": {
				RegisterFn: func(router *gin.RouterGroup) {
					router.GET("/vms", func(c *gin.Context) {
						c.JSON(200, gin.H{"vms": []any{}, "total": 0, "page": 1, "pageCount": 1})
					})
				},
			},
			"/api/v2": {
				RegisterFn: func(router *gin.RouterGroup) {
					router.GET("/status", func(c *gin.Context) {
						c.JSON(200, gin.H{"status": "ok"})
					})
				},
			},
		}
	})

	AfterEach(func() {
		_ = os.RemoveAll(tempDir)
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
			srv, err = server.NewServer(cfg, groups)
			Expect(err).ToNot(HaveOccurred())

			go func() {
				_ = srv.Start(context.TODO())
			}()
			time.Sleep(100 * time.Millisecond)

			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/api/v1/vms", cfg.Server.HTTPPort))
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(200))
			_ = resp.Body.Close()
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
			srv, err = server.NewServer(cfg, groups)
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

			for _, route := range []string{"/", "/api/v1/vms", "/api/v2/status"} {
				resp, err := client.Get(fmt.Sprintf("https://localhost:%d%s", cfg.Server.HTTPPort, route))
				Expect(err).ToNot(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(200))
				Expect(resp.TLS).NotTo(BeNil())
				Expect(resp.TLS.Version).To(BeNumerically(">=", tls.VersionTLS12))
				_ = resp.Body.Close()
			}

			legacyClient := &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						InsecureSkipVerify: true,
						MinVersion:         tls.VersionTLS11,
						MaxVersion:         tls.VersionTLS11,
					},
				},
			}
			_, err = legacyClient.Get(fmt.Sprintf("https://localhost:%d/api/v2/status", cfg.Server.HTTPPort))
			Expect(err).To(HaveOccurred())
		})

		// Given a production server with static files
		// When we request the root path
		// Then it should serve the index.html
		It("serves static index.html at root", func() {
			var err error
			srv, err = server.NewServer(cfg, groups)
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
			_ = resp.Body.Close()
		})

		// Given a production server
		// When we request a non-existent API route
		// Then it should return 404 with a JSON error
		It("returns 404 JSON for unknown API routes", func() {
			var err error
			srv, err = server.NewServer(cfg, groups)
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

			for _, route := range []string{"/api", "/api/v1/nonexistent", "/api/v2/nonexistent"} {
				resp, err := client.Get(fmt.Sprintf("https://localhost:%d%s", cfg.Server.HTTPPort, route))
				Expect(err).ToNot(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(404))
				Expect(resp.Header.Get("Content-Type")).To(ContainSubstring("application/json"))
				_ = resp.Body.Close()
			}
		})

		// Given a production server
		// When we request a non-existent non-API route
		// Then it should serve index.html (SPA fallback)
		It("serves index.html for non-API routes (SPA fallback)", func() {
			var err error
			srv, err = server.NewServer(cfg, groups)
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

			for _, route := range []string{"/some/spa/route", "/apiculture"} {
				resp, err := client.Get(fmt.Sprintf("https://localhost:%d%s", cfg.Server.HTTPPort, route))
				Expect(err).ToNot(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(200))
				_ = resp.Body.Close()
			}
		})

		// Given a running production server
		// When we call Stop
		// Then subsequent requests should fail
		It("stops accepting requests after Stop", func() {
			var err error
			srv, err = server.NewServer(cfg, groups)
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
			_, err = client.Get(fmt.Sprintf("https://localhost:%d/api/v1/vms", cfg.Server.HTTPPort))
			Expect(err).To(HaveOccurred())
		})
	})
})
