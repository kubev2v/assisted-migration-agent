package main

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	gm "github.com/onsi/gomega"

	v2 "github.com/kubev2v/assisted-migration-agent/api/v2"
	"github.com/kubev2v/assisted-migration-agent/pkg/e2e/infra"
	"github.com/kubev2v/assisted-migration-agent/test/e2e-v2/service"

	"github.com/google/uuid"
)

var _ = ginkgo.Describe("API validation v2 e2e tests", ginkgo.Ordered, func() {
	var agentSvc *service.AgentSvc

	ginkgo.BeforeAll(func() {
		ginkgo.GinkgoWriter.Println("Starting postgres...")
		err := infraManager.StartPostgres()
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start postgres")
		time.Sleep(2 * time.Second)

		agentSvc = service.DefaultAgentSvc(cfg.AgentAPIUrl)

		agentID := uuid.NewString()
		ginkgo.GinkgoWriter.Printf("Starting agent %s for validation tests (v2)...\n", agentID)
		_, err = infraManager.StartAgent(infra.AgentConfig{
			AgentID:        agentID,
			SourceID:       uuid.NewString(),
			Mode:           "disconnected",
			ConsoleURL:     cfg.AgentProxyUrl,
			UpdateInterval: "1s",
			APIVersion:     "v2",
		})
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")

		gm.Eventually(func() error {
			_, err := agentSvc.Status()
			return err
		}, 30*time.Second, 1*time.Second).Should(gm.BeNil(), "agent did not become ready")
	})

	ginkgo.AfterAll(func() {
		ginkgo.GinkgoWriter.Println("Cleaning up validation v2 tests...")
		_ = infraManager.RemoveAgent()
		_ = infraManager.StopPostgres()
	})

	// -----------------------------------------------------------------
	// POST /agent (SetAgentMode)
	// -----------------------------------------------------------------
	ginkgo.Context("POST /agent", func() {
		ginkgo.It("should reject invalid JSON", func() {
			status, err := agentSvc.SetAgentModeRaw([]byte("not json"))
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(status).To(gm.Equal(http.StatusBadRequest))
		})

		ginkgo.It("should reject missing mode field", func() {
			body, _ := json.Marshal(map[string]any{})
			status, err := agentSvc.SetAgentModeRaw(body)
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(status).To(gm.Equal(http.StatusBadRequest))
		})

		ginkgo.It("should reject invalid mode value", func() {
			body, _ := json.Marshal(map[string]string{"mode": "invalid"})
			status, err := agentSvc.SetAgentModeRaw(body)
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(status).To(gm.Equal(http.StatusBadRequest))
		})

		ginkgo.It("should accept valid mode 'disconnected'", func() {
			body, _ := json.Marshal(map[string]string{"mode": "disconnected"})
			status, err := agentSvc.SetAgentModeRaw(body)
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(status).To(gm.Equal(http.StatusOK))
		})
	})

	// -----------------------------------------------------------------
	// Group validation (requires collected data for the groups table)
	// -----------------------------------------------------------------
	ginkgo.Context("groups (with collected data)", ginkgo.Ordered, func() {
		ginkgo.BeforeAll(func() {
			ginkgo.GinkgoWriter.Println("Starting vcsim for group validation tests...")
			err := infraManager.StartVcsim()
			gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start vcsim")

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
				_ = resp.Body.Close()
				return nil
			}, 30*time.Second, 1*time.Second).Should(gm.BeNil(), "vcsim did not become ready")

			ginkgo.GinkgoWriter.Println("Storing credentials...")
			_, err = agentSvc.StoreCredentials(infra.VcsimURL, infra.VcsimUsername, infra.VcsimPassword)
			gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to store credentials")

			ginkgo.GinkgoWriter.Println("Starting collector...")
			_, err = agentSvc.StartCollector()
			gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start collector")

			gm.Eventually(func() int {
				collections, err := agentSvc.ListCollections()
				if err != nil {
					return 0
				}
				return len(collections.Collections)
			}, 120*time.Second, 2*time.Second).Should(gm.BeNumerically(">", 0), "expected at least 1 collection")
		})

		ginkgo.AfterAll(func() {
			_ = infraManager.StopVcsim()
		})

		// -----------------------------------------------------------------
		// POST /groups (CreateGroup)
		// -----------------------------------------------------------------
		ginkgo.Context("POST /groups", func() {
			ginkgo.It("should reject invalid JSON", func() {
				status, err := agentSvc.CreateGroupRaw([]byte("not json"))
				gm.Expect(err).ToNot(gm.HaveOccurred())
				gm.Expect(status).To(gm.Equal(http.StatusBadRequest))
			})

			ginkgo.It("should reject missing name", func() {
				body, _ := json.Marshal(v2.CreateGroupRequest{Filter: "memory > 0"})
				status, err := agentSvc.CreateGroupRaw(body)
				gm.Expect(err).ToNot(gm.HaveOccurred())
				gm.Expect(status).To(gm.Equal(http.StatusBadRequest))
			})

			ginkgo.It("should reject name longer than 100 characters", func() {
				body, _ := json.Marshal(v2.CreateGroupRequest{
					Name: strings.Repeat("a", 101), Filter: "memory > 0",
				})
				status, err := agentSvc.CreateGroupRaw(body)
				gm.Expect(err).ToNot(gm.HaveOccurred())
				gm.Expect(status).To(gm.Equal(http.StatusBadRequest))
			})

			ginkgo.It("should reject whitespace-only name", func() {
				body, _ := json.Marshal(v2.CreateGroupRequest{Name: "   ", Filter: "memory > 0"})
				status, err := agentSvc.CreateGroupRaw(body)
				gm.Expect(err).ToNot(gm.HaveOccurred())
				gm.Expect(status).To(gm.Equal(http.StatusBadRequest))
			})

			ginkgo.It("should reject missing filter", func() {
				body, _ := json.Marshal(v2.CreateGroupRequest{Name: "valid-name"})
				status, err := agentSvc.CreateGroupRaw(body)
				gm.Expect(err).ToNot(gm.HaveOccurred())
				gm.Expect(status).To(gm.Equal(http.StatusBadRequest))
			})

			ginkgo.It("should reject invalid filter syntax", func() {
				body, _ := json.Marshal(v2.CreateGroupRequest{
					Name: "valid-name", Filter: "invalid %%% filter",
				})
				status, err := agentSvc.CreateGroupRaw(body)
				gm.Expect(err).ToNot(gm.HaveOccurred())
				gm.Expect(status).To(gm.Equal(http.StatusBadRequest))
			})

			ginkgo.It("should reject description longer than 500 characters", func() {
				desc := strings.Repeat("x", 501)
				body, _ := json.Marshal(v2.CreateGroupRequest{
					Name: "valid-name", Filter: "memory > 0", Description: &desc,
				})
				status, err := agentSvc.CreateGroupRaw(body)
				gm.Expect(err).ToNot(gm.HaveOccurred())
				gm.Expect(status).To(gm.Equal(http.StatusBadRequest))
			})

			ginkgo.It("should reject duplicate group name", func() {
				group, err := agentSvc.CreateGroup("dup-check-v2", "memory > 0", "")
				gm.Expect(err).ToNot(gm.HaveOccurred())
				defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

				body, _ := json.Marshal(v2.CreateGroupRequest{Name: "dup-check-v2", Filter: "memory > 0"})
				status, err := agentSvc.CreateGroupRaw(body)
				gm.Expect(err).ToNot(gm.HaveOccurred())
				gm.Expect(status).To(gm.Equal(http.StatusBadRequest))
			})
		})

		// -----------------------------------------------------------------
		// PATCH /groups/{id} (UpdateGroup)
		// -----------------------------------------------------------------
		ginkgo.Context("PATCH /groups/{id}", func() {
			var groupID string

			ginkgo.BeforeEach(func() {
				group, err := agentSvc.CreateGroup("update-target-v2", "memory > 0", "")
				gm.Expect(err).ToNot(gm.HaveOccurred())
				groupID = group.Id
			})

			ginkgo.AfterEach(func() {
				if groupID != "" {
					_, _ = agentSvc.DeleteGroup(groupID)
					groupID = ""
				}
			})

			ginkgo.It("should reject invalid JSON", func() {
				status, err := agentSvc.UpdateGroupRaw(groupID, []byte("not json"))
				gm.Expect(err).ToNot(gm.HaveOccurred())
				gm.Expect(status).To(gm.Equal(http.StatusBadRequest))
			})

			ginkgo.It("should reject empty body (no fields)", func() {
				body, _ := json.Marshal(map[string]any{})
				status, err := agentSvc.UpdateGroupRaw(groupID, body)
				gm.Expect(err).ToNot(gm.HaveOccurred())
				gm.Expect(status).To(gm.Equal(http.StatusBadRequest))
			})

			ginkgo.It("should reject empty name", func() {
				name := ""
				body, _ := json.Marshal(v2.UpdateGroupRequest{Name: &name})
				status, err := agentSvc.UpdateGroupRaw(groupID, body)
				gm.Expect(err).ToNot(gm.HaveOccurred())
				gm.Expect(status).To(gm.Equal(http.StatusBadRequest))
			})

			ginkgo.It("should reject name longer than 100 characters", func() {
				name := strings.Repeat("a", 101)
				body, _ := json.Marshal(v2.UpdateGroupRequest{Name: &name})
				status, err := agentSvc.UpdateGroupRaw(groupID, body)
				gm.Expect(err).ToNot(gm.HaveOccurred())
				gm.Expect(status).To(gm.Equal(http.StatusBadRequest))
			})

			ginkgo.It("should reject empty filter", func() {
				filter := ""
				body, _ := json.Marshal(v2.UpdateGroupRequest{Filter: &filter})
				status, err := agentSvc.UpdateGroupRaw(groupID, body)
				gm.Expect(err).ToNot(gm.HaveOccurred())
				gm.Expect(status).To(gm.Equal(http.StatusBadRequest))
			})

			ginkgo.It("should reject invalid filter syntax", func() {
				filter := "invalid %%% filter"
				body, _ := json.Marshal(v2.UpdateGroupRequest{Filter: &filter})
				status, err := agentSvc.UpdateGroupRaw(groupID, body)
				gm.Expect(err).ToNot(gm.HaveOccurred())
				gm.Expect(status).To(gm.Equal(http.StatusBadRequest))
			})

			ginkgo.It("should reject description longer than 500 characters", func() {
				desc := strings.Repeat("x", 501)
				body, _ := json.Marshal(v2.UpdateGroupRequest{Description: &desc})
				status, err := agentSvc.UpdateGroupRaw(groupID, body)
				gm.Expect(err).ToNot(gm.HaveOccurred())
				gm.Expect(status).To(gm.Equal(http.StatusBadRequest))
			})

			ginkgo.It("should return 404 for non-existent group", func() {
				name := "new-name"
				body, _ := json.Marshal(v2.UpdateGroupRequest{Name: &name})
				nonExistentUUID := uuid.NewString()
				status, err := agentSvc.UpdateGroupRaw(nonExistentUUID, body)
				gm.Expect(err).ToNot(gm.HaveOccurred())
				gm.Expect(status).To(gm.Equal(http.StatusNotFound))
			})

			ginkgo.It("should return 400 for invalid UUID group id", func() {
				name := "new-name"
				body, _ := json.Marshal(v2.UpdateGroupRequest{Name: &name})
				status, err := agentSvc.UpdateGroupRaw("abc", body)
				gm.Expect(err).ToNot(gm.HaveOccurred())
				gm.Expect(status).To(gm.Equal(http.StatusBadRequest))
			})
		})

		// -----------------------------------------------------------------
		// DELETE /groups/{id} (DeleteGroup)
		// -----------------------------------------------------------------
		ginkgo.Context("DELETE /groups/{id}", func() {
			ginkgo.It("should return 204 for non-existent group (idempotent)", func() {
				nonExistentUUID := uuid.NewString()
				status, err := agentSvc.DeleteGroup(nonExistentUUID)
				gm.Expect(err).ToNot(gm.HaveOccurred())
				gm.Expect(status).To(gm.Equal(http.StatusNoContent))
			})

			ginkgo.It("should return 400 for invalid UUID group id", func() {
				status, err := agentSvc.DeleteGroup("abc")
				gm.Expect(err).ToNot(gm.HaveOccurred())
				gm.Expect(status).To(gm.Equal(http.StatusBadRequest))
			})
		})

		// -----------------------------------------------------------------
		// GET /groups/{id} (GetGroup)
		// -----------------------------------------------------------------
		ginkgo.Context("GET /groups/{id}", func() {
			ginkgo.It("should return 404 for non-existent group", func() {
				nonExistentUUID := uuid.NewString()
				status, err := agentSvc.GetGroupStatus(nonExistentUUID)
				gm.Expect(err).ToNot(gm.HaveOccurred())
				gm.Expect(status).To(gm.Equal(http.StatusNotFound))
			})

			ginkgo.It("should return 400 for invalid UUID group id", func() {
				status, err := agentSvc.GetGroupStatus("abc")
				gm.Expect(err).ToNot(gm.HaveOccurred())
				gm.Expect(status).To(gm.Equal(http.StatusBadRequest))
			})
		})
	})
})
