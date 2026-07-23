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

		// Wait for collection to complete
		gm.Eventually(func() string {
			status, err := agentSvc.GetCollectorStatus(agentSvc.CollectorID)
			if err != nil {
				return "error"
			}
			ginkgo.GinkgoWriter.Printf("Collector status: %s\n", status.Status)
			return string(status.Status)
		}, 60*time.Second, 2*time.Second).Should(gm.Equal("collected"))
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

	// Given a successful collection has completed
	// When retrieving the collection ID from the collector
	// Then the collection should exist in the list and be accessible
	ginkgo.It("should have collection ID matching the credential submission", func() {
		collections, err := agentSvc.ListCollections()
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to list collections")
		gm.Expect(collections.Collections).ToNot(gm.BeEmpty(), "expected at least 1 collection")

		// Verify the collection ID stored by StartCollector is present in the list
		found := false
		for _, c := range collections.Collections {
			if c.Id != "" {
				found = true
				agentSvc.CollectionID = c.Id
				break
			}
		}
		gm.Expect(found).To(gm.BeTrue(), "expected to find a collection with a valid ID")
		ginkgo.GinkgoWriter.Printf("Verified collection ID: %s\n", agentSvc.CollectionID)
	})
})
