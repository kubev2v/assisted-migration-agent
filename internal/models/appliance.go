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

// ApplianceVirtualMachine carries the VM attributes appliance rules are
// evaluated against. It holds only the fields rules can match on, so detection
// does not depend on the full VM model.
type ApplianceVirtualMachine struct {
	ID              string
	Name            string
	Annotation      string
	Folder          string
	Host            string
	UUID            string
	Firmware        string
	PowerState      string
	ConnectionState string
	GuestID         string
	NestedHVEnabled bool
	CpuCount        int32
	CoresPerSocket  int32
	MemoryMB        int32
	DiskSize        int64
	NICCount        int32
	Datacenter      string
	Cluster         string
	HostName        string
	IPAddress       string
	Disks           []ApplianceDisk
}

type ApplianceDisk struct {
	File string
	Size int64
}

type ConfidenceLevel int

const (
	NoConfidence ConfidenceLevel = iota
	ConfidenceLowerLevel
	ConfidenceMediumLevel
	ConfidenceHighLevel
)

type OsvVerdict int

const (
	OsvBlocked OsvVerdict = iota
	OsvConditional
	OsvSupported
	OsvUnknown
)

type RuleKind int

const (
	DefaultRule RuleKind = iota
	CustomRule
)

type Evidence struct {
	RuleName          string
	VirtualMachineID  string
	MatchedConditions map[string]int // conditions name is the key, weight the value
}

type DetectionResult struct {
	VirtualMachineID  string
	RuleName          string
	MatchedConditions []string
	Confidence        ConfidenceLevel
}

type DetectionError struct {
	VirtualMachineID string
	RuleName         string
	Err              error
}

type Rule struct {
	ID         int64
	Name       string
	Vendor     string
	RuleKind   RuleKind
	Category   string
	OsvVerdict OsvVerdict
	Confidence ConfidenceLevel
	MinMatches int

	Conditions []any
}

// ConditionInput is a condition as the client described it. It is not
// executable: the payload is keyed by a function name the service has to
// resolve against its registry, and the tier is assigned by the service.
// Services resolve every ConditionInput before handing a Rule to the store;
// rules seeded internally may carry executable conditions directly.
type ConditionInput struct {
	Name       string
	Expression string
	Function   *FunctionInput
}

// FunctionInput names a registered predicate and carries the payload the name
// resolves to. Exactly one payload field is set.
type FunctionInput struct {
	Name         string
	DiskGeometry *DiskGeometry
}

type DiskGeometry struct {
	Disks     []int64
	Tolerance int
	Mode      DiskGeometryMode
}

type DiskGeometryMode string

const (
	DiskGeometryExact    DiskGeometryMode = "exact"
	DiskGeometrySuperset DiskGeometryMode = "superset"
)

// ConditionFuncs maps a persisted function name to its implementation. An
// implementation is either func(T, ApplianceVirtualMachine) (bool, error) or
// func([]T, ApplianceVirtualMachine) (bool, error), depending on how many
// values the condition carries.
type ConditionFuncs map[string]any

// ExpressionCondition is a filter expression evaluated against
// ApplianceVirtualMachine. It is the only non-func condition shape.
type ExpressionCondition struct {
	Name       string
	Tier       int
	Expression string
}

type FuncCondition[T any] struct {
	Data     T
	Name     string
	Tier     int
	FuncName string
	Func     func(value T, vm ApplianceVirtualMachine) (bool, error)
}
