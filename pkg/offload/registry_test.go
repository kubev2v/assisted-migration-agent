package offload

import (
	"testing"
)

func TestLookup(t *testing.T) {
	r := NewRegistry()

	tests := []struct {
		name   string
		vendor string
		want   bool // expect non-nil
	}{
		{"known vendor", "NETAPP", true},
		{"case insensitive", "netapp", true},
		{"mixed case", "NetApp", true},
		{"whitespace padded", "  NETAPP  ", true},
		{"pure", "PURE", true},
		{"dell dgc", "DGC", true},
		{"dell emc", "DellEMC", true},
		{"ibm", "IBM", true},
		{"hp legacy", "HP", true},
		{"hpe", "HPE", true},
		{"hitachi", "HITACHI", true},
		{"infinidat", "NFINIDAT", true},
		{"unknown vendor", "ACME", false},
		{"empty string", "", false},
		{"local disk vendor", "ATA", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Lookup(tt.vendor)
			if (got != nil) != tt.want {
				t.Errorf("Lookup(%q) = %v, want non-nil=%v", tt.vendor, got, tt.want)
			}
		})
	}
}

func TestLookup_VendorCapabilities(t *testing.T) {
	r := NewRegistry()

	// Pure does not support RDM
	pure := r.Lookup("PURE")
	if pure == nil {
		t.Fatal("expected PURE profile")
	}
	if pure.RDM {
		t.Error("PURE should not support RDM")
	}
	if !pure.XCOPY {
		t.Error("PURE should support XCOPY")
	}

	// IBM does not support VVol
	ibm := r.Lookup("IBM")
	if ibm == nil {
		t.Fatal("expected IBM profile")
	}
	if ibm.VVol {
		t.Error("IBM should not support VVol")
	}
}

func TestDatastoreCapabilities(t *testing.T) {
	r := NewRegistry()

	tests := []struct {
		name   string
		vendor string
		dsType string
		want   int // expected capability count
	}{
		{"VMFS NetApp — all 4", "NETAPP", "VMFS", 4},
		{"VMFS Pure — 3 (no RDM)", "PURE", "VMFS", 3},
		{"VMFS IBM — 3 (no VVol)", "IBM", "VMFS", 3},
		{"NFS — no caps", "NETAPP", "NFS", 0},
		{"VVol type — no caps", "NETAPP", "VVol", 0},
		{"OTHER type — no caps", "NETAPP", "OTHER", 0},
		{"unknown vendor VMFS — no caps", "ACME", "VMFS", 0},
		{"empty vendor — no caps", "", "VMFS", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caps := r.DatastoreCapabilities(tt.vendor, tt.dsType)
			if len(caps) != tt.want {
				t.Errorf("DatastoreCapabilities(%q, %q) returned %d caps %v, want %d",
					tt.vendor, tt.dsType, len(caps), caps, tt.want)
			}
		})
	}
}

func TestDatastoreCapabilities_Content(t *testing.T) {
	r := NewRegistry()

	caps := r.DatastoreCapabilities("NETAPP", "VMFS")
	capSet := make(map[Capability]bool)
	for _, c := range caps {
		capSet[c] = true
	}

	for _, expected := range []Capability{CopyOffload, XCOPY, RDM, VVol} {
		if !capSet[expected] {
			t.Errorf("expected %q in NETAPP VMFS capabilities", expected)
		}
	}
}

func TestVendorFromNAA(t *testing.T) {
	tests := []struct {
		name string
		naa  string
		want string
	}{
		{"NetApp primary OUI", "naa.600a098038313954492458313031344c", "NETAPP"},
		{"NetApp alternate OUI", "naa.60080e5000000000000000000000abcd", "NETAPP"},
		{"Pure FlashArray", "naa.624a9370a7b9f7ecc01e40f70001181f", "PURE"},
		{"HPE 3PAR/Primera", "naa.6002ac0000000000000000000000abcd", "HP"},
		{"Hitachi VSP", "naa.60060e8000000000000000000000abcd", "HITACHI"},
		{"IBM FlashSystem", "naa.6005076000000000000000000000abcd", "IBM"},
		{"Dell EMC Symmetrix/VMAX", "naa.6000097000000000000000000000abcd", "DGC"},
		{"Dell EMC CLARiiON/VNX", "naa.6006016000000000000000000000abcd", "DGC"},
		{"Dell EMC PowerStore", "naa.68ccf09000000000000000000000abcd", "DellEMC"},
		{"Infinidat InfiniBox", "naa.6742b0f000000000000000000000abcd", "NFINIDAT"},
		{"unknown OUI", "naa.6ffffff000000000000000000000abcd", ""},
		{"NAA type 5 — not supported", "naa.50014380205678ab", ""},
		{"EUI format — not supported", "eui.b4f2d5322f73780f5a5beec600000002", ""},
		{"local disk", "mpx.vmhba0:C0:T1:L0", ""},
		{"empty string", "", ""},
		{"too short NAA6", "naa.600a0", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VendorFromNAA(tt.naa)
			if got != tt.want {
				t.Errorf("VendorFromNAA(%q) = %q, want %q", tt.naa, got, tt.want)
			}
		})
	}
}

func TestVendorFromNAA_AllKnownVendors(t *testing.T) {
	// Ensure every vendor in knownVendors can be resolved from at least one OUI
	r := NewRegistry()
	vendorsCovered := make(map[string]bool)
	for _, oui := range ouiToVendor {
		vendorsCovered[normalizeVendor(oui)] = true
	}

	for _, vp := range knownVendors {
		norm := normalizeVendor(vp.Vendor)
		// HPE and HP share the same OUI — HP is in the table, HPE is not (they share hardware)
		if norm == "HPE" {
			continue
		}
		if !vendorsCovered[norm] {
			t.Errorf("vendor %q in knownVendors has no OUI mapping", vp.Vendor)
		}
	}

	// Verify each OUI maps to a vendor that exists in the registry
	for oui, vendor := range ouiToVendor {
		if r.Lookup(vendor) == nil {
			t.Errorf("OUI %s maps to vendor %q which is not in the registry", oui, vendor)
		}
	}
}

func TestPairCapabilities(t *testing.T) {
	r := NewRegistry()

	sameArray := "naa.600a098038313954"
	diffArray := "naa.600a098038314648"

	tests := []struct {
		name       string
		srcVendor  string
		tgtVendor  string
		srcArrayID string
		tgtArrayID string
		tgtDSType  string
		wantCaps   []Capability
	}{
		{
			name:      "same array same vendor — all caps",
			srcVendor: "NETAPP", tgtVendor: "NETAPP",
			srcArrayID: sameArray, tgtArrayID: sameArray,
			tgtDSType: "VMFS",
			wantCaps:  []Capability{CopyOffload, XCOPY, RDM, VVol},
		},
		{
			name:      "same array Pure — no RDM",
			srcVendor: "PURE", tgtVendor: "PURE",
			srcArrayID: "naa.624a9370a7b9f7ec", tgtArrayID: "naa.624a9370a7b9f7ec",
			tgtDSType: "VMFS",
			wantCaps:  []Capability{CopyOffload, XCOPY, VVol},
		},
		{
			name:      "cross array — only copy-offload",
			srcVendor: "NETAPP", tgtVendor: "NETAPP",
			srcArrayID: sameArray, tgtArrayID: diffArray,
			tgtDSType: "VMFS",
			wantCaps:  []Capability{CopyOffload},
		},
		{
			name:      "NFS target — no caps",
			srcVendor: "NETAPP", tgtVendor: "NETAPP",
			srcArrayID: sameArray, tgtArrayID: sameArray,
			tgtDSType: "NFS",
			wantCaps:  nil,
		},
		{
			name:      "unknown target vendor — no caps",
			srcVendor: "NETAPP", tgtVendor: "ACME",
			srcArrayID: sameArray, tgtArrayID: sameArray,
			tgtDSType: "VMFS",
			wantCaps:  nil,
		},
		{
			name:      "empty array IDs — only copy-offload",
			srcVendor: "NETAPP", tgtVendor: "NETAPP",
			srcArrayID: "", tgtArrayID: "",
			tgtDSType: "VMFS",
			wantCaps:  []Capability{CopyOffload},
		},
		{
			name:      "source empty array — only copy-offload",
			srcVendor: "NETAPP", tgtVendor: "NETAPP",
			srcArrayID: "", tgtArrayID: sameArray,
			tgtDSType: "VMFS",
			wantCaps:  []Capability{CopyOffload},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.PairCapabilities(tt.srcVendor, tt.tgtVendor, tt.srcArrayID, tt.tgtArrayID, tt.tgtDSType)
			if len(got) != len(tt.wantCaps) {
				t.Fatalf("PairCapabilities() = %v (len %d), want %v (len %d)",
					got, len(got), tt.wantCaps, len(tt.wantCaps))
			}
			for i, c := range got {
				if c != tt.wantCaps[i] {
					t.Errorf("cap[%d] = %q, want %q", i, c, tt.wantCaps[i])
				}
			}
		})
	}
}
