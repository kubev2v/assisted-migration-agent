package sizer

import (
	"errors"
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("cluster", func() {
	Describe("Place", func() {
		When("a bundle has explicit node rules", func() {
			It("places the complete bundle on the allowed node", func() {
				// Arrange
				cluster := newCluster(MachineSet{Name: "worker", CPU: 8, Memory: 32})
				first := cluster.CreateNode(cluster.MachineSets["worker"])
				second := cluster.CreateNode(cluster.MachineSets["worker"])
				bundle := NewPlacementBundle(0)
				bundle.AddService(newPlacementService(Service{Name: "api", RequiredCPU: 1, RequiredMemory: 2}))
				bundle.AddService(newPlacementService(Service{Name: "db", RequiredCPU: 2, RequiredMemory: 3}))
				bundle.AddPlacementRule(PlacementRule{Policy: PlacementAllowed, Name: second.Name})

				// Act
				node, placed := cluster.Place(*bundle)

				// Assert
				Expect(placed).To(BeTrue())
				Expect(node.Name).To(Equal(second.Name))
				Expect(cluster.getNode(first.Name).Services).To(BeEmpty())
				Expect(node.CPU.Requested).To(BeNumerically("~", 3.09, 1e-12))
				Expect(node.Memory.Requested).To(BeNumerically("~", 8.56, 1e-12))
			})
		})

		When("the bundle is not permitted on any node", func() {
			It("rejects a node without a matching allow rule", func() {
				// Arrange
				cluster := newCluster()
				cluster.Nodes = Nodes{{Name: "node", CPU: Resource{Total: 4}, Memory: Resource{Total: 8}}}
				bundle := NewPlacementBundle(0)
				bundle.AddService(newPlacementService(Service{Name: "a", RequiredCPU: 3, RequiredMemory: 1}))

				// Act
				node, placed := cluster.Place(*bundle)

				// Assert
				Expect(placed).To(BeFalse())
				Expect(node).To(Equal(Node{}))
				Expect(cluster.Nodes[0].Services).To(BeEmpty())
			})

			It("rejects a node with an explicit deny rule", func() {
				// Arrange
				cluster := newCluster()
				cluster.Nodes = Nodes{{Name: "node", CPU: Resource{Total: 4}, Memory: Resource{Total: 8}}}
				bundle := NewPlacementBundle(0)
				bundle.AddService(newPlacementService(Service{Name: "a", RequiredCPU: 1, RequiredMemory: 1}))
				bundle.AddPlacementRule(PlacementRule{Policy: PlacementDenied, Name: "node"})

				// Act
				node, placed := cluster.Place(*bundle)

				// Assert
				Expect(placed).To(BeFalse())
				Expect(node).To(Equal(Node{}))
				Expect(cluster.Nodes[0].Services).To(BeEmpty())
			})

			It("rejects a complete bundle that exceeds node capacity", func() {
				// Arrange
				cluster := newCluster()
				cluster.Nodes = Nodes{{Name: "node", CPU: Resource{Total: 4}, Memory: Resource{Total: 8}}}
				bundle := NewPlacementBundle(0)
				bundle.AddService(newPlacementService(Service{Name: "a", RequiredCPU: 3, RequiredMemory: 1}))
				bundle.AddService(newPlacementService(Service{Name: "b", RequiredCPU: 2, RequiredMemory: 1}))
				bundle.AddPlacementRule(PlacementRule{Policy: PlacementAllowed, Name: "node"})

				// Act
				node, placed := cluster.Place(*bundle)

				// Assert
				Expect(placed).To(BeFalse())
				Expect(node).To(Equal(Node{}))
				Expect(cluster.Nodes[0].Services).To(BeEmpty())
			})
		})

		When("the bundle exactly fits the remaining capacity", func() {
			It("accepts the placement", func() {
				// Arrange
				cluster := newCluster()
				cluster.Nodes = Nodes{{Name: "node", CPU: Resource{Requested: 3, Total: 4}, Memory: Resource{Requested: 6, Total: 8}}}
				bundle := NewPlacementBundle(0)
				bundle.AddService(newPlacementService(Service{Name: "api", RequiredCPU: 1, RequiredMemory: 2}))
				bundle.AddPlacementRule(PlacementRule{Policy: PlacementAllowed, Name: "node"})

				// Act
				node, placed := cluster.Place(*bundle)

				// Assert
				Expect(placed).To(BeTrue())
				Expect(node.CPU.Requested).To(Equal(float64(4)))
				Expect(node.Memory.Requested).To(Equal(float64(8)))
			})
		})
	})

	Describe("CreateAndPlace", func() {
		When("eligible machine sets have different usable CPU", func() {
			It("creates a node from the largest eligible machine set", func() {
				// Arrange
				cluster := newCluster(
					MachineSet{Name: "alpha", CPU: 8, Memory: 32},
					MachineSet{Name: "zulu", CPU: 32, Memory: 128},
				)
				bundle := NewPlacementBundle(0)
				bundle.AddService(newPlacementService(Service{Name: "api", RequiredCPU: 1, RequiredMemory: 2}))

				// Act
				node, err := cluster.CreateAndPlace(bundle)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(node.MachineSetName).To(Equal("zulu"))
			})

			It("ignores machine sets the bundle does not allow", func() {
				// Arrange
				cluster := newCluster(
					MachineSet{Name: "alpha", CPU: 8, Memory: 32},
					MachineSet{Name: "zulu", CPU: 32, Memory: 128},
				)
				bundle := NewPlacementBundle(0)
				bundle.AllowedMachineSets = []string{"alpha"}
				bundle.AddService(newPlacementService(Service{Name: "api", RequiredCPU: 1, RequiredMemory: 2}))

				// Act
				node, err := cluster.CreateAndPlace(bundle)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(node.MachineSetName).To(Equal("alpha"))
			})
		})

		When("eligible machine sets tie on usable CPU", func() {
			DescribeTable("deterministically chooses the alphabetically first name",
				func(machineSets []MachineSet) {
					// Arrange
					bundle := NewPlacementBundle(0)
					bundle.AddService(newPlacementService(Service{Name: "api", RequiredCPU: 1, RequiredMemory: 2}))

					// Act
					chosen := make([]string, 0, 10)
					for range 10 {
						cluster := newCluster(machineSets...)
						node, err := cluster.CreateAndPlace(bundle)
						Expect(err).NotTo(HaveOccurred())
						chosen = append(chosen, node.MachineSetName)
					}

					// Assert
					Expect(chosen).To(Equal(slices.Repeat([]string{"alpha"}, 10)))
				},
				Entry("when the winner is registered first", []MachineSet{
					{Name: "alpha", CPU: 16, Memory: 32},
					{Name: "mike", CPU: 16, Memory: 32},
					{Name: "zulu", CPU: 16, Memory: 32},
				}),
				Entry("when the winner is registered last", []MachineSet{
					{Name: "zulu", CPU: 16, Memory: 32},
					{Name: "mike", CPU: 16, Memory: 32},
					{Name: "alpha", CPU: 16, Memory: 32},
				}),
				Entry("when the winner is registered between its peers", []MachineSet{
					{Name: "zulu", CPU: 16, Memory: 32},
					{Name: "alpha", CPU: 16, Memory: 32},
					{Name: "mike", CPU: 16, Memory: 32},
				}),
			)
		})

		When("no eligible machine set fits the bundle", func() {
			It("reports NotEnoughResourcesError", func() {
				// Arrange
				cluster := newCluster(MachineSet{Name: "small", CPU: 2, Memory: 4})
				bundle := NewPlacementBundle(0)
				bundle.AddService(newPlacementService(Service{Name: "api", RequiredCPU: 8, RequiredMemory: 16}))

				// Act
				node, err := cluster.CreateAndPlace(bundle)

				// Assert
				var resourceErr *NotEnoughResourcesError
				Expect(errors.As(err, &resourceErr)).To(BeTrue())
				Expect(node).To(Equal(Node{}))
			})
		})

	})

	Describe("ApplyEligibilityRules", func() {
		When("a node already hosts a service the bundle avoids", func() {
			It("writes an explicit allow or deny rule for every existing node", func() {
				// Arrange
				cluster := newCluster(
					MachineSet{Name: "worker", CPU: 8, Memory: 32},
					MachineSet{Name: "database", CPU: 8, Memory: 32},
					MachineSet{Name: "cp", IsControlPlane: true, CPU: 8, Memory: 32},
				)
				worker := cluster.CreateNode(cluster.MachineSets["worker"])
				database := cluster.CreateNode(cluster.MachineSets["database"])
				controlPlane := cluster.CreateNode(cluster.MachineSets["cp"])
				cluster.getNode(database.Name).Services = []Service{{Name: "postgres"}}

				bundle := NewPlacementBundle(0)
				bundle.AddService(newPlacementService(Service{Name: "api", Avoid: []string{"postgres"}}))

				// Act
				cluster.ApplyEligibilityRules(bundle)

				// Assert
				Expect(bundle.Rules).To(Equal([]PlacementRule{
					{Policy: PlacementAllowed, Name: worker.Name},
					{Policy: PlacementDenied, Name: database.Name},
					{Policy: PlacementDenied, Name: controlPlane.Name},
				}))
			})
		})

		When("the bundle allows only some machine sets", func() {
			It("denies nodes whose machine set is not allowed", func() {
				// Arrange
				cluster := newCluster(
					MachineSet{Name: "worker", CPU: 8, Memory: 32},
					MachineSet{Name: "database", CPU: 8, Memory: 32},
				)
				worker := cluster.CreateNode(cluster.MachineSets["worker"])
				database := cluster.CreateNode(cluster.MachineSets["database"])

				bundle := NewPlacementBundle(0)
				bundle.AllowedMachineSets = []string{"database"}
				bundle.AddService(newPlacementService(Service{Name: "postgres"}))

				// Act
				cluster.ApplyEligibilityRules(bundle)

				// Assert
				Expect(bundle.Rules).To(Equal([]PlacementRule{
					{Policy: PlacementDenied, Name: worker.Name},
					{Policy: PlacementAllowed, Name: database.Name},
				}))
			})
		})
	})

	Describe("ResolveAvoid", func() {
		When("two bundles avoid each other", func() {
			It("records the avoid relation in both directions", func() {
				// Arrange
				first := NewPlacementBundle(0)
				first.AddService(newPlacementService(Service{Name: "api"}))
				second := NewPlacementBundle(1)
				second.AddService(newPlacementService(Service{Name: "postgres", Avoid: []string{"api"}}))

				bundles := []*PlacementBundle{first, second}

				// Act
				for _, bundle := range bundles {
					bundle.ResolveAvoid(bundles)
				}

				// Assert
				Expect(first.AvoidBundleIDs).To(Equal([]int{second.Id}))
				Expect(second.AvoidBundleIDs).To(Equal([]int{first.Id}))
			})
		})
	})

	Describe("CreateNode", func() {
		When("machine sets define kubelet and control-plane reservations", func() {
			It("reserves kubelet capacity everywhere and control-plane capacity only on control-plane nodes", func() {
				// Arrange
				cluster := newCluster(
					MachineSet{
						Name:                          "worker",
						CPU:                           8,
						Memory:                        32,
						ControlPlaneReservedResources: &ControlPlaneResources{CPU: 0.5, Memory: 2.25},
					},
					MachineSet{
						Name:                          "control-plane",
						IsControlPlane:                true,
						CPU:                           8,
						Memory:                        32,
						ControlPlaneReservedResources: &ControlPlaneResources{CPU: 0.5, Memory: 2.25},
					},
				)

				// Act
				worker := cluster.CreateNode(cluster.MachineSets["worker"])
				controlPlane := cluster.CreateNode(cluster.MachineSets["control-plane"])

				// Assert
				Expect(worker.Name).To(Equal("Node 1"))
				Expect(worker.MachineSetName).To(Equal("worker"))
				Expect(worker.CPU.Total).To(Equal(8))
				Expect(worker.Memory.Total).To(Equal(32))
				Expect(worker.CPU.Requested).To(BeNumerically("~", 0.09, 1e-12))
				Expect(worker.Memory.Requested).To(BeNumerically("~", 3.56, 1e-12))
				Expect(worker.Services).To(BeEmpty())

				Expect(controlPlane.Name).To(Equal("Node 2"))
				Expect(controlPlane.IsControlPlane).To(BeTrue())
				Expect(controlPlane.CPU.Requested).To(BeNumerically("~", 0.59, 1e-12))
				Expect(controlPlane.Memory.Requested).To(BeNumerically("~", 5.81, 1e-12))
				Expect(cluster.Nodes).To(HaveLen(2))
			})
		})
	})
})
