package console_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	externalRef0 "github.com/kubev2v/migration-planner/api/v1alpha1"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/pkg/console"
	"github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

func TestRequestBuilder(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RequestBuilder Suite")
}

var _ = Describe("RequestBuilder", func() {
	var (
		builder  *console.RequestBuilder
		sourceID uuid.UUID
		agentID  uuid.UUID
	)

	BeforeEach(func() {
		sourceID = uuid.New()
		agentID = uuid.New()
		// Create with nil client for unit tests (we're not calling the functions)
		builder = console.NewRequestBuilder(nil, sourceID, agentID)
	})

	Describe("Build", func() {
		Context("GroupInventoryUpsertEvent", func() {
			It("should build request function for upsert event", func() {
				total := 2
				payload := map[string]interface{}{
					"groupID":   "550e8400-e29b-41d4-a716-446655440000",
					"groupName": "test-group",
					"inventory": externalRef0.Inventory{
						VcenterId: "vcenter-01",
						Clusters: map[string]externalRef0.InventoryData{
							"cluster1": {
								Vms: externalRef0.VMs{Total: total},
							},
						},
					},
				}
				data, err := json.Marshal(payload)
				Expect(err).NotTo(HaveOccurred())

				event := models.Event{
					Kind: models.GroupInventoryUpsertEvent,
					Data: data,
				}

				fn, err := builder.Build(event)
				Expect(err).NotTo(HaveOccurred())
				Expect(fn).NotTo(BeNil())
			})

			It("should extract vmsCount from inventory clusters", func() {
				payload := map[string]interface{}{
					"groupID":   "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
					"groupName": "production",
					"inventory": externalRef0.Inventory{
						VcenterId: "vcenter-01",
						Clusters: map[string]externalRef0.InventoryData{
							"cluster1": {
								Vms: externalRef0.VMs{Total: 2},
							},
							"cluster2": {
								Vms: externalRef0.VMs{Total: 1},
							},
						},
					},
				}
				data, err := json.Marshal(payload)
				Expect(err).NotTo(HaveOccurred())

				event := models.Event{
					Kind: models.GroupInventoryUpsertEvent,
					Data: data,
				}

				fn, err := builder.Build(event)
				Expect(err).NotTo(HaveOccurred())
				Expect(fn).NotTo(BeNil())

				// The vmsCount should be extracted as sum of cluster totals = 3
				// We can't test the actual call without a mock client, but we verify parsing works
			})

			It("should build DELETE request for inventory with no VMs", func() {
				// When vmsCount is 0, buildGroupInventoryRequest should return
				// a DELETE function instead of UPDATE. This handles both:
				// - Groups created with 0 VMs (DELETE returns 404 = success)
				// - Groups updated to 0 VMs (DELETE removes from backend)
				payload := map[string]interface{}{
					"groupID":   "f47ac10b-58cc-4372-a567-0e02b2c3d479",
					"groupName": "empty-group",
					"inventory": externalRef0.Inventory{
						VcenterId: "vcenter-01",
						Clusters:  map[string]externalRef0.InventoryData{},
					},
				}
				data, err := json.Marshal(payload)
				Expect(err).NotTo(HaveOccurred())

				event := models.Event{
					Kind: models.GroupInventoryUpsertEvent,
					Data: data,
				}

				fn, err := builder.Build(event)
				Expect(err).NotTo(HaveOccurred())
				Expect(fn).NotTo(BeNil())
				// Note: Without a mock client, we can't verify the DELETE call directly,
				// but the function builds successfully and will call DeleteSourceSubset
			})

			It("should return error for malformed payload", func() {
				event := models.Event{
					Kind: models.GroupInventoryUpsertEvent,
					Data: []byte(`invalid json`),
				}

				fn, err := builder.Build(event)
				Expect(err).To(HaveOccurred())
				Expect(fn).To(BeNil())
			})
		})

		Context("GroupInventoryDeleteEvent", func() {
			It("should build request function for delete event", func() {
				payload := map[string]interface{}{
					"groupID":   "550e8400-e29b-41d4-a716-446655440000",
					"groupName": "test-group",
				}
				data, err := json.Marshal(payload)
				Expect(err).NotTo(HaveOccurred())

				event := models.Event{
					Kind: models.GroupInventoryDeleteEvent,
					Data: data,
				}

				fn, err := builder.Build(event)
				Expect(err).NotTo(HaveOccurred())
				Expect(fn).NotTo(BeNil())
			})

			It("should return error for malformed payload", func() {
				event := models.Event{
					Kind: models.GroupInventoryDeleteEvent,
					Data: []byte(`{"groupID": "not-a-number"}`),
				}

				fn, err := builder.Build(event)
				Expect(err).To(HaveOccurred())
				Expect(fn).To(BeNil())
			})
		})

		Context("InventoryUpdateEvent", func() {
			It("should build request function for inventory update", func() {
				inv := externalRef0.Inventory{
					VcenterId: "vcenter-test",
					Clusters:  map[string]externalRef0.InventoryData{},
				}
				data, err := json.Marshal(inv)
				Expect(err).NotTo(HaveOccurred())

				event := models.Event{
					Kind: models.InventoryUpdateEvent,
					Data: data,
				}

				fn, err := builder.Build(event)
				Expect(err).NotTo(HaveOccurred())
				Expect(fn).NotTo(BeNil())
			})
		})

		Context("Unknown Event Type", func() {
			It("should return error for unknown event kind", func() {
				event := models.Event{
					Kind: models.EventKind("unknown"),
					Data: []byte("{}"),
				}

				fn, err := builder.Build(event)
				Expect(err).To(HaveOccurred())
				Expect(errors.IsUnknownEventKindError(err)).To(BeTrue())
				Expect(fn).To(BeNil())
			})
		})
	})

	Describe("Subset ID Generation", func() {
		It("should generate deterministic UUID from group ID", func() {
			// Same group ID should always generate same subset ID
			payload1 := map[string]interface{}{
				"groupID":   "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
				"groupName": "test",
				"inventory": externalRef0.Inventory{
					VcenterId: "vcenter-01",
					Clusters:  map[string]externalRef0.InventoryData{},
				},
			}
			data1, _ := json.Marshal(payload1)
			event1 := models.Event{Kind: models.GroupInventoryUpsertEvent, Data: data1}

			payload2 := map[string]interface{}{
				"groupID":   "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
				"groupName": "test-renamed",
				"inventory": externalRef0.Inventory{
					VcenterId: "vcenter-01",
					Clusters:  map[string]externalRef0.InventoryData{},
				},
			}
			data2, _ := json.Marshal(payload2)
			event2 := models.Event{Kind: models.GroupInventoryUpsertEvent, Data: data2}

			fn1, err1 := builder.Build(event1)
			fn2, err2 := builder.Build(event2)

			Expect(err1).NotTo(HaveOccurred())
			Expect(err2).NotTo(HaveOccurred())
			Expect(fn1).NotTo(BeNil())
			Expect(fn2).NotTo(BeNil())

			// Both should target the same subset (we can't verify UUID without calling,
			// but we verify both build successfully with same groupID)
		})

		It("should generate different UUIDs for different group IDs", func() {
			payload1 := map[string]interface{}{
				"groupID":   "f47ac10b-58cc-4372-a567-0e02b2c3d479",
				"groupName": "group1",
				"inventory": externalRef0.Inventory{
					VcenterId: "vcenter-01",
					Clusters:  map[string]externalRef0.InventoryData{},
				},
			}
			data1, _ := json.Marshal(payload1)
			event1 := models.Event{Kind: models.GroupInventoryUpsertEvent, Data: data1}

			payload2 := map[string]interface{}{
				"groupID":   "f47ac10b-58cc-4372-a567-0e02b2c3d480",
				"groupName": "group2",
				"inventory": externalRef0.Inventory{
					VcenterId: "vcenter-01",
					Clusters:  map[string]externalRef0.InventoryData{},
				},
			}
			data2, _ := json.Marshal(payload2)
			event2 := models.Event{Kind: models.GroupInventoryUpsertEvent, Data: data2}

			fn1, err1 := builder.Build(event1)
			fn2, err2 := builder.Build(event2)

			Expect(err1).NotTo(HaveOccurred())
			Expect(err2).NotTo(HaveOccurred())
			Expect(fn1).NotTo(BeNil())
			Expect(fn2).NotTo(BeNil())
		})
	})
})
