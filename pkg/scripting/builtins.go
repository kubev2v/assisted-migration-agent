package scripting

import (
	"context"
	"fmt"
	"time"

	starlarkre "github.com/magnetde/starlark-re"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

type Builtins struct {
	rt        runtime
	addOutput func(Output)
}

func NewBuiltins(rt runtime, addOutput func(Output)) *Builtins {
	return &Builtins{rt: rt, addOutput: addOutput}
}

func (b *Builtins) Globals(ctx context.Context) starlark.StringDict {
	return starlark.StringDict{
		"collections": starlark.NewBuiltin("collections", b.collections(ctx)),
		"sprintf":     starlark.NewBuiltin("sprintf", builtinSprintf),
		"json":        starlark.NewBuiltin("json", jsonReport),
		"csv":         starlark.NewBuiltin("csv", csvReport),
		"template":    templateModule(),
		"diff":        b.diffModule(),
		"math":        mathModule(),
		"re":          starlarkre.NewModule(),
		"debug":       debugModule(),
	}
}

func builtinSprintf(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	if len(args) == 0 {
		return starlark.None, fmt.Errorf("sprintf: requires at least a format string")
	}
	fmtStr, ok := args[0].(starlark.String)
	if !ok {
		return starlark.None, fmt.Errorf("sprintf: first argument must be a string, got %s", args[0].Type())
	}
	goArgs := make([]any, len(args)-1)
	for i := 1; i < len(args); i++ {
		goArgs[i-1] = starlarkToGo(args[i])
	}
	return starlark.String(fmt.Sprintf(string(fmtStr), goArgs...)), nil
}

// ---------------------------------------------------------------------------
// collections()
// ---------------------------------------------------------------------------

func (b *Builtins) collections(ctx context.Context) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
		infos := b.rt.ListCollections()
		cols := make([]*Collection, len(infos))
		for i, info := range infos {
			cols[i] = b.newCollection(ctx, info)
		}
		return NewCollectionList(cols), nil
	}
}

func (b *Builtins) newCollection(ctx context.Context, info CollectionInfo) *Collection {
	col := &Collection{
		id:        starlark.String(info.ID),
		timestamp: starlark.String(info.CreatedAt.Format(time.RFC3339)),
	}

	collectionID := info.ID

	col.VirtualMachinesFn = func(expression string) (*VirtualMachineList, error) {
		return b.listVirtualMachines(ctx, collectionID, expression)
	}

	col.InventoryFn = func() (*Inventory, error) {
		return b.fetchInventory(ctx, collectionID)
	}

	col.GroupsFn = func() ([]*Group, error) {
		return b.listGroups(ctx, collectionID)
	}

	return col
}

func (b *Builtins) listVirtualMachines(ctx context.Context, collectionID, expression string) (*VirtualMachineList, error) {
	vms, err := b.rt.ListVirtualMachines(ctx, collectionID, expression)
	if err != nil {
		return nil, err
	}

	all := make([]*VirtualMachine, len(vms))
	for i := range vms {
		all[i] = NewVirtualMachine(vms[i])
	}

	return NewVirtualMachineListWithCollection(all, collectionID, &b.rt), nil
}

func (b *Builtins) fetchInventory(ctx context.Context, collectionID string) (*Inventory, error) {
	inv, err := b.rt.GetInventory(ctx, collectionID)
	if err != nil {
		return nil, err
	}
	data, err := jsonBytesToStarlarkDict(inv.Data)
	if err != nil {
		return nil, fmt.Errorf("inventory data: %w", err)
	}
	return NewInventory(inv.UpdatedAt.Format(time.RFC3339), data), nil
}

func (b *Builtins) listGroups(ctx context.Context, collectionID string) ([]*Group, error) {
	groups, err := b.rt.ListGroups(ctx, collectionID)
	if err != nil {
		return nil, err
	}
	result := make([]*Group, len(groups))
	for i, g := range groups {
		sg := NewGroup(g)
		grp := g
		sg.VirtualMachinesFn = func() (*VirtualMachineList, error) {
			return b.listVirtualMachines(ctx, collectionID, grp.Filter)
		}
		result[i] = sg
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Exported module constructors for tests
// ---------------------------------------------------------------------------

func DiffModule() *starlarkstruct.Module {
	return &starlarkstruct.Module{
		Name: "diff",
		Members: starlark.StringDict{
			"vm":             starlark.NewBuiltin("diff.vm", diffVM),
			"vms":            starlark.NewBuiltin("diff.vms", diffVMs),
			"sets":           starlark.NewBuiltin("diff.sets", diffSets),
			"fields":         starlark.NewBuiltin("diff.fields", diffFields),
			"summary":        starlark.NewBuiltin("diff.summary", diffSummary),
			"rank_changes":   starlark.NewBuiltin("diff.rank_changes", diffRankChanges),
			"timeline":       starlark.NewBuiltin("diff.timeline", diffTimeline),
			"where":          starlark.NewBuiltin("diff.where", diffWhere),
			"distribution":   starlark.NewBuiltin("diff.distribution", diffDistribution),
			"resource_delta": starlark.NewBuiltin("diff.resource_delta", diffResourceDelta),
		},
	}
}

// ---------------------------------------------------------------------------
// diff module
// ---------------------------------------------------------------------------

func (b *Builtins) diffModule() *starlarkstruct.Module {
	return &starlarkstruct.Module{
		Name: "diff",
		Members: starlark.StringDict{
			"vm":             starlark.NewBuiltin("diff.vm", diffVM),
			"vms":            starlark.NewBuiltin("diff.vms", diffVMs),
			"sets":           starlark.NewBuiltin("diff.sets", diffSets),
			"fields":         starlark.NewBuiltin("diff.fields", diffFields),
			"summary":        starlark.NewBuiltin("diff.summary", diffSummary),
			"rank_changes":   starlark.NewBuiltin("diff.rank_changes", diffRankChanges),
			"timeline":       starlark.NewBuiltin("diff.timeline", diffTimeline),
			"distribution":   starlark.NewBuiltin("diff.distribution", diffDistribution),
			"resource_delta": starlark.NewBuiltin("diff.resource_delta", diffResourceDelta),
		},
	}
}
