//go:build ignore

// Generates 50 VMs distributed across 3 subfolders (databases, workload, sap)
// into the vcsim-model directory. Also regenerates the root VM folder (group-2)
// and 3 subfolder XMLs from templates.
//
// Run from this directory: go run generate_vcsim_model.go
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
)

const modelDir = "vcsim-model"

type diskSpec struct {
	sizeGB int64
}

type vmSpec struct {
	index       int
	vmID        int
	name        string
	numCPU      int
	memoryMB    int64
	disks       []diskSpec
	hostID      string
	resPoolID   string
	envBrowser  string
	createTask  int
	powerOnTask int
	folder      string
}

type vmTemplateData struct {
	VMID         int
	Name         string
	NumCPU       int
	MemoryMB     int64
	HostID       string
	ResPoolID    string
	EnvBrowser   string
	PowerOnTask  int
	ParentFolder string
	Timestamp    string
	UUID         string
	InstanceUUID string
	MACAddress   string
	CdromDevName int64
	TotalBytes   int64
	NumDisks     int
	Disks        []diskTemplateData
}

type diskTemplateData struct {
	Index       int
	Key         int
	UnitNumber  int
	SizeGB      int64
	CapacityKB  int64
	CapacityB   int64
	CapacityFmt string
	DiskUUID    string
	VMName      string
	DiskNum     int
	FileKeyBase int
}

type taskTemplateData struct {
	TaskID    int
	VMID      int
	Timestamp string
}

type childRef struct {
	Type string
	ID   string
}

type folderTemplateData struct {
	FolderID   string
	ParentType string
	ParentID   string
	Name       string
	Children   []childRef
	Tasks      []int
}

var vmFolders = []struct {
	id   string
	name string
}{
	{"group-60", "databases"},
	{"group-61", "workload"},
	{"group-62", "sap"},
}

func buildSpecs() []vmSpec {
	memories := []int64{4096, 8192, 16384, 32768, 65536, 131072}
	cpus := []int{1, 2, 4, 8, 16}
	disk1Sizes := []int64{100, 200, 300, 400, 500}
	disk2Sizes := []int64{50, 100, 150, 200, 250}
	disk3Sizes := []int64{25, 50, 75, 100}

	specs := make([]vmSpec, 50)
	for i := 0; i < 50; i++ {
		numDisks := (i % 3) + 1
		disks := make([]diskSpec, numDisks)
		disks[0] = diskSpec{sizeGB: disk1Sizes[i%len(disk1Sizes)]}
		if numDisks >= 2 {
			disks[1] = diskSpec{sizeGB: disk2Sizes[i%len(disk2Sizes)]}
		}
		if numDisks >= 3 {
			disks[2] = diskSpec{sizeGB: disk3Sizes[i%len(disk3Sizes)]}
		}

		host := "host-21"
		resPool := "resgroup-23"
		envBrowser := "envbrowser-22"
		if i%2 == 1 {
			host = "host-37"
			resPool = "resgroup-27"
			envBrowser = "envbrowser-26"
		}

		folder := vmFolders[0].id
		if i >= 17 && i < 34 {
			folder = vmFolders[1].id
		} else if i >= 34 {
			folder = vmFolders[2].id
		}

		specs[i] = vmSpec{
			index:       i,
			vmID:        100 + i,
			name:        fmt.Sprintf("test-vm-%02d", i+1),
			numCPU:      cpus[i%len(cpus)],
			memoryMB:    memories[i%len(memories)],
			disks:       disks,
			hostID:      host,
			resPoolID:   resPool,
			envBrowser:  envBrowser,
			createTask:  200 + i,
			powerOnTask: 300 + i,
			folder:      folder,
		}
	}
	return specs
}

func removeGeneratedFiles() {
	patterns := []string{
		"????-Folder-group-6?.xml",
		"????-VirtualMachine-vm-*.xml",
		"????-Task-task-[23]*.xml",
	}
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(filepath.Join(modelDir, pattern))
		for _, m := range matches {
			_ = os.Remove(m)
		}
	}
}

func formatWithCommas(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

func toVMTemplateData(spec vmSpec) vmTemplateData {
	ts := fmt.Sprintf("2026-02-28T12:00:%02d.000000000Z", spec.index%60)

	var totalBytes int64
	for _, d := range spec.disks {
		totalBytes += d.sizeGB * 1024 * 1024 * 1024
	}

	disks := make([]diskTemplateData, len(spec.disks))
	fileKey := 5
	for i, d := range spec.disks {
		capKB := d.sizeGB * 1024 * 1024
		unitNum := i
		if unitNum >= 7 {
			unitNum++
		}
		disks[i] = diskTemplateData{
			Index:       i,
			Key:         204 + i,
			UnitNumber:  unitNum,
			SizeGB:      d.sizeGB,
			CapacityKB:  capKB,
			CapacityB:   capKB * 1024,
			CapacityFmt: formatWithCommas(capKB),
			DiskUUID:    fmt.Sprintf("6a99%04x-e7cd-5506-871a-%012x", spec.vmID+i, spec.vmID*100+i),
			VMName:      spec.name,
			DiskNum:     i + 1,
			FileKeyBase: fileKey,
		}
		fileKey += 2
	}

	return vmTemplateData{
		VMID:         spec.vmID,
		Name:         spec.name,
		NumCPU:       spec.numCPU,
		MemoryMB:     spec.memoryMB,
		HostID:       spec.hostID,
		ResPoolID:    spec.resPoolID,
		EnvBrowser:   spec.envBrowser,
		PowerOnTask:  spec.powerOnTask,
		ParentFolder: spec.folder,
		Timestamp:    ts,
		UUID:         fmt.Sprintf("564d%04x-abcd-1234-5678-%012x", spec.vmID, spec.vmID),
		InstanceUUID: fmt.Sprintf("5000%04x-dcba-4321-8765-%012x", spec.vmID, spec.vmID),
		MACAddress:   fmt.Sprintf("00:0c:29:%02x:%02x:%02x", (spec.vmID/256)%256, spec.vmID%256, 0),
		CdromDevName: 824634877992 + int64(spec.vmID)*1000,
		TotalBytes:   totalBytes,
		NumDisks:     len(spec.disks),
		Disks:        disks,
	}
}

var funcMap = template.FuncMap{
	"commas": formatWithCommas,
	"add":    func(a, b int) int { return a + b },
}

func loadTemplates() *template.Template {
	return template.Must(template.New("").Funcs(funcMap).ParseGlob("templates/*.tmpl"))
}

func executeTemplate(tmpl *template.Template, name string, data any) string {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		fmt.Fprintf(os.Stderr, "Error executing template %s: %v\n", name, err)
		os.Exit(1)
	}
	return buf.String()
}

func writeFile(filename, content string) {
	p := filepath.Join(modelDir, filename)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", p, err)
		os.Exit(1)
	}
}

func main() {
	specs := buildSpecs()

	fmt.Println("Removing previously generated files...")
	removeGeneratedFiles()

	tmpl := loadTemplates()

	folderVMs := map[string][]int{}
	folderTasks := map[string][]int{}
	allTasks := make([]int, len(specs))
	for i, s := range specs {
		folderVMs[s.folder] = append(folderVMs[s.folder], s.vmID)
		folderTasks[s.folder] = append(folderTasks[s.folder], s.createTask)
		allTasks[i] = s.createTask
	}

	fmt.Println("Generating root VM folder (group-2)...")
	var subfolderChildren []childRef
	for _, f := range vmFolders {
		subfolderChildren = append(subfolderChildren, childRef{Type: "Folder", ID: f.id})
	}
	writeFile("0049-Folder-group-2.xml", executeTemplate(tmpl, "folder.xml.tmpl", folderTemplateData{
		FolderID:   "group-2",
		ParentType: "Datacenter",
		ParentID:   "datacenter-1",
		Name:       "vm",
		Children:   subfolderChildren,
		Tasks:      allTasks,
	}))

	fmt.Println("Generating subfolder XML files...")
	fileIndex := 94
	for _, f := range vmFolders {
		var children []childRef
		for _, vmID := range folderVMs[f.id] {
			children = append(children, childRef{Type: "VirtualMachine", ID: fmt.Sprintf("vm-%d", vmID)})
		}
		writeFile(
			fmt.Sprintf("%04d-Folder-%s.xml", fileIndex, f.id),
			executeTemplate(tmpl, "folder.xml.tmpl", folderTemplateData{
				FolderID:   f.id,
				ParentType: "Folder",
				ParentID:   "group-2",
				Name:       f.name,
				Children:   children,
				Tasks:      folderTasks[f.id],
			}),
		)
		fileIndex++
	}

	fmt.Println("Generating VM and Task XML files...")
	for _, spec := range specs {
		vmData := toVMTemplateData(spec)
		ts := vmData.Timestamp

		writeFile(
			fmt.Sprintf("%04d-VirtualMachine-vm-%d.xml", fileIndex, spec.vmID),
			executeTemplate(tmpl, "vm.xml.tmpl", vmData),
		)
		fileIndex++

		writeFile(
			fmt.Sprintf("%04d-Task-task-%d.xml", fileIndex, spec.createTask),
			executeTemplate(tmpl, "task_create_vm.xml.tmpl", taskTemplateData{
				TaskID:    spec.createTask,
				VMID:      spec.vmID,
				Timestamp: ts,
			}),
		)
		fileIndex++

		writeFile(
			fmt.Sprintf("%04d-Task-task-%d.xml", fileIndex, spec.powerOnTask),
			executeTemplate(tmpl, "task_power_on.xml.tmpl", taskTemplateData{
				TaskID:    spec.powerOnTask,
				VMID:      spec.vmID,
				Timestamp: ts,
			}),
		)
		fileIndex++
	}

	fmt.Printf("Done. Generated 1 root folder + 3 subfolders + %d VMs + %d Tasks = %d files (indices 0049, 0094-%04d)\n",
		len(specs), len(specs)*2, 4+len(specs)*3, fileIndex-1)
	fmt.Printf("Folders: databases (17 VMs), workload (17 VMs), sap (16 VMs)\n")
	fmt.Printf("VMs: 50 (test-vm-01 through test-vm-50)\n")
	fmt.Printf("CPU: 1/2/4/8/16, Memory: 4-128GB, Disks: 1-3\n")
}
