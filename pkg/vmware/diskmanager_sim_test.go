package vmware_test

import (
	"context"
	"crypto/tls"
	"testing"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/simulator"

	"github.com/kubev2v/assisted-migration-agent/pkg/vmware"
)

func TestFindDatacenterForDatastore(t *testing.T) {
	model := simulator.VPX()
	model.Datacenter = 2
	model.Datastore = 1

	if err := model.Create(); err != nil {
		t.Fatal(err)
	}
	model.Service.TLS = new(tls.Config)
	s := model.Service.NewServer()
	defer s.Close()

	ctx := context.Background()
	gc, err := govmomi.NewClient(ctx, s.URL, true)
	if err != nil {
		t.Fatal(err)
	}
	defer gc.Logout(ctx)

	dm := vmware.NewDiskManager(gc)

	// DC0 has LocalDS_0, DC1 has LocalDS_0 (each DC gets its own)
	// The simulator names datastores "LocalDS_0" per DC.
	// Use the finder to discover actual datastore names.
	t.Run("finds datacenter for datastore in first DC", func(t *testing.T) {
		dc, err := dm.FindDatacenterForDatastore(ctx, "LocalDS_0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dc == nil {
			t.Fatal("expected non-nil datacenter")
		}
		t.Logf("found datacenter: %s", dc.Name())
	})

	t.Run("returns error for nonexistent datastore", func(t *testing.T) {
		_, err := dm.FindDatacenterForDatastore(ctx, "NoSuchDS")
		if err == nil {
			t.Fatal("expected error for nonexistent datastore")
		}
	})

	t.Run("DefaultDatacenter fails with multiple DCs", func(t *testing.T) {
		// This confirms the bug: FindDatacenter("") fails on multi-DC
		_, err := dm.FindDatacenter(ctx, "")
		if err == nil {
			t.Fatal("expected DefaultDatacenter to fail with multiple DCs")
		}
		t.Logf("confirmed DefaultDatacenter failure: %v", err)
	})
}
