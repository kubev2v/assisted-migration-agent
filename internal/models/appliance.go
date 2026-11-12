package models

type VirtualMachinesIdentites map[string]string

type Appliance struct {
	Name            string
	Vendor          string
	Version         string
	Confidence      int
	FullVersion     string
	VendorUrl       string
	ProductUrl      string
	ApplicationUrl  string
	VirtualMachines VirtualMachinesIdentites
}
