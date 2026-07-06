package main

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:staticcheck
	. "github.com/onsi/gomega"    //nolint:staticcheck

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/test/e2e/infra"
	"github.com/kubev2v/assisted-migration-agent/test/e2e/service"

	"github.com/google/uuid"
)

var _ = Describe("API validation e2e tests", Ordered, func() {
	var agentSvc *service.AgentSvc

	BeforeAll(func() {
		GinkgoWriter.Println("Starting postgres...")
		err := infraManager.StartPostgres()
		Expect(err).ToNot(HaveOccurred(), "failed to start postgres")
		time.Sleep(2 * time.Second)

		agentSvc = service.DefaultAgentSvc(cfg.AgentAPIUrl)

		agentID := uuid.NewString()
		GinkgoWriter.Printf("Starting agent %s for validation tests...\n", agentID)
		_, err = infraManager.StartAgent(infra.AgentConfig{
			AgentID:        agentID,
			SourceID:       uuid.NewString(),
			Mode:           "disconnected",
			ConsoleURL:     cfg.AgentProxyUrl,
			UpdateInterval: "1s",
		})
		Expect(err).ToNot(HaveOccurred(), "failed to start agent")

		Eventually(func() error {
			_, err := agentSvc.Status()
			return err
		}, 30*time.Second, 1*time.Second).Should(BeNil(), "agent did not become ready")
	})

	AfterAll(func() {
		GinkgoWriter.Println("Cleaning up validation tests...")
		_ = infraManager.RemoveAgent()
		_ = infraManager.StopPostgres()
	})

	// -----------------------------------------------------------------
	// POST /agent (SetAgentMode)
	// -----------------------------------------------------------------
	Context("POST /agent", func() {
		It("should reject invalid JSON", func() {
			status, err := agentSvc.SetAgentModeRaw([]byte("not json"))
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(http.StatusBadRequest))
		})

		It("should reject missing mode field", func() {
			body, _ := json.Marshal(map[string]any{})
			status, err := agentSvc.SetAgentModeRaw(body)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(http.StatusBadRequest))
		})

		It("should reject invalid mode value", func() {
			body, _ := json.Marshal(map[string]string{"mode": "invalid"})
			status, err := agentSvc.SetAgentModeRaw(body)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(http.StatusBadRequest))
		})

		It("should accept valid mode 'disconnected'", func() {
			body, _ := json.Marshal(map[string]string{"mode": "disconnected"})
			status, err := agentSvc.SetAgentModeRaw(body)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(http.StatusOK))
		})
	})

	// -----------------------------------------------------------------
	// POST /collector (StartCollector)
	// -----------------------------------------------------------------
	Context("POST /collector", func() {
		It("should reject invalid JSON", func() {
			status, err := agentSvc.StartCollectorRaw([]byte("not json"))
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(http.StatusBadRequest))
		})

		It("should reject missing url", func() {
			body, _ := json.Marshal(map[string]string{
				"username": "admin",
				"password": "secret",
			})
			status, err := agentSvc.StartCollectorRaw(body)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(http.StatusBadRequest))
		})

		It("should reject missing username", func() {
			body, _ := json.Marshal(map[string]string{
				"url":      "https://vcenter.example.com/sdk",
				"password": "secret",
			})
			status, err := agentSvc.StartCollectorRaw(body)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(http.StatusBadRequest))
		})

		It("should reject missing password", func() {
			body, _ := json.Marshal(map[string]string{
				"url":      "https://vcenter.example.com/sdk",
				"username": "admin",
			})
			status, err := agentSvc.StartCollectorRaw(body)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(http.StatusBadRequest))
		})

		It("should reject invalid URL format", func() {
			body, _ := json.Marshal(map[string]string{
				"url":      "not-a-valid-url",
				"username": "admin",
				"password": "secret",
			})
			status, err := agentSvc.StartCollectorRaw(body)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(http.StatusBadRequest))
		})
	})

	// -----------------------------------------------------------------
	// POST /vms/inspector (StartInspection)
	// -----------------------------------------------------------------
	Context("POST /vms/inspector", func() {
		It("should reject invalid JSON", func() {
			status, err := agentSvc.StartInspectionRaw([]byte("not json"))
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(http.StatusBadRequest))
		})

		It("should reject empty credentials", func() {
			body, _ := json.Marshal(map[string]any{
				"credentials": map[string]string{
					"url": "", "username": "", "password": "",
				},
				"vmIds": []string{"vm-1"},
			})
			status, err := agentSvc.StartInspectionRaw(body)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(http.StatusBadRequest))
		})

		It("should reject empty vmIds", func() {
			body, _ := json.Marshal(map[string]any{
				"credentials": map[string]string{
					"url": "https://vcenter.example.com/sdk", "username": "admin", "password": "pass",
				},
				"vmIds": []string{},
			})
			status, err := agentSvc.StartInspectionRaw(body)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(http.StatusBadRequest))
		})

		It("should reject invalid URL in credentials", func() {
			body, _ := json.Marshal(map[string]any{
				"credentials": map[string]string{
					"url": "not-a-url", "username": "admin", "password": "pass",
				},
				"vmIds": []string{"vm-1"},
			})
			status, err := agentSvc.StartInspectionRaw(body)
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(http.StatusBadRequest))
		})
	})

	// -----------------------------------------------------------------
	// Group validation (requires collected data for the groups table)
	// -----------------------------------------------------------------
	Context("groups (with collected data)", Ordered, func() {
		BeforeAll(func() {
			GinkgoWriter.Println("Starting vcsim for group validation tests...")
			err := infraManager.StartVcsim()
			Expect(err).ToNot(HaveOccurred(), "failed to start vcsim")

			client := &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
				},
			}
			Eventually(func() error {
				resp, err := client.Get(infra.VcsimURL)
				if err != nil {
					return err
				}
				_ = resp.Body.Close()
				return nil
			}, 30*time.Second, 1*time.Second).Should(BeNil(), "vcsim did not become ready")

			GinkgoWriter.Println("Starting collector...")
			_, err = agentSvc.StartCollector(infra.VcsimURL, infra.VcsimUsername, infra.VcsimPassword)
			Expect(err).ToNot(HaveOccurred(), "failed to start collector")

			Eventually(func() string {
				status, err := agentSvc.GetCollectorStatus()
				if err != nil {
					return "error"
				}
				return status.Status
			}, 120*time.Second, 2*time.Second).Should(Equal("collected"), "collector did not reach collected state")
		})

		AfterAll(func() {
			_ = infraManager.StopVcsim()
		})

		// -----------------------------------------------------------------
		// POST /groups (CreateGroup)
		// -----------------------------------------------------------------
		Context("POST /groups", func() {
			It("should reject invalid JSON", func() {
				status, err := agentSvc.CreateGroupRaw([]byte("not json"))
				Expect(err).ToNot(HaveOccurred())
				Expect(status).To(Equal(http.StatusBadRequest))
			})

			It("should reject missing name", func() {
				body, _ := json.Marshal(v1.CreateGroupRequest{Filter: "memory > 0"})
				status, err := agentSvc.CreateGroupRaw(body)
				Expect(err).ToNot(HaveOccurred())
				Expect(status).To(Equal(http.StatusBadRequest))
			})

			It("should reject name longer than 100 characters", func() {
				body, _ := json.Marshal(v1.CreateGroupRequest{
					Name: strings.Repeat("a", 101), Filter: "memory > 0",
				})
				status, err := agentSvc.CreateGroupRaw(body)
				Expect(err).ToNot(HaveOccurred())
				Expect(status).To(Equal(http.StatusBadRequest))
			})

			It("should reject whitespace-only name", func() {
				body, _ := json.Marshal(v1.CreateGroupRequest{Name: "   ", Filter: "memory > 0"})
				status, err := agentSvc.CreateGroupRaw(body)
				Expect(err).ToNot(HaveOccurred())
				Expect(status).To(Equal(http.StatusBadRequest))
			})

			It("should reject missing filter", func() {
				body, _ := json.Marshal(v1.CreateGroupRequest{Name: "valid-name"})
				status, err := agentSvc.CreateGroupRaw(body)
				Expect(err).ToNot(HaveOccurred())
				Expect(status).To(Equal(http.StatusBadRequest))
			})

			It("should reject invalid filter syntax", func() {
				body, _ := json.Marshal(v1.CreateGroupRequest{
					Name: "valid-name", Filter: "invalid %%% filter",
				})
				status, err := agentSvc.CreateGroupRaw(body)
				Expect(err).ToNot(HaveOccurred())
				Expect(status).To(Equal(http.StatusBadRequest))
			})

			It("should reject description longer than 500 characters", func() {
				desc := strings.Repeat("x", 501)
				body, _ := json.Marshal(v1.CreateGroupRequest{
					Name: "valid-name", Filter: "memory > 0", Description: &desc,
				})
				status, err := agentSvc.CreateGroupRaw(body)
				Expect(err).ToNot(HaveOccurred())
				Expect(status).To(Equal(http.StatusBadRequest))
			})

			It("should reject duplicate group name", func() {
				group, err := agentSvc.CreateGroup("dup-check", "memory > 0", "")
				Expect(err).ToNot(HaveOccurred())
				defer func() { _, _ = agentSvc.DeleteGroup(group.Id.String()) }()

				body, _ := json.Marshal(v1.CreateGroupRequest{Name: "dup-check", Filter: "memory > 0"})
				status, err := agentSvc.CreateGroupRaw(body)
				Expect(err).ToNot(HaveOccurred())
				Expect(status).To(Equal(http.StatusBadRequest))
			})
		})

		// -----------------------------------------------------------------
		// PATCH /groups/{id} (UpdateGroup)
		// -----------------------------------------------------------------
		Context("PATCH /groups/{id}", func() {
			var groupID string

			BeforeEach(func() {
				group, err := agentSvc.CreateGroup("update-target", "memory > 0", "")
				Expect(err).ToNot(HaveOccurred())
				groupID = group.Id.String()
			})

			AfterEach(func() {
				if groupID != "" {
					_, _ = agentSvc.DeleteGroup(groupID)
					groupID = ""
				}
			})

			It("should reject invalid JSON", func() {
				status, err := agentSvc.UpdateGroupRaw(groupID, []byte("not json"))
				Expect(err).ToNot(HaveOccurred())
				Expect(status).To(Equal(http.StatusBadRequest))
			})

			It("should reject empty body (no fields)", func() {
				body, _ := json.Marshal(map[string]any{})
				status, err := agentSvc.UpdateGroupRaw(groupID, body)
				Expect(err).ToNot(HaveOccurred())
				Expect(status).To(Equal(http.StatusBadRequest))
			})

			It("should reject empty name", func() {
				name := ""
				body, _ := json.Marshal(v1.UpdateGroupRequest{Name: &name})
				status, err := agentSvc.UpdateGroupRaw(groupID, body)
				Expect(err).ToNot(HaveOccurred())
				Expect(status).To(Equal(http.StatusBadRequest))
			})

			It("should reject name longer than 100 characters", func() {
				name := strings.Repeat("a", 101)
				body, _ := json.Marshal(v1.UpdateGroupRequest{Name: &name})
				status, err := agentSvc.UpdateGroupRaw(groupID, body)
				Expect(err).ToNot(HaveOccurred())
				Expect(status).To(Equal(http.StatusBadRequest))
			})

			It("should reject empty filter", func() {
				filter := ""
				body, _ := json.Marshal(v1.UpdateGroupRequest{Filter: &filter})
				status, err := agentSvc.UpdateGroupRaw(groupID, body)
				Expect(err).ToNot(HaveOccurred())
				Expect(status).To(Equal(http.StatusBadRequest))
			})

			It("should reject invalid filter syntax", func() {
				filter := "invalid %%% filter"
				body, _ := json.Marshal(v1.UpdateGroupRequest{Filter: &filter})
				status, err := agentSvc.UpdateGroupRaw(groupID, body)
				Expect(err).ToNot(HaveOccurred())
				Expect(status).To(Equal(http.StatusBadRequest))
			})

			It("should reject description longer than 500 characters", func() {
				desc := strings.Repeat("x", 501)
				body, _ := json.Marshal(v1.UpdateGroupRequest{Description: &desc})
				status, err := agentSvc.UpdateGroupRaw(groupID, body)
				Expect(err).ToNot(HaveOccurred())
				Expect(status).To(Equal(http.StatusBadRequest))
			})

			It("should return 404 for non-existent group", func() {
				name := "new-name"
				body, _ := json.Marshal(v1.UpdateGroupRequest{Name: &name})
				nonExistentUUID := uuid.New().String()
				status, err := agentSvc.UpdateGroupRaw(nonExistentUUID, body)
				Expect(err).ToNot(HaveOccurred())
				Expect(status).To(Equal(http.StatusNotFound))
			})

			It("should return 400 for invalid UUID group id", func() {
				name := "new-name"
				body, _ := json.Marshal(v1.UpdateGroupRequest{Name: &name})
				status, err := agentSvc.UpdateGroupRaw("abc", body)
				Expect(err).ToNot(HaveOccurred())
				Expect(status).To(Equal(http.StatusBadRequest))
			})
		})

		// -----------------------------------------------------------------
		// DELETE /groups/{id} (DeleteGroup)
		// -----------------------------------------------------------------
		Context("DELETE /groups/{id}", func() {
			It("should return 204 for non-existent group (idempotent)", func() {
				nonExistentUUID := uuid.New().String()
				status, err := agentSvc.DeleteGroup(nonExistentUUID)
				Expect(err).ToNot(HaveOccurred())
				Expect(status).To(Equal(http.StatusNoContent))
			})

			It("should return 400 for invalid UUID group id", func() {
				status, err := agentSvc.DeleteGroup("abc")
				Expect(err).ToNot(HaveOccurred())
				Expect(status).To(Equal(http.StatusBadRequest))
			})
		})

		// -----------------------------------------------------------------
		// GET /groups/{id} (GetGroup)
		// -----------------------------------------------------------------
		Context("GET /groups/{id}", func() {
			It("should return 404 for non-existent group", func() {
				nonExistentUUID := uuid.New().String()
				status, err := agentSvc.GetGroupStatus(nonExistentUUID)
				Expect(err).ToNot(HaveOccurred())
				Expect(status).To(Equal(http.StatusNotFound))
			})

			It("should return 400 for invalid UUID group id", func() {
				status, err := agentSvc.GetGroupStatus("abc")
				Expect(err).ToNot(HaveOccurred())
				Expect(status).To(Equal(http.StatusBadRequest))
			})
		})
	})
})
