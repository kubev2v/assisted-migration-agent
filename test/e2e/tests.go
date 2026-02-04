package main

import (
	"context"
	"net/http"
	"net/url"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Agent", Ordered, func() {
	var (
		stack       *Stack
		proxy       *Proxy
		requests    chan Request
		proxyServer *http.Server
	)

	BeforeAll(func() {
		var err error
		stack, err = NewStack(cfg)
		Expect(err).ToNot(HaveOccurred(), "failed to create stack")

		GinkgoWriter.Println("Starting postgres...")
		err = stack.StartPostgres()
		Expect(err).ToNot(HaveOccurred(), "failed to start postgres")
		time.Sleep(2 * time.Second) // wait for postgres to be ready

		GinkgoWriter.Println("Starting backend...")
		err = stack.StartBackend()
		Expect(err).ToNot(HaveOccurred(), "failed to start backend")

		// Wait for backend to be ready
		err = WaitForReady(http.DefaultClient, cfg.BackendAgentEndpoint+"/health", 30*time.Second)
		Expect(err).ToNot(HaveOccurred(), "backend not ready")

		target, err := url.Parse(cfg.BackendAgentEndpoint)
		Expect(err).ToNot(HaveOccurred(), "failed to parse backend endpoint")

		proxy, requests = NewProxy(target)
		proxyServer = &http.Server{
			Addr:    ":8080",
			Handler: proxy.Handler(),
		}
		go proxyServer.ListenAndServe()
		time.Sleep(100 * time.Millisecond)
		GinkgoWriter.Println("Proxy started on :8080")
	})

	AfterAll(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = proxyServer.Shutdown(ctx)
		proxy.Close()

		_ = stack.StopBackend()
		_ = stack.StopPostgres()
	})

	_ = requests // silence unused variable warning
})
