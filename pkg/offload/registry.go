// Package offload provides a vendor capability registry for storage copy
// offload methods. It maps SCSI vendor strings (as reported by vSphere) to
// the offload capabilities each vendor's hardware supports.
//
// This package is designed as a clean, self-contained module boundary.
// In the future it will become a separate go module synced with forklift
// releases.
package offload

import "strings"

// Capability is a storage copy offload method.
type Capability string

const (
	CopyOffload Capability = "copy-offload"
	XCOPY       Capability = "xcopy"
	RDM         Capability = "rdm"
	VVol        Capability = "vvol"
)

// VendorProfile describes what offload methods a storage vendor supports.
type VendorProfile struct {
	Vendor      string // canonical SCSI vendor string
	CopyOffload bool
	XCOPY       bool
	RDM         bool
	VVol        bool
}

// Capabilities returns the list of capabilities this vendor supports.
func (v *VendorProfile) Capabilities() []Capability {
	var caps []Capability
	if v.CopyOffload {
		caps = append(caps, CopyOffload)
	}
	if v.XCOPY {
		caps = append(caps, XCOPY)
	}
	if v.RDM {
		caps = append(caps, RDM)
	}
	if v.VVol {
		caps = append(caps, VVol)
	}
	return caps
}

// Registry holds the known vendor profiles. Use NewRegistry to get a
// pre-populated instance.
type Registry struct {
	vendors map[string]VendorProfile
}

// NewRegistry returns a registry pre-populated with all known storage vendors
// and their offload capabilities.
func NewRegistry() *Registry {
	r := &Registry{vendors: make(map[string]VendorProfile)}
	for _, v := range knownVendors {
		r.vendors[normalizeVendor(v.Vendor)] = v
	}
	return r
}

// Lookup returns the VendorProfile for the given SCSI vendor string, or nil
// if the vendor is unknown. Lookup is case-insensitive and trims whitespace
// (SCSI vendor strings are often padded with spaces).
func (r *Registry) Lookup(vendor string) *VendorProfile {
	v, ok := r.vendors[normalizeVendor(vendor)]
	if !ok {
		return nil
	}
	return &v
}

// DatastoreCapabilities returns the intrinsic offload capabilities of a
// single datastore, based on its SCSI vendor string and datastore type.
// Only VMFS datastores support block-level offload; NFS/VVol/other return nil.
func (r *Registry) DatastoreCapabilities(vendor, dsType string) []Capability {
	if dsType != "VMFS" {
		return nil
	}
	vp := r.Lookup(vendor)
	if vp == nil {
		return nil
	}
	return vp.Capabilities()
}

// PairCapabilities computes which offload methods are feasible for a
// source→target datastore pair.
//
// Rules:
//   - copy-offload: target is VMFS and target vendor supports CopyOffload
//   - xcopy: target vendor supports XCOPY and both datastores are on the same
//     storage array (same non-empty storageArrayId)
//   - rdm: same-array and target vendor supports RDM
//   - vvol: same-array and target vendor supports VVol
func (r *Registry) PairCapabilities(srcVendor, tgtVendor, srcArrayID, tgtArrayID, tgtDSType string) []Capability { //nolint:revive // srcVendor reserved for future per-vendor source constraints
	if tgtDSType != "VMFS" {
		return nil
	}

	tgt := r.Lookup(tgtVendor)
	if tgt == nil {
		return nil
	}

	var caps []Capability

	if tgt.CopyOffload {
		caps = append(caps, CopyOffload)
	}

	sameArray := srcArrayID != "" && srcArrayID == tgtArrayID

	if sameArray && tgt.XCOPY {
		caps = append(caps, XCOPY)
	}
	if sameArray && tgt.RDM {
		caps = append(caps, RDM)
	}
	if sameArray && tgt.VVol {
		caps = append(caps, VVol)
	}

	return caps
}

// VendorFromNAA derives the SCSI vendor string from a NAA device identifier
// by extracting the IEEE OUI (Organizationally Unique Identifier) embedded
// in the NAA prefix and looking it up in a known mapping.
//
// For NAA type 6 (naa.6...): bytes 1-3 (hex chars 1-6 after "naa.6") encode
// the OUI of the storage array manufacturer.
//
// Returns "" for unknown OUIs, non-NAA-6 formats, or empty input.
func VendorFromNAA(naaDevice string) string {
	if !strings.HasPrefix(naaDevice, "naa.6") {
		return ""
	}

	// "naa.6" = 5 chars, OUI is next 6 hex chars → need at least 11 chars
	if len(naaDevice) < 11 {
		return ""
	}

	oui := strings.ToUpper(naaDevice[5:11])
	if vendor, ok := ouiToVendor[oui]; ok {
		return vendor
	}
	return ""
}

// ouiToVendor maps IEEE OUI prefixes (from NAA type 6 identifiers) to the
// canonical SCSI vendor string used in knownVendors. Derived from the
// forklift vsphere-copy-offload-populator vendor list.
var ouiToVendor = map[string]string{
	"00A098": "NETAPP",   // NetApp ONTAP (primary OUI)
	"0080E5": "NETAPP",   // NetApp (alternate OUI)
	"24A937": "PURE",     // Pure Storage FlashArray
	"002AC0": "HP",       // HPE 3PAR / Primera / Alletra
	"0060E8": "HITACHI",  // Hitachi Vantara VSP
	"005076": "IBM",      // IBM FlashSystem / SVC
	"000097": "DGC",      // Dell EMC Symmetrix / VMAX / PowerMax
	"006016": "DGC",      // Dell EMC CLARiiON / VNX / Unity
	"8CCF09": "DellEMC",  // Dell EMC PowerStore
	"742B0F": "NFINIDAT", // Infinidat InfiniBox
}

func normalizeVendor(v string) string {
	return strings.ToUpper(strings.TrimSpace(v))
}

// knownVendors lists all storage vendors supported by forklift, mapped to
// the SCSI vendor strings reported by vSphere's HostStorageSystem.
//
// When this package becomes a standalone module, this table will be
// generated from the forklift vendor registry at release time.
var knownVendors = []VendorProfile{
	{Vendor: "NETAPP", CopyOffload: true, XCOPY: true, RDM: true, VVol: true},
	{Vendor: "PURE", CopyOffload: true, XCOPY: true, RDM: false, VVol: true},
	{Vendor: "DGC", CopyOffload: true, XCOPY: true, RDM: true, VVol: true},      // Dell EMC CLARiiON/VNX
	{Vendor: "DellEMC", CopyOffload: true, XCOPY: true, RDM: true, VVol: true},  // Dell EMC PowerStore/Unity
	{Vendor: "IBM", CopyOffload: true, XCOPY: true, RDM: true, VVol: false},     // IBM FlashSystem/SVC
	{Vendor: "HP", CopyOffload: true, XCOPY: true, RDM: true, VVol: true},       // HPE legacy
	{Vendor: "HPE", CopyOffload: true, XCOPY: true, RDM: true, VVol: true},      // HPE current
	{Vendor: "HITACHI", CopyOffload: true, XCOPY: true, RDM: true, VVol: true},  // Hitachi VSP
	{Vendor: "NFINIDAT", CopyOffload: true, XCOPY: true, RDM: true, VVol: true}, // Infinidat InfiniBox
}
