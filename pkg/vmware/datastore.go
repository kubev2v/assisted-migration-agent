package vmware

import "strings"

// StorageArrayID derives a storage array identifier from NAA device IDs.
// Two datastores with the same StorageArrayID are on the same physical array.
//
// For NAA type 6 (enterprise SAN): first 16 hex chars encode OUI + array serial.
// For NAA type 5: first 8 hex chars encode the vendor identifier.
// For EUI (e.g. Dell ScaleIO): first 16 hex chars.
// For local disks (mpx., vml.) or unknown formats: returns "".
func StorageArrayID(naaDevices []string) string {
	if len(naaDevices) == 0 {
		return ""
	}

	dev := naaDevices[0]

	switch {
	case strings.HasPrefix(dev, "naa.6"):
		// NAA type 6: 32 hex chars total. First 16 = OUI + array serial.
		// "naa." = 4 chars, so we need chars 0..19 (prefix + 16 hex)
		if len(dev) >= 20 {
			return dev[:20]
		}
		return dev

	case strings.HasPrefix(dev, "naa.5"):
		// NAA type 5: first 8 hex chars encode the vendor.
		// "naa." = 4 chars, so we need chars 0..12
		if len(dev) >= 12 {
			return dev[:12]
		}
		return dev

	case strings.HasPrefix(dev, "eui."):
		// EUI-64 identifier. First 16 hex chars identify the array.
		// "eui." = 4 chars, so we need chars 0..20
		if len(dev) >= 20 {
			return dev[:20]
		}
		return dev

	default:
		// Local disks (mpx.vmhba0:...), vml., or unknown — cannot determine array
		return ""
	}
}
