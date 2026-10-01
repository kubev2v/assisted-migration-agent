package services_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/pkg/crypto"
)

func newAccessPasswordService(t *testing.T) (*services.AccessPasswordService, *store.Store) {
	t.Helper()
	pool := store.NewPool(5 * time.Minute)
	t.Cleanup(pool.Close)
	db, err := pool.NewDatabase(store.MainDatabaseID, filepath.Join(t.TempDir(), "agent.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background(), migrations.RunMain); err != nil {
		t.Fatal(err)
	}
	st, err := db.Store()
	if err != nil {
		t.Fatal(err)
	}
	return services.NewAccessPasswordService(st.AccessPassword()), st
}

func TestCreateIsAtomic(t *testing.T) {
	svc, _ := newAccessPasswordService(t)
	has, err := svc.Has(context.Background())
	if err != nil || has {
		t.Fatalf("unexpected initial password state: has=%t err=%v", has, err)
	}
	passwords := []string{"password-one", "password-two"}
	results := make(chan bool, len(passwords))
	errs := make(chan error, len(passwords))
	start := make(chan struct{})
	var wg sync.WaitGroup

	for _, password := range passwords {
		wg.Go(func() {
			<-start
			created, err := svc.Create(context.Background(), password)
			results <- created
			errs <- err
		})
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)

	winners := 0
	for created := range results {
		if created {
			winners++
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("expected one setup winner, got %d", winners)
	}
	has, err = svc.Has(context.Background())
	if err != nil || !has {
		t.Fatalf("password was not configured: has=%t err=%v", has, err)
	}
	created, err := svc.Create(context.Background(), "password-three")
	if err != nil || created {
		t.Fatalf("subsequent setup overwrote password: created=%t err=%v", created, err)
	}

	verified := 0
	for _, password := range passwords {
		ok, err := svc.Verify(context.Background(), password)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			verified++
		}
	}
	if verified != 1 {
		t.Fatalf("expected exactly one configured password, got %d", verified)
	}
}

func TestReplacePreservesCredentials(t *testing.T) {
	svc, st := newAccessPasswordService(t)
	ctx := context.Background()
	crypt := crypto.NewCrypto()
	key := crypt.Hash256("credential-key")
	credentials := models.Credentials{URL: "https://vcenter.example", Username: "admin", Password: "secret"}
	encrypted, err := crypt.Encrypt(key, credentials)
	if err != nil {
		t.Fatal(err)
	}

	if err := st.Credentials().Save(ctx, "credentials", encrypted); err != nil {
		t.Fatal(err)
	}
	before, err := st.Credentials().Get(ctx, "credentials")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, "password-one"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Replace(ctx, "password-two"); err != nil {
		t.Fatal(err)
	}
	after, err := st.Credentials().Get(ctx, "credentials")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("replacing access password changed stored credentials")
	}
	decrypted, err := crypt.Decrypt(key, after)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decrypted, credentials) {
		t.Fatal("credentials no longer decrypt with their persistent key")
	}

	oldOK, err := svc.Verify(ctx, "password-one")
	if err != nil || oldOK {
		t.Fatalf("old password remained valid: ok=%t err=%v", oldOK, err)
	}
	newOK, err := svc.Verify(ctx, "password-two")
	if err != nil || !newOK {
		t.Fatalf("new password is not valid: ok=%t err=%v", newOK, err)
	}
}

func TestRejectsInvalidPasswordsAndCorruptHashes(t *testing.T) {
	svc, st := newAccessPasswordService(t)
	ctx := context.Background()
	for _, password := range []string{"short", strings.Repeat("x", 129), string([]byte{0xff})} {
		if _, err := svc.Create(ctx, password); err == nil {
			t.Fatalf("accepted invalid password %q", password)
		}
	}
	if _, err := svc.Create(ctx, "password-one"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Replace(ctx, "short"); err == nil {
		t.Fatal("accepted invalid replacement password")
	}
	ok, err := svc.Verify(ctx, "password-one")
	if err != nil || !ok {
		t.Fatalf("invalid replacement changed password: ok=%t err=%v", ok, err)
	}

	if err := st.AccessPassword().Replace(ctx, "invalid-hash"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Verify(ctx, "password-one"); err == nil {
		t.Fatal("accepted corrupt password hash")
	}
}

type failingAccessPasswordStore struct {
	store.QueryInterceptor
}

func (failingAccessPasswordStore) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("database unavailable")
}

func TestReplacePropagatesDatabaseFailure(t *testing.T) {
	svc := services.NewAccessPasswordService(store.NewAccessPasswordStore(failingAccessPasswordStore{}))
	if err := svc.Replace(context.Background(), "password-one"); err == nil {
		t.Fatal("expected database error")
	}
}
