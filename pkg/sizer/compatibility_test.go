package sizer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// referenceRequest is the fixture side of the TypeScript request schema. The
// fixtures also carry platform and zones fields that the Go API does not model.
type referenceRequest struct {
	MachineSets []MachineSet
	Workloads   []Workload
	Detailed    bool
}

// referenceResponse mirrors the TypeScript response, where node details are
// grouped by zone; the Go API reports a flat node list instead.
type referenceResponse struct {
	NodeCount           int
	TotalCPU            int
	TotalMemory         int
	ResourceConsumption ResourceConsumption
	Advanced            []referenceZone
}

type referenceZone struct {
	Zone  string
	Nodes []Node
}

// referenceCase is one fixture generated from the original TypeScript sizer.
// Checks lists the assertions this port must satisfy; an empty list records the
// reference result without asserting it.
type referenceCase struct {
	Name     string
	Checks   []string
	Note     string
	Request  referenceRequest
	Response struct {
		Success bool
		Error   string
		Data    referenceResponse
	}
}

// floatTolerance absorbs summation-order differences when comparing float64 sums.
const floatTolerance = 1e-9

// knownChecks lists the assertion names a fixture may request.
var knownChecks = map[string]bool{
	"error":       true,
	"nodeCount":   true,
	"totals":      true,
	"consumption": true,
	"nodes":       true,
}

// referenceCases holds the fixtures generated from the original implementation.
var referenceCases = mustLoadReferenceCases()

// mustLoadReferenceCases reads the generated fixtures during suite construction.
func mustLoadReferenceCases() []referenceCase {
	data, err := os.ReadFile("testdata/reference.json")
	if err != nil {
		panic(fmt.Sprintf("could not read reference fixtures: %v", err))
	}

	var cases []referenceCase
	if err := json.Unmarshal(data, &cases); err != nil {
		panic(fmt.Sprintf("could not decode reference fixtures: %v", err))
	}
	if len(cases) == 0 {
		panic("reference fixtures are empty")
	}

	return cases
}

// partitionReferenceCases separates asserted fixtures from recorded divergences.
func partitionReferenceCases(cases []referenceCase) (asserted, unasserted []referenceCase) {
	for _, entry := range cases {
		if len(entry.Checks) == 0 {
			unasserted = append(unasserted, entry)
			continue
		}
		asserted = append(asserted, entry)
	}

	return asserted, unasserted
}

// referenceEntries turns asserted fixtures into table entries.
func referenceEntries(cases []referenceCase) []any {
	entries := make([]any, 0, len(cases))
	for _, entry := range cases {
		entries = append(entries, Entry(entry.Name, entry))
	}

	return entries
}

// referenceChecks validates the fixture's assertion names and returns them as a set.
func referenceChecks(entry referenceCase) map[string]bool {
	GinkgoHelper()

	checks := make(map[string]bool, len(entry.Checks))
	for _, check := range entry.Checks {
		Expect(knownChecks).To(HaveKey(check), entry.Name+": unknown check")
		checks[check] = true
	}

	return checks
}

// assertConsumption compares aggregate requests and reporting-only limits.
func assertConsumption(name string, actual, expected ResourceConsumption) {
	GinkgoHelper()

	Expect(actual.CPU).To(BeNumerically("~", expected.CPU, floatTolerance), name+": consumption CPU")
	Expect(actual.Memory).To(BeNumerically("~", expected.Memory, floatTolerance), name+": consumption memory")

	if !Expect(actual.Limits == nil).To(Equal(expected.Limits == nil), name+": limits presence") {
		return
	}
	if expected.Limits != nil {
		Expect(actual.Limits.CPU).To(BeNumerically("~", expected.Limits.CPU, floatTolerance), name+": limits CPU")
		Expect(actual.Limits.Memory).To(BeNumerically("~", expected.Limits.Memory, floatTolerance), name+": limits memory")
	}

	if !Expect(actual.OverCommitRatio == nil).To(Equal(expected.OverCommitRatio == nil), name+": ratio presence") {
		return
	}
	if expected.OverCommitRatio != nil {
		Expect(actual.OverCommitRatio.CPU).To(BeNumerically("~", expected.OverCommitRatio.CPU, floatTolerance), name+": ratio CPU")
		Expect(actual.OverCommitRatio.Memory).To(BeNumerically("~", expected.OverCommitRatio.Memory, floatTolerance), name+": ratio memory")
	}
}

// assertNodes compares detailed node and service reporting. The reference
// fixtures group nodes by zone, so expected zones are flattened in order.
func assertNodes(name string, actual []Node, expected []referenceZone) {
	GinkgoHelper()

	expectedNodes := make([]Node, 0)
	for _, zone := range expected {
		expectedNodes = append(expectedNodes, zone.Nodes...)
	}

	if !Expect(actual).To(HaveLen(len(expectedNodes)), name+": nodes") {
		return
	}

	for i := range expectedNodes {
		assertNode(name, actual[i], expectedNodes[i])
	}
}

// assertNode compares one reported node.
func assertNode(name string, actual, expected Node) {
	GinkgoHelper()

	label := name + ": node " + expected.Name
	Expect(actual.Name).To(Equal(expected.Name), label+": name")
	Expect(actual.MachineSetName).To(Equal(expected.MachineSetName), label+": machine set")
	Expect(actual.IsControlPlane).To(Equal(expected.IsControlPlane), label+": control plane")
	Expect(actual.CPU.Total).To(Equal(expected.CPU.Total), label+": total CPU")
	Expect(actual.Memory.Total).To(Equal(expected.Memory.Total), label+": total memory")

	Expect(actual.CPU.Requested).To(BeNumerically("~", expected.CPU.Requested, floatTolerance), label+": requested CPU")
	Expect(actual.Memory.Requested).To(BeNumerically("~", expected.Memory.Requested, floatTolerance), label+": requested memory")
	Expect(actual.CPU.Limits).To(BeNumerically("~", expected.CPU.Limits, floatTolerance), label+": limits CPU")
	Expect(actual.Memory.Limits).To(BeNumerically("~", expected.Memory.Limits, floatTolerance), label+": limits memory")
	Expect(actual.CPU.OverCommitRatio).To(BeNumerically("~", expected.CPU.OverCommitRatio, floatTolerance), label+": ratio CPU")
	Expect(actual.Memory.OverCommitRatio).To(BeNumerically("~", expected.Memory.OverCommitRatio, floatTolerance), label+": ratio memory")

	Expect(serviceNames(actual.Services)).To(Equal(serviceNames(expected.Services)), label+": services")
}

// serviceNames returns sorted service names so placement order does not matter.
func serviceNames(services []Service) []string {
	names := make([]string, 0, len(services))
	for _, service := range services {
		names = append(names, service.Name)
	}
	sort.Strings(names)

	return names
}

var _ = Describe("TypeScript reference compatibility", func() {
	asserted, unasserted := partitionReferenceCases(referenceCases)

	table := func(entry referenceCase) {
		// Arrange
		checks := referenceChecks(entry)
		expected := entry.Response.Data

		// Act
		actual, err := NewSizer().Calculate(context.Background(), entry.Request.MachineSets, entry.Request.Workloads, entry.Request.Detailed)

		// Assert
		if checks["error"] {
			Expect(err).To(HaveOccurred())
			return
		}
		Expect(err).NotTo(HaveOccurred())

		if checks["nodeCount"] {
			Expect(actual.NodeCount).To(Equal(expected.NodeCount), entry.Name+": node count")
		}
		if checks["totals"] {
			Expect(actual.TotalCPU).To(Equal(expected.TotalCPU), entry.Name+": total CPU")
			Expect(actual.TotalMemory).To(Equal(expected.TotalMemory), entry.Name+": total memory")
		}
		if checks["consumption"] {
			assertConsumption(entry.Name, actual.ResourceConsumption, expected.ResourceConsumption)
		}
		if checks["nodes"] {
			assertNodes(entry.Name, actual.Nodes, expected.Advanced)
		}
	}

	DescribeTable("matches the recorded reference result", append([]any{table}, referenceEntries(asserted)...)...)

	When("a fixture records a known divergence", func() {
		It("explains why the case asserts nothing", func() {
			Expect(unasserted).NotTo(BeEmpty())

			for _, entry := range unasserted {
				Expect(entry.Note).NotTo(BeEmpty(), entry.Name)
			}
		})
	})
})
