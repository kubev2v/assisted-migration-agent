// Package vcsim generates a vcsim XML inventory model from Go structs and
// templates. It has no knowledge of containers or processes: callers (see
// pkg/e2e/infra) own starting/restarting the vcsim container itself and use
// this package to (re)generate the model directory it loads from.
// basemodel/ is the pre-VM-generation vcsim state Generator starts from;
// DefaultInventory (in default_inventory.go) is the standard 50-VM set most
// e2e tests seed it with.
package vcsim

type DiskSpec struct {
	SizeGB int64 `json:"sizeGB"`
}

type AppInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type VAppProduct struct {
	Key         int32  `json:"key"`
	ClassID     string `json:"classId,omitempty"`
	InstanceID  string `json:"instanceId,omitempty"`
	Name        string `json:"name,omitempty"`
	Vendor      string `json:"vendor,omitempty"`
	Version     string `json:"version,omitempty"`
	FullVersion string `json:"fullVersion,omitempty"`
	VendorURL   string `json:"vendorUrl,omitempty"`
	ProductURL  string `json:"productUrl,omitempty"`
	AppURL      string `json:"appUrl,omitempty"`
}

type VM struct {
	Name         string        `json:"name"`
	CPU          int           `json:"cpu"`
	MemoryMB     int64         `json:"memoryMB"`
	Disks        []DiskSpec    `json:"disks"`
	Folder       string        `json:"folder"`
	GuestApps    []AppInfo     `json:"guestApps,omitempty"`
	VAppProducts []VAppProduct `json:"vAppProducts,omitempty"`

	vmID        int
	createTask  int
	powerOnTask int
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
	GuestApps    string
	VAppProducts []VAppProduct
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
