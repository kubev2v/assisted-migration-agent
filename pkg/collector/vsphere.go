package collector

import (
	"context"
	"fmt"
	"math/rand"
	"net/url"
	"time"

	api "github.com/kubev2v/forklift/pkg/apis/forklift/v1beta1"
	"github.com/kubev2v/forklift/pkg/controller/provider/container/vsphere"
	"github.com/kubev2v/forklift/pkg/controller/provider/model"
	webprovider "github.com/kubev2v/forklift/pkg/controller/provider/web"
	"github.com/kubev2v/forklift/pkg/controller/provider/web/base"
	web "github.com/kubev2v/forklift/pkg/controller/provider/web/vsphere"
	libcontainer "github.com/kubev2v/forklift/pkg/lib/inventory/container"
	libmodel "github.com/kubev2v/forklift/pkg/lib/inventory/model"
	libweb "github.com/kubev2v/forklift/pkg/lib/inventory/web"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/session"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/soap"
	"go.uber.org/zap"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

type Collector interface {
	VerifyCredentials(ctx context.Context, creds *models.Credentials) error
	Collect(ctx context.Context, creds *models.Credentials) error
	DB() libmodel.DB
	DBPath() string
	Close()
}

type VSphereCollector struct {
	collector *vsphere.Collector
	container *libcontainer.Container
	db        libmodel.DB
	dbPath    string
}

func NewVSphereCollector(dbPath string) *VSphereCollector {
	return &VSphereCollector{
		dbPath: dbPath,
	}
}

func (c *VSphereCollector) VerifyCredentials(ctx context.Context, creds *models.Credentials) error {
	u, err := url.ParseRequestURI(creds.URL)
	if err != nil {
		return err
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/sdk"
	}
	u.User = url.UserPassword(creds.Username, creds.Password)

	verifyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	vimClient, err := vim25.NewClient(verifyCtx, soap.NewClient(u, true))
	if err != nil {
		return err
	}

	client := &govmomi.Client{
		SessionManager: session.NewManager(vimClient),
		Client:         vimClient,
	}

	zap.S().Named("collector").Info("verifying vCenter credentials")
	if err := client.Login(verifyCtx, u.User); err != nil {
		return srvErrors.NewVCenterError(err)
	}

	_ = client.Logout(verifyCtx)
	client.CloseIdleConnections()

	zap.S().Named("collector").Info("vCenter credentials verified successfully")
	return nil
}

func (c *VSphereCollector) Collect(ctx context.Context, creds *models.Credentials) error {
	provider := createProvider(creds)
	secret := createSecret(creds)

	db, err := createDB(provider, c.dbPath)
	if err != nil {
		return err
	}
	c.db = db
	c.collector = vsphere.New(db, provider, secret)

	zap.S().Info("starting forklift vSphere collector")

	container, err := startWebContainer(c.collector)
	if err != nil {
		return err
	}
	c.container = container

	zap.S().Info("forklift vSphere collection completed (parity reached)")
	return nil
}

func (c *VSphereCollector) DB() libmodel.DB {
	return c.db
}

func (c *VSphereCollector) DBPath() string {
	return c.dbPath
}

// Close cleans up collector resources.
func (c *VSphereCollector) Close() {
	if c.container != nil {
		c.container.Delete(c.collector.Owner())
	}
	if c.db != nil {
		_ = c.db.Close(false)
	}
}

// createProvider creates a forklift Provider object from credentials.
func createProvider(creds *models.Credentials) *api.Provider {
	vsphereType := api.VSphere
	return &api.Provider{
		ObjectMeta: meta.ObjectMeta{
			UID: "1",
		},
		Spec: api.ProviderSpec{
			URL:  creds.URL,
			Type: &vsphereType,
		},
	}
}

// createSecret creates a Kubernetes Secret with vCenter credentials.
func createSecret(creds *models.Credentials) *core.Secret {
	return &core.Secret{
		ObjectMeta: meta.ObjectMeta{
			Name:      "vsphere-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"user":               []byte(creds.Username),
			"password":           []byte(creds.Password),
			"insecureSkipVerify": []byte("true"),
		},
	}
}

// createDB creates the SQLite database for the collector.
func createDB(provider *api.Provider, path string) (libmodel.DB, error) {
	models := model.Models(provider)
	db := libmodel.New(path, models...)
	if err := db.Open(true); err != nil {
		return nil, err
	}
	return db, nil
}

// startWebContainer starts the forklift web container which triggers collection.
// It blocks until the collector reaches parity (fully synchronized with vCenter).
func startWebContainer(collector *vsphere.Collector) (*libcontainer.Container, error) {
	container := libcontainer.New()
	if err := container.Add(collector); err != nil {
		return nil, err
	}

	handlers := []libweb.RequestHandler{
		&libweb.SchemaHandler{},
		&webprovider.ProviderHandler{
			Handler: base.Handler{
				Container: container,
			},
		},
	}
	handlers = append(handlers, web.Handlers(container)...)

	// choose an random port to isolate the webserver
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	webServer := libweb.WebServer{
		Port:      r.Intn(65000-10000+1) + 10000,
		Container: container,
		Handlers:  handlers,
	}
	webServer.Start()

	// Wait for collector to reach parity (fully synchronized with vCenter)
	// This matches the migration-planner implementation
	const maxRetries = 300 // 5 minutes timeout (300 * 1 second)
	for i := 0; i < maxRetries; i++ {
		time.Sleep(1 * time.Second)
		if collector.HasParity() {
			zap.S().Debug("collector reached parity")
			return container, nil
		}
		if i > 0 && i%30 == 0 {
			zap.S().Infof("waiting for vSphere collection... (%d seconds)", i)
		}
	}

	return container, fmt.Errorf("timed out waiting for collector parity")
}
