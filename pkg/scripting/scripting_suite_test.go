package scripting

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

func TestScripting(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Scripting Suite")
}

func execScript(globals starlark.StringDict, script string) starlark.StringDict {
	thread := &starlark.Thread{Name: "test"}
	result, err := starlark.ExecFileOptions(
		&syntax.FileOptions{TopLevelControl: true},
		thread, "test.star", script, globals,
	)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	return result
}

func intVal(result starlark.StringDict, name string) int64 {
	v, ok := result[name].(starlark.Int)
	ExpectWithOffset(1, ok).To(BeTrue(), "expected %s to be Int, got %T", name, result[name])
	i, _ := v.Int64()
	return i
}

func floatVal(result starlark.StringDict, name string) float64 {
	v, ok := result[name].(starlark.Float)
	ExpectWithOffset(1, ok).To(BeTrue(), "expected %s to be Float, got %T", name, result[name])
	return float64(v)
}

func strVal(result starlark.StringDict, name string) string {
	v, ok := result[name].(starlark.String)
	ExpectWithOffset(1, ok).To(BeTrue(), "expected %s to be String, got %T", name, result[name])
	return string(v)
}

func makeVM(id, name, cluster string, cpuCount, memoryMB int, migratable, template bool) *VirtualMachine {
	return &VirtualMachine{
		id: starlark.String(id), name: starlark.String(name),
		cluster: starlark.String(cluster), datacenter: starlark.String("dc1"),
		cpuCount: starlark.MakeInt(cpuCount), memoryMB: starlark.MakeInt(memoryMB),
		diskSize: starlark.MakeInt(0), powerState: starlark.String("poweredOn"),
		isMigratable: starlark.Bool(migratable), isTemplate: starlark.Bool(template),
		migrationExcluded: false, issueCount: starlark.MakeInt(0),
		inspectionStatus: starlark.String("completed"), inspectionConcernCount: starlark.MakeInt(0),
		labels: starlark.NewList(nil), groups: starlark.NewList(nil), tags: starlark.NewList(nil),
		disks: starlark.NewList(nil), inspectionConcerns: starlark.NewList(nil),
		applications: NewApplicationList(nil),
		nics: starlark.NewList(nil), issues: starlark.NewList(nil), networks: starlark.NewList(nil),
		utilizationCpuP95: starlark.None, utilizationMemP95: starlark.None,
		utilizationCpuMax: starlark.None, utilizationMemMax: starlark.None,
		utilizationDisk: starlark.None, utilizationConfidence: starlark.None,
	}
}
