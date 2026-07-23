package main

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/kubev2v/assisted-migration-agent/pkg/e2e/infra"
	"github.com/kubev2v/assisted-migration-agent/test/e2e-v2/service"

	"github.com/google/uuid"
	"github.com/onsi/ginkgo/v2"
	gm "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Collection lifecycle v2 e2e tests", ginkgo.Ordered, func() {
	var agentSvc *service.AgentSvc

	ginkgo.BeforeAll(func() {
		ginkgo.GinkgoWriter.Println("Starting postgres...")
		err := infraManager.StartPostgres()
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start postgres")
		time.Sleep(2 * time.Second) // wait for postgres to be ready

		ginkgo.GinkgoWriter.Println("Starting vcsim...")
		err = infraManager.StartVcsim()
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start vcsim")
		time.Sleep(1 * time.Second) // allow vcsim to initialize

		client := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}

		gm.Eventually(func() error {
			resp, err := client.Get(infra.VcsimURL)
			if err != nil {
				return err
			}
			defer func() {
				_ = resp.Body.Close()
			}()
			if resp.StatusCode >= 500 {
				return fmt.Errorf("server error: %d", resp.StatusCode)
			}
			return nil
		}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

		agentSvc = service.DefaultAgentSvc(cfg.AgentAPIUrl)

		agentID := uuid.NewString()
		_, err = infraManager.StartAgent(infra.AgentConfig{
			AgentID:        agentID,
			SourceID:       uuid.NewString(),
			Mode:           "disconnected",
			APIVersion:     "v2",
			ConsoleURL:     cfg.AgentProxyUrl,
			UpdateInterval: "1s",
		})
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")
		ginkgo.GinkgoWriter.Printf("Agent started with ID: %s\n", agentID)

		gm.Eventually(func() error {
			_, err := agentSvc.Status()
			return err
		}, 30*time.Second, 1*time.Second).Should(gm.BeNil())

		// Store credentials and start collector
		_, err = agentSvc.StoreCredentials(infra.VcsimURL, infra.VcsimUsername, infra.VcsimPassword)
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to store credentials")

		_, err = agentSvc.StartCollector()
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start collector")

		// Wait for a collection to appear in the pool
		gm.Eventually(func() int {
			collections, err := agentSvc.ListCollections()
			if err != nil {
				return 0
			}
			ginkgo.GinkgoWriter.Printf("Collections: %d\n", len(collections.Collections))
			return len(collections.Collections)
		}, 120*time.Second, 2*time.Second).Should(gm.BeNumerically(">", 0), "expected at least 1 collection")
	})

	ginkgo.AfterAll(func() {
		_ = infraManager.RemoveAgent()
		_ = infraManager.StopVcsim()
		_ = infraManager.StopPostgres()
	})

	// Given a successful collection has completed
	// When listing collections
	// Then at least one collection should be returned with non-empty ID and Name
	ginkgo.It("should list collections after successful collection", func() {
		collections, err := agentSvc.ListCollections()
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to list collections")
		gm.Expect(collections.Collections).ToNot(gm.BeEmpty(), "expected at least 1 collection")

		first := collections.Collections[0]
		gm.Expect(first.Id).ToNot(gm.BeEmpty(), "expected collection ID to be non-empty")
		gm.Expect(first.Name).ToNot(gm.BeEmpty(), "expected collection Name to be non-empty")
		ginkgo.GinkgoWriter.Printf("Collection: id=%s name=%s createdAt=%s\n", first.Id, first.Name, first.CreatedAt)
	})
})
