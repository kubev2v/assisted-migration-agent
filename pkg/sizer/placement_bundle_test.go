package sizer

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("placement rules and bundles", func() {
	Describe("PlacementRule", func() {
		DescribeTable("classifies its policy",
			func(rule PlacementRule, allowed, denied bool) {
				// Act
				isAllowed := rule.IsAllowed()
				isDenied := rule.IsDenied()

				// Assert
				Expect(isAllowed).To(Equal(allowed))
				Expect(isDenied).To(Equal(denied))
			},
			Entry("when the rule allows", PlacementRule{Policy: PlacementAllowed}, true, false),
			Entry("when the rule denies", PlacementRule{Policy: PlacementDenied}, false, true),
		)

		When("the rule names a node", func() {
			It("matches only that node name for either policy", func() {
				// Arrange
				node := Node{Name: "node-a", MachineSetName: "workers"}

				// Act
				allowMatch := PlacementRule{Policy: PlacementAllowed, Name: "node-a"}.IsMatch(node)
				denyMatch := PlacementRule{Policy: PlacementDenied, Name: "node-a"}.IsMatch(node)
				machineSetMatch := PlacementRule{Policy: PlacementAllowed, Name: "workers"}.IsMatch(node)

				// Assert
				Expect(allowMatch).To(BeTrue())
				Expect(denyMatch).To(BeTrue())
				Expect(machineSetMatch).To(BeFalse())
			})
		})
	})

	Describe("PlacementBundle", func() {
		When("services and rules are added", func() {
			It("caches totals and copies the caller's slices", func() {
				// Arrange
				services := []Service{
					{Name: "api", RequiredCPU: 1.25, RequiredMemory: 2.5},
					{Name: "worker", RequiredCPU: 0.75, RequiredMemory: 1.5},
				}
				rules := []PlacementRule{{Policy: PlacementDenied, Name: "node-a"}}
				bundle := NewPlacementBundle(0)
				for _, service := range services {
					bundle.AddService(newPlacementService(service))
				}
				for _, rule := range rules {
					bundle.AddPlacementRule(rule)
				}

				// Act
				services[0].RequiredCPU = 99
				rules[0].Name = "node-b"
				returnedServices := bundle.Services()
				returnedServices[0].RequiredMemory = 99

				// Assert
				Expect(bundle.TotalCPU()).To(Equal(float64(2)))
				Expect(bundle.TotalMemory()).To(Equal(float64(4)))
				Expect(bundle.Services()).To(Equal([]Service{
					{Name: "api", RequiredCPU: 1.25, RequiredMemory: 2.5},
					{Name: "worker", RequiredCPU: 0.75, RequiredMemory: 1.5},
				}))
				Expect(bundle.Rules).To(Equal([]PlacementRule{{Policy: PlacementDenied, Name: "node-a"}}))
			})
		})

		When("a bundle holds several services", func() {
			It("removes the identified service and its totals", func() {
				// Arrange
				bundle := NewPlacementBundle(0)
				kept := newPlacementService(Service{Name: "api", RequiredCPU: 1, RequiredMemory: 2})
				removed := newPlacementService(Service{Name: "db", RequiredCPU: 4, RequiredMemory: 8})
				bundle.AddService(kept)
				bundle.AddService(removed)

				// Act
				bundle.RemoveService(removed.ID)

				// Assert
				Expect(bundle.Services()).To(Equal([]Service{{Name: "api", RequiredCPU: 1, RequiredMemory: 2}}))
				Expect(bundle.TotalCPU()).To(Equal(float64(1)))
				Expect(bundle.TotalMemory()).To(Equal(float64(2)))
			})
		})

		When("a node has no placement rule", func() {
			It("denies the node", func() {
				// Arrange
				bundle := NewPlacementBundle(0)

				// Act
				allowed := bundle.IsPlacementAllowed(Node{Name: "node-a"})

				// Assert
				Expect(allowed).To(BeFalse())
			})
		})

		When("rules are added more than once", func() {
			It("keeps one allow and one deny rule per node", func() {
				// Arrange
				bundle := NewPlacementBundle(0)
				allow := PlacementRule{Policy: PlacementAllowed, Name: "node-a"}
				deny := PlacementRule{Policy: PlacementDenied, Name: "node-b"}

				// Act
				bundle.AddPlacementRule(allow)
				bundle.AddPlacementRule(allow)
				bundle.AddPlacementRule(deny)
				bundle.AddPlacementRule(deny)

				// Assert
				Expect(bundle.Rules).To(Equal([]PlacementRule{allow, deny}))
				Expect(bundle.IsPlacementAllowed(Node{Name: "node-a"})).To(BeTrue())
				Expect(bundle.IsPlacementAllowed(Node{Name: "node-b"})).To(BeFalse())
			})

			It("preserves a denial when an allow is added later", func() {
				// Arrange
				bundle := NewPlacementBundle(0)
				node := Node{Name: "node-a"}
				allow := PlacementRule{Policy: PlacementAllowed, Name: node.Name}
				deny := PlacementRule{Policy: PlacementDenied, Name: node.Name}
				bundle.AddPlacementRule(allow)
				Expect(bundle.IsPlacementAllowed(node)).To(BeTrue())

				// Act
				bundle.AddPlacementRule(deny)
				bundle.AddPlacementRule(allow)

				// Assert
				Expect(bundle.Rules).To(Equal([]PlacementRule{deny}))
				Expect(bundle.IsPlacementAllowed(node)).To(BeFalse())
			})
		})
	})
})
