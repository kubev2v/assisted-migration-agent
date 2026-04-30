package rightsizing

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/performance"
	"github.com/vmware/govmomi/session"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
)

// DesiredMetrics is the ordered list of vSphere counter names queried per VM.
// vSphere units:
//   - cpu.usagemhz.average    MHz consumed by the VM
//   - cpu.usage.average       hundredths of a percent (5000 = 50.00 %)
//   - mem.active.average      KB of memory actively used by the guest
//   - mem.consumed.average    KB consumed (guest + overhead)
//   - disk.used.latest        KB of disk actually used
//   - disk.provisioned.latest KB of disk provisioned (thin + thick)
var DesiredMetrics = []string{
	"cpu.usagemhz.average",
	"cpu.usage.average",
	"mem.active.average",
	"mem.consumed.average",
	"disk.used.latest",
	"disk.provisioned.latest",
}

// Config holds the parameters for a single rightsizing collection run.
// Insecure should be false in production. The dev CLI defaults to true.
type Config struct {
	VCenterURL string
	Username   string
	Password   string
	Insecure   bool
	NameFilter string
	ClusterID  string
	Lookback   time.Duration
	IntervalID int
	BatchSize  int
}

// VMInfo carries the display name and MoRef of a discovered VM.
type VMInfo struct {
	Name string
	Ref  types.ManagedObjectReference
}

// VMReport holds aggregated metric stats for one VM.
type VMReport struct {
	Name     string
	MOID     string
	Metrics  map[string]MetricStats
	Warnings []string
}

// Connect authenticates a govmomi client. Never logs the password.
func Connect(ctx context.Context, cfg Config) (*govmomi.Client, error) {
	u, err := soap.ParseURL(cfg.VCenterURL)
	if err != nil {
		return nil, fmt.Errorf("invalid vCenter URL: %w", err)
	}
	u.User = url.UserPassword(cfg.Username, cfg.Password)

	soapClient := soap.NewClient(u, cfg.Insecure)
	vimClient, err := vim25.NewClient(ctx, soapClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create vim25 client: %w", err)
	}
	client := &govmomi.Client{
		Client:         vimClient,
		SessionManager: session.NewManager(vimClient),
	}
	if err := client.Login(ctx, u.User); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}
	return client, nil
}

// DiscoverVMs lists VMs from vCenter, preferring powered-on VMs, filtered by
// name substring (when cfg.NameFilter is set).
func DiscoverVMs(ctx context.Context, client *govmomi.Client, cfg Config) ([]VMInfo, error) {
	container := client.ServiceContent.RootFolder
	if cfg.ClusterID != "" {
		container = types.ManagedObjectReference{
			Type:  "ClusterComputeResource",
			Value: cfg.ClusterID,
		}
	}

	m := view.NewManager(client.Client)
	v, err := m.CreateContainerView(ctx, container, []string{"VirtualMachine"}, true)
	if err != nil {
		return nil, fmt.Errorf("failed to create container view: %w", err)
	}
	defer func() { _ = v.Destroy(ctx) }()

	var vms []mo.VirtualMachine
	if err := v.Retrieve(ctx, []string{"VirtualMachine"}, []string{"name", "runtime.powerState"}, &vms); err != nil {
		return nil, fmt.Errorf("failed to retrieve VMs: %w", err)
	}

	var poweredOn, other []mo.VirtualMachine
	for _, vm := range vms {
		if vm.Runtime.PowerState == types.VirtualMachinePowerStatePoweredOn {
			poweredOn = append(poweredOn, vm)
		} else {
			other = append(other, vm)
		}
	}
	ordered := make([]mo.VirtualMachine, 0, len(vms))
	ordered = append(ordered, poweredOn...)
	ordered = append(ordered, other...)

	var result []VMInfo
	for _, vm := range ordered {
		if cfg.NameFilter != "" && !strings.Contains(vm.Name, cfg.NameFilter) {
			continue
		}
		result = append(result, VMInfo{Name: vm.Name, Ref: vm.Self})
	}
	return result, nil
}

//TODO: QueryMetrics is long, has high cyclomatic complexity and has too many reponsibilites. It should be refactored according to clean code principles.

// QueryMetrics queries historical performance metrics for all VMs in batches.
// Counter info is resolved once; one QueryPerf call is made per batch of cfg.BatchSize VMs.
// The returned map is keyed by VM MoRef value and always contains an entry for every input VM.
func QueryMetrics(ctx context.Context, client *govmomi.Client, vms []VMInfo, cfg Config, start, end time.Time) (map[string]VMReport, []string) {
	pm := performance.NewManager(client.Client)

	countersByName, err := pm.CounterInfoByName(ctx)
	if err != nil {
		return nil, []string{fmt.Sprintf("failed to get counter info: %v", err)}
	}
	countersByKey, err := pm.CounterInfoByKey(ctx)
	if err != nil {
		return nil, []string{fmt.Sprintf("failed to get counter info by key: %v", err)}
	}

	var metricIDs []types.PerfMetricId
	var globalWarnings []string
	for _, name := range DesiredMetrics {
		info, ok := countersByName[name]
		if !ok {
			globalWarnings = append(globalWarnings, fmt.Sprintf("metric %q not recognized by this vCenter", name))
			continue
		}
		metricIDs = append(metricIDs, types.PerfMetricId{CounterId: info.Key, Instance: ""})
	}
	if len(metricIDs) == 0 {
		return nil, append(globalWarnings, "no desired metrics recognized by this vCenter")
	}

	maxSamples := max(int32(cfg.Lookback/(time.Duration(cfg.IntervalID)*time.Second)), 1)
	results := make(map[string]VMReport, len(vms))

	for i := 0; i < len(vms); i += cfg.BatchSize {
		batch := vms[i:min(i+cfg.BatchSize, len(vms))]
		specs := make([]types.PerfQuerySpec, len(batch))
		for j, vm := range batch {
			s, e := start, end
			specs[j] = types.PerfQuerySpec{
				Entity:     vm.Ref,
				IntervalId: int32(cfg.IntervalID),
				MetricId:   metricIDs,
				StartTime:  &s,
				EndTime:    &e,
				MaxSample:  maxSamples,
			}
		}

		// TODO: construct performance.Manager once per command run and pass it into this function.
		// Currently a fresh Manager is created per QueryMetrics call, bypassing its internal
		// CounterInfoByName cache and incurring one extra vCenter round-trip per call. Also batch
		// the VM refs so SampleByName is called once for all VMs instead of once per VM.
		raw, err := pm.Query(ctx, specs)
		if err != nil {
			for _, vm := range batch {
				results[vm.Ref.Value] = VMReport{
					Name:     vm.Name,
					MOID:     vm.Ref.Value,
					Warnings: []string{fmt.Sprintf("batch query failed: %v", err)},
				}
			}
			continue
		}

		for _, base := range raw {
			em, ok := base.(*types.PerfEntityMetric)
			if !ok {
				continue
			}
			moid := em.Entity.Value
			metrics := make(map[string]MetricStats)
			var warnings []string
			for _, v := range em.Value {
				series, ok := v.(*types.PerfMetricIntSeries)
				if !ok {
					continue
				}
				info, ok := countersByKey[series.Id.CounterId]
				if !ok {
					continue
				}
				name := info.Name()
				if _, exists := metrics[name]; exists {
					continue
				}
				if len(series.Value) == 0 {
					warnings = append(warnings, fmt.Sprintf("metric %q returned no samples", name))
					continue
				}
				metrics[name] = ComputeStats(series.Value)
			}
			if len(metrics) == 0 && len(warnings) == 0 {
				warnings = append(warnings, "query succeeded but returned no samples")
			}
			results[moid] = VMReport{MOID: moid, Metrics: metrics, Warnings: warnings}
		}

		for _, vm := range batch {
			if r, exists := results[vm.Ref.Value]; !exists {
				results[vm.Ref.Value] = VMReport{
					Name:     vm.Name,
					MOID:     vm.Ref.Value,
					Warnings: []string{"vCenter returned no data for this VM"},
				}
			} else {
				r.Name = vm.Name
				results[vm.Ref.Value] = r
			}
		}
	}
	return results, globalWarnings
}
