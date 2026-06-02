package models

// VMGuestApps holds a VM's identity and the names of its guest applications.
type VMGuestApps struct {
	ID       string
	Name     string
	AppNames []string
}

// ApplicationVM represents a VM matched to an application.
type ApplicationVM struct {
	ID   string
	Name string
}

// ApplicationOverview represents detected application with matching VM info.
type ApplicationOverview struct {
	Name        string
	Description string
	VMCount     int
	VMs         []ApplicationVM
}

// ApplicationVMRecord represents a single app-to-VM match stored in the database.
type ApplicationVMRecord struct {
	AppName string
	AppDesc string
	VMID    string
	VMName  string
}
