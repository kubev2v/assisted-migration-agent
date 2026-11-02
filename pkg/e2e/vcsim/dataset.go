package vcsim

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"text/template"
	"time"
)

//go:embed basemodel/*
var baseModelFS embed.FS

//go:embed templates/*
var templatesFS embed.FS

const (
	folderTemplate       = "folder.xml.tmpl"
	vmTemplate           = "vm.xml.tmpl"
	taskCreateVMTemplate = "task_create_vm.xml.tmpl"
	taskPowerOnTemplate  = "task_power_on.xml.tmpl"
)

type Dataset struct {
	mu        sync.Mutex
	vms       []VM
	nextVMID  int
	nextGroup int
	outputDir string
	tmpl      *template.Template
}

func NewDataset(outputDir string) (*Dataset, error) {
	funcMap := template.FuncMap{
		"commas": formatWithCommas,
		"add":    func(a, b int) int { return a + b },
	}

	tmpl, err := template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/*.tmpl")
	if err != nil {
		return nil, fmt.Errorf("parsing templates: %w", err)
	}

	return &Dataset{
		vms:       make([]VM, 0),
		nextVMID:  100,
		nextGroup: 60,
		outputDir: outputDir,
		tmpl:      tmpl,
	}, nil
}

func (d *Dataset) AddVMs(vms []VM) []VM {
	d.mu.Lock()
	defer d.mu.Unlock()

	for i := range vms {
		if vms[i].CPU == 0 {
			vms[i].CPU = 2
		}
		if vms[i].MemoryMB == 0 {
			vms[i].MemoryMB = 4096
		}
		if len(vms[i].Disks) == 0 {
			vms[i].Disks = []DiskSpec{{SizeGB: 100}}
		}

		vms[i].vmID = d.nextVMID
		vms[i].createTask = d.nextVMID + 100
		vms[i].powerOnTask = d.nextVMID + 200
		d.nextVMID++

		d.vms = append(d.vms, vms[i])
	}

	return vms
}

func (d *Dataset) RemoveVM(name string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	for i, vm := range d.vms {
		if vm.Name == name {
			d.vms = append(d.vms[:i], d.vms[i+1:]...)
			return true
		}
	}
	return false
}

func (d *Dataset) ListVMs() []VM {
	d.mu.Lock()
	defer d.mu.Unlock()

	result := make([]VM, len(d.vms))
	copy(result, d.vms)
	return result
}

func (d *Dataset) GenerateXML() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := os.RemoveAll(d.outputDir); err != nil {
		return fmt.Errorf("clearing output dir: %w", err)
	}
	if err := os.MkdirAll(d.outputDir, 0755); err != nil {
		return fmt.Errorf("creating output dir: %w", err)
	}

	if err := d.copyBaseModel(); err != nil {
		return fmt.Errorf("copying base model: %w", err)
	}

	if err := d.generateFolders(); err != nil {
		return fmt.Errorf("generating folders: %w", err)
	}

	if err := d.generateVMs(); err != nil {
		return fmt.Errorf("generating VMs: %w", err)
	}

	return nil
}

func (d *Dataset) copyBaseModel() error {
	return fs.WalkDir(baseModelFS, "basemodel", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel("basemodel", path)
		target := filepath.Join(d.outputDir, rel)

		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}

		data, err := baseModelFS.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
}

func (d *Dataset) generateFolders() error {
	folders := map[string][]VM{}
	for _, vm := range d.vms {
		folder := vm.Folder
		if folder == "" {
			folder = "vm"
		}
		folders[folder] = append(folders[folder], vm)
	}

	var rootChildren []childRef
	var allTasks []int

	groupID := 60
	folderIDs := map[string]string{}

	for folderName := range folders {
		if folderName == "vm" {
			continue
		}
		id := fmt.Sprintf("group-%d", groupID)
		folderIDs[folderName] = id
		rootChildren = append(rootChildren, childRef{Type: "Folder", ID: id})
		groupID++
	}

	for _, vm := range d.vms {
		allTasks = append(allTasks, vm.createTask)
	}

	if vms, ok := folders["vm"]; ok {
		for _, vm := range vms {
			rootChildren = append(rootChildren, childRef{Type: "VirtualMachine", ID: fmt.Sprintf("vm-%d", vm.vmID)})
		}
	}

	rootFolder := folderTemplateData{
		FolderID:   "group-2",
		ParentType: "Datacenter",
		ParentID:   "datacenter-1",
		Name:       "vm",
		Children:   rootChildren,
		Tasks:      allTasks,
	}
	if err := d.writeTemplate(folderTemplate, "0049-Folder-group-2.xml", rootFolder); err != nil {
		return err
	}

	fileIndex := 94
	for folderName, vms := range folders {
		if folderName == "vm" {
			continue
		}
		id := folderIDs[folderName]
		var children []childRef
		var tasks []int
		for _, vm := range vms {
			children = append(children, childRef{Type: "VirtualMachine", ID: fmt.Sprintf("vm-%d", vm.vmID)})
			tasks = append(tasks, vm.createTask)
		}

		data := folderTemplateData{
			FolderID:   id,
			ParentType: "Folder",
			ParentID:   "group-2",
			Name:       folderName,
			Children:   children,
			Tasks:      tasks,
		}
		if err := d.writeTemplate(folderTemplate, fmt.Sprintf("%04d-Folder-%s.xml", fileIndex, id), data); err != nil {
			return err
		}
		fileIndex++
	}

	return nil
}

func (d *Dataset) generateVMs() error {
	fileIndex := 100
	for i, vm := range d.vms {
		vmData := d.toVMTemplateData(vm, i)

		if err := d.writeTemplate(vmTemplate, fmt.Sprintf("%04d-VirtualMachine-vm-%d.xml", fileIndex, vm.vmID), vmData); err != nil {
			return err
		}
		fileIndex++

		taskData := taskTemplateData{
			TaskID:    vm.createTask,
			VMID:      vm.vmID,
			Timestamp: vmData.Timestamp,
		}
		if err := d.writeTemplate(taskCreateVMTemplate, fmt.Sprintf("%04d-Task-task-%d.xml", fileIndex, vm.createTask), taskData); err != nil {
			return err
		}
		fileIndex++

		taskData = taskTemplateData{
			TaskID:    vm.powerOnTask,
			VMID:      vm.vmID,
			Timestamp: vmData.Timestamp,
		}
		if err := d.writeTemplate(taskPowerOnTemplate, fmt.Sprintf("%04d-Task-task-%d.xml", fileIndex, vm.powerOnTask), taskData); err != nil {
			return err
		}
		fileIndex++
	}
	return nil
}

func (d *Dataset) toVMTemplateData(vm VM, index int) vmTemplateData {
	ts := time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")

	var totalBytes int64
	for _, d := range vm.Disks {
		totalBytes += d.SizeGB * 1024 * 1024 * 1024
	}

	disks := make([]diskTemplateData, len(vm.Disks))
	fileKey := 5
	for i, d := range vm.Disks {
		capKB := d.SizeGB * 1024 * 1024
		unitNum := i
		if unitNum >= 7 {
			unitNum++
		}
		disks[i] = diskTemplateData{
			Index:       i,
			Key:         204 + i,
			UnitNumber:  unitNum,
			SizeGB:      d.SizeGB,
			CapacityKB:  capKB,
			CapacityB:   capKB * 1024,
			CapacityFmt: formatWithCommas(capKB),
			DiskUUID:    fmt.Sprintf("6a99%04x-e7cd-5506-871a-%012x", vm.vmID+i, vm.vmID*100+i),
			VMName:      vm.Name,
			DiskNum:     i + 1,
			FileKeyBase: fileKey,
		}
		fileKey += 2
	}

	parentFolder := "group-2"
	if vm.Folder != "" && vm.Folder != "vm" {
		parentFolder = d.folderIDForName(vm.Folder)
	}

	hostID := "host-21"
	resPoolID := "resgroup-23"
	envBrowser := "envbrowser-22"
	if index%2 == 1 {
		hostID = "host-37"
		resPoolID = "resgroup-27"
		envBrowser = "envbrowser-26"
	}

	var guestApps string
	if len(vm.GuestApps) > 0 {
		guestApps = `{"applications":[`
		for j, app := range vm.GuestApps {
			if j > 0 {
				guestApps += ","
			}
			guestApps += fmt.Sprintf(`{"a":"%s","v":"%s"}`, app.Name, app.Version)
		}
		guestApps += `]}`
	}

	return vmTemplateData{
		VMID:         vm.vmID,
		Name:         vm.Name,
		NumCPU:       vm.CPU,
		MemoryMB:     vm.MemoryMB,
		HostID:       hostID,
		ResPoolID:    resPoolID,
		EnvBrowser:   envBrowser,
		PowerOnTask:  vm.powerOnTask,
		ParentFolder: parentFolder,
		Timestamp:    ts,
		UUID:         fmt.Sprintf("564d%04x-abcd-1234-5678-%012x", vm.vmID, vm.vmID),
		InstanceUUID: fmt.Sprintf("5000%04x-dcba-4321-8765-%012x", vm.vmID, vm.vmID),
		MACAddress:   fmt.Sprintf("00:0c:29:%02x:%02x:%02x", (vm.vmID/256)%256, vm.vmID%256, 0),
		CdromDevName: 824634877992 + int64(vm.vmID)*1000,
		TotalBytes:   totalBytes,
		NumDisks:     len(vm.Disks),
		Disks:        disks,
		GuestApps:    guestApps,
		VAppProducts: vm.VAppProducts,
	}
}

func (d *Dataset) folderIDForName(name string) string {
	groupID := 60
	seen := map[string]bool{}
	for _, vm := range d.vms {
		folder := vm.Folder
		if folder == "" || folder == "vm" {
			continue
		}
		if seen[folder] {
			continue
		}
		if folder == name {
			return fmt.Sprintf("group-%d", groupID)
		}
		seen[folder] = true
		groupID++
	}
	return "group-2"
}

func (d *Dataset) writeTemplate(tmplName, filename string, data any) error {
	var buf bytes.Buffer
	if err := d.tmpl.ExecuteTemplate(&buf, tmplName, data); err != nil {
		return fmt.Errorf("executing template %s: %w", tmplName, err)
	}
	return os.WriteFile(filepath.Join(d.outputDir, filename), buf.Bytes(), 0644)
}
