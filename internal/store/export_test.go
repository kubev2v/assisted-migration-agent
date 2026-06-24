package store

import "testing"

func TestExportStore_SupportedScopes(t *testing.T) {
	st := NewExportStore(nil)
	scopes := st.SupportedScopes()
	want := len(exportScopes) + 1
	if len(scopes) != want {
		t.Fatalf("got %d scopes, want %d", len(scopes), want)
	}
	for scope := range exportScopes {
		if !st.IsValidScope(scope) {
			t.Fatalf("exportScopes contains unregistered scope %q", scope)
		}
	}
	if !st.IsValidScope("utilization") {
		t.Fatal("expected utilization scope to be valid")
	}
	if st.IsValidScope("bogus") {
		t.Fatal("expected bogus scope to be invalid")
	}
}

func TestExportScopes_uniqueFilenames(t *testing.T) {
	seen := make(map[string]string, len(exportScopes))
	for scope, spec := range exportScopes {
		if prev, ok := seen[spec.filename]; ok {
			t.Fatalf("scopes %q and %q both write %q", prev, scope, spec.filename)
		}
		seen[spec.filename] = scope
	}
}
