package models

// ApplicationDefinition is a catalog entry used to detect software on VMs.
type ApplicationDefinition struct {
	Name                      string
	Desc                      string
	Vendor                    string
	Category                  string
	Certified                 string
	CertificationComment      string
	ContainerizationCandidate bool
	Match                     string
	Confidence                int
	Processes                 ProcessSignature
	Products                  []ProductSignature
}

type ProcessSignature struct {
	Names      []string
	MinMatched int
}

type ProductSignature struct {
	Names      []string `json:"name_contains"`
	ExactNames []string `json:"name_equals"`
	Vendors    []string `json:"vendor_contains"`
}

// VMGuestApps holds the VM data used by application detection.
type VMGuestApps struct {
	ID       string
	Name     string
	AppNames []string
	Products []AppProduct
}

// ApplicationVM represents a VM matched to an application.
type ApplicationVM struct {
	ID   string
	Name string
}

// ApplicationOverview represents detected application with matching VM info.
type ApplicationOverview struct {
	Name                      string
	Description               string
	Vendor                    string
	Category                  string
	Confidence                int
	ContainerizationCandidate bool
	VMCount                   int
	VMs                       []ApplicationVM
}

// ApplicationVMRecord represents a single app-to-VM match stored in the database.
type ApplicationVMRecord struct {
	AppName     string
	AppDesc     string
	AppVendor   string
	AppCategory string
	Confidence  int
	VMID        string
	VMName      string
}
