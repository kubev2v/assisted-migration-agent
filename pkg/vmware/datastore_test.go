package vmware

import "testing"

func TestStorageArrayID(t *testing.T) {
	tests := []struct {
		name       string
		naaDevices []string
		want       string
	}{
		{
			name:       "NAA type 6 — NetApp iSCSI",
			naaDevices: []string{"naa.600a098038313954492458313031344c"},
			want:       "naa.600a098038313954",
		},
		{
			name:       "NAA type 6 — same array different LUN",
			naaDevices: []string{"naa.600a0980383139544924583130316a78"},
			want:       "naa.600a098038313954",
		},
		{
			name:       "NAA type 6 — different NetApp array",
			naaDevices: []string{"naa.600a098038314648593f517773636465"},
			want:       "naa.600a098038314648",
		},
		{
			name:       "NAA type 6 — 3PAR",
			naaDevices: []string{"naa.60002ac0000000000000182d00021f6b"},
			want:       "naa.60002ac000000000",
		},
		{
			name:       "NAA type 6 — Pure FlashArray",
			naaDevices: []string{"naa.624a9370a7b9f7ecc01e40f70001181f"},
			want:       "naa.624a9370a7b9f7ec",
		},
		{
			name:       "NAA type 5",
			naaDevices: []string{"naa.50014380205678ab"},
			want:       "naa.50014380",
		},
		{
			name:       "EUI format — Dell ScaleIO",
			naaDevices: []string{"eui.b4f2d5322f73780f5a5beec600000002"},
			want:       "eui.b4f2d5322f73780f",
		},
		{
			name:       "EUI format — same array different LUN",
			naaDevices: []string{"eui.b4f2d5322f73780f5a5c15d400000005"},
			want:       "eui.b4f2d5322f73780f",
		},
		{
			name:       "local disk — mpx",
			naaDevices: []string{"mpx.vmhba0:C0:T1:L0"},
			want:       "",
		},
		{
			name:       "local disk — vml",
			naaDevices: []string{"vml.01000000002020202020"},
			want:       "",
		},
		{
			name:       "empty input",
			naaDevices: nil,
			want:       "",
		},
		{
			name:       "empty slice",
			naaDevices: []string{},
			want:       "",
		},
		{
			name:       "multiple extents — uses first",
			naaDevices: []string{"naa.600a098038313954492458313031344c", "naa.60002ac0000000000000182d00021f6b"},
			want:       "naa.600a098038313954",
		},
		{
			name:       "short NAA6 — returns full string",
			naaDevices: []string{"naa.600a"},
			want:       "naa.600a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StorageArrayID(tt.naaDevices)
			if got != tt.want {
				t.Errorf("StorageArrayID(%v) = %q, want %q", tt.naaDevices, got, tt.want)
			}
		})
	}
}

func TestStorageArrayID_SameArray(t *testing.T) {
	// Datastores that should be on the same array must return the same ID
	sameArray := []string{
		"naa.600a098038313954492458313031344c",
		"naa.600a098038313954492458313031ab12",
		"naa.600a098038313954492458313032ff00",
	}

	id := StorageArrayID([]string{sameArray[0]})
	for _, naa := range sameArray[1:] {
		got := StorageArrayID([]string{naa})
		if got != id {
			t.Errorf("expected same array ID for %s and %s, got %q vs %q", sameArray[0], naa, id, got)
		}
	}

	// Different array must return different ID
	diffArray := "naa.600a098038314648593f517773636465"
	diffID := StorageArrayID([]string{diffArray})
	if diffID == id {
		t.Errorf("expected different array ID for %s vs %s, both got %q", sameArray[0], diffArray, id)
	}
}
