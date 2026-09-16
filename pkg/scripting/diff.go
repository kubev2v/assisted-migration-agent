package scripting

import (
	"fmt"
	"math"
	"sort"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

func starlarkEqual(x, y starlark.Value) bool {
	ok, err := starlark.CompareDepth(syntax.EQL, x, y, 4)
	if err != nil {
		return false
	}
	return ok
}

var comparableFields = []string{
	"name", "power_state", "cluster", "datacenter",
	"cpu_count", "cores_per_socket", "memory_mb", "disk_size", "storage_used",
	"is_migratable", "is_template", "migration_excluded", "fault_tolerance_enabled",
	"issue_count", "inspection_status", "inspection_concern_count",
	"os_name", "firmware", "folder", "host", "hostname", "ip_address", "connection_state",
	"utilization_cpu_p95", "utilization_mem_p95",
	"utilization_cpu_max", "utilization_mem_max",
	"utilization_disk", "utilization_confidence",
	"labels", "groups",
}

func compareVMs(vmA, vmB *VirtualMachine, fields []string) []*FieldChange {
	var changes []*FieldChange
	for _, field := range fields {
		oldVal, _ := vmA.Attr(field)
		newVal, _ := vmB.Attr(field)
		if oldVal == nil || newVal == nil {
			continue
		}
		if !starlarkEqual(oldVal, newVal) {
			changes = append(changes, NewFieldChange(field, oldVal, newVal))
		}
	}
	return changes
}

func diffVM(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var vmA, vmB *VirtualMachine
	if err := starlark.UnpackPositionalArgs("diff.vm", args, kwargs, 2, &vmA, &vmB); err != nil {
		return nil, err
	}
	changes := compareVMs(vmA, vmB, comparableFields)
	elems := make([]starlark.Value, len(changes))
	for i, c := range changes {
		elems[i] = c
	}
	return starlark.NewList(elems), nil
}

func diffVMs(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var listA, listB *VirtualMachineList
	if err := starlark.UnpackPositionalArgs("diff.vms", args, kwargs, 2, &listA, &listB); err != nil {
		return nil, err
	}

	indexA := make(map[string]*VirtualMachine, len(listA.vms))
	for _, vm := range listA.vms {
		indexA[string(vm.id)] = vm
	}
	indexB := make(map[string]*VirtualMachine, len(listB.vms))
	for _, vm := range listB.vms {
		indexB[string(vm.id)] = vm
	}

	var added, removed []*VirtualMachine
	var changed []*VirtualMachineDiff
	unchanged := 0

	for _, vm := range listB.vms {
		if _, exists := indexA[string(vm.id)]; !exists {
			added = append(added, vm)
		}
	}
	for _, vm := range listA.vms {
		if _, exists := indexB[string(vm.id)]; !exists {
			removed = append(removed, vm)
		}
	}
	for id, vmA := range indexA {
		vmB, exists := indexB[id]
		if !exists {
			continue
		}
		changes := compareVMs(vmA, vmB, comparableFields)
		if len(changes) > 0 {
			changed = append(changed, NewVirtualMachineDiff(id, vmA, vmB, changes))
		} else {
			unchanged++
		}
	}

	return NewCollectionDiff(added, removed, changed, unchanged), nil
}

func diffSets(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var listA, listB *starlark.List
	if err := starlark.UnpackPositionalArgs("diff.sets", args, kwargs, 2, &listA, &listB); err != nil {
		return nil, err
	}

	setA := make(map[string]bool, listA.Len())
	for i := 0; i < listA.Len(); i++ {
		if s, ok := listA.Index(i).(starlark.String); ok {
			setA[string(s)] = true
		}
	}
	setB := make(map[string]bool, listB.Len())
	for i := 0; i < listB.Len(); i++ {
		if s, ok := listB.Index(i).(starlark.String); ok {
			setB[string(s)] = true
		}
	}

	var onlyA, onlyB, common []starlark.Value
	for s := range setA {
		if setB[s] {
			common = append(common, starlark.String(s))
		} else {
			onlyA = append(onlyA, starlark.String(s))
		}
	}
	for s := range setB {
		if !setA[s] {
			onlyB = append(onlyB, starlark.String(s))
		}
	}

	d := starlark.NewDict(3)
	d.SetKey(starlark.String("only_in_a"), starlark.NewList(onlyA))
	d.SetKey(starlark.String("only_in_b"), starlark.NewList(onlyB))
	d.SetKey(starlark.String("common"), starlark.NewList(common))
	return d, nil
}

func diffFields(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var vmA, vmB *VirtualMachine
	var fieldList *starlark.List
	if err := starlark.UnpackPositionalArgs("diff.fields", args, kwargs, 3, &vmA, &vmB, &fieldList); err != nil {
		return nil, err
	}
	fields := make([]string, fieldList.Len())
	for i := 0; i < fieldList.Len(); i++ {
		fields[i] = string(fieldList.Index(i).(starlark.String))
	}
	changes := compareVMs(vmA, vmB, fields)
	elems := make([]starlark.Value, len(changes))
	for i, c := range changes {
		elems[i] = c
	}
	return starlark.NewList(elems), nil
}

func diffSummary(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var listA, listB *VirtualMachineList
	if err := starlark.UnpackPositionalArgs("diff.summary", args, kwargs, 2, &listA, &listB); err != nil {
		return nil, err
	}

	indexA := make(map[string]*VirtualMachine, len(listA.vms))
	for _, vm := range listA.vms {
		indexA[string(vm.id)] = vm
	}
	indexB := make(map[string]*VirtualMachine, len(listB.vms))
	for _, vm := range listB.vms {
		indexB[string(vm.id)] = vm
	}

	addedCount, removedCount, changedCount, unchangedCount := 0, 0, 0, 0
	fieldsChanged := make(map[string]int)

	for _, vm := range listB.vms {
		if _, exists := indexA[string(vm.id)]; !exists {
			addedCount++
		}
	}
	for _, vm := range listA.vms {
		if _, exists := indexB[string(vm.id)]; !exists {
			removedCount++
		}
	}
	for id, vmA := range indexA {
		vmB, exists := indexB[id]
		if !exists {
			continue
		}
		changes := compareVMs(vmA, vmB, comparableFields)
		if len(changes) > 0 {
			changedCount++
			for _, c := range changes {
				fieldsChanged[string(c.field)]++
			}
		} else {
			unchangedCount++
		}
	}

	fcDict := starlark.NewDict(len(fieldsChanged))
	for field, count := range fieldsChanged {
		fcDict.SetKey(starlark.String(field), starlark.MakeInt(count))
	}

	d := starlark.NewDict(6)
	d.SetKey(starlark.String("added"), starlark.MakeInt(addedCount))
	d.SetKey(starlark.String("removed"), starlark.MakeInt(removedCount))
	d.SetKey(starlark.String("changed"), starlark.MakeInt(changedCount))
	d.SetKey(starlark.String("unchanged"), starlark.MakeInt(unchangedCount))
	d.SetKey(starlark.String("total_a"), starlark.MakeInt(len(listA.vms)))
	d.SetKey(starlark.String("total_b"), starlark.MakeInt(len(listB.vms)))
	d.SetKey(starlark.String("fields_changed"), fcDict)
	return d, nil
}

func diffRankChanges(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var listA, listB *VirtualMachineList
	var field starlark.String
	if err := starlark.UnpackPositionalArgs("diff.rank_changes", args, kwargs, 3, &listA, &listB, &field); err != nil {
		return nil, err
	}
	fieldName := string(field)

	indexA := make(map[string]*VirtualMachine, len(listA.vms))
	for _, vm := range listA.vms {
		indexA[string(vm.id)] = vm
	}

	type ranked struct {
		diff  *VirtualMachineDiff
		delta float64
	}
	var results []ranked

	for _, vmB := range listB.vms {
		vmA, exists := indexA[string(vmB.id)]
		if !exists {
			continue
		}
		changes := compareVMs(vmA, vmB, []string{fieldName})
		if len(changes) == 0 {
			continue
		}
		d := NewVirtualMachineDiff(string(vmB.id), vmA, vmB, changes)
		delta := numericDelta(changes[0].oldValue, changes[0].newValue)
		results = append(results, ranked{diff: d, delta: delta})
	}

	sort.Slice(results, func(i, j int) bool {
		return math.Abs(results[i].delta) > math.Abs(results[j].delta)
	})

	elems := make([]starlark.Value, len(results))
	for i, r := range results {
		elems[i] = r.diff
	}
	return starlark.NewList(elems), nil
}

func numericDelta(oldVal, newVal starlark.Value) float64 {
	toFloat := func(v starlark.Value) float64 {
		switch v := v.(type) {
		case starlark.Int:
			i, _ := v.Int64()
			return float64(i)
		case starlark.Float:
			return float64(v)
		default:
			return 0
		}
	}
	return toFloat(newVal) - toFloat(oldVal)
}

func diffWhere(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var listA, listB *VirtualMachineList
	var fn starlark.Callable
	if err := starlark.UnpackPositionalArgs("diff.where", args, kwargs, 3, &listA, &listB, &fn); err != nil {
		return nil, err
	}

	indexA := make(map[string]*VirtualMachine, len(listA.vms))
	for _, vm := range listA.vms {
		indexA[string(vm.id)] = vm
	}

	var results []starlark.Value
	for _, vmB := range listB.vms {
		vmA, exists := indexA[string(vmB.id)]
		if !exists {
			continue
		}
		valA, err := starlark.Call(thread, fn, starlark.Tuple{vmA}, nil)
		if err != nil {
			return nil, fmt.Errorf("diff.where: %w", err)
		}
		valB, err := starlark.Call(thread, fn, starlark.Tuple{vmB}, nil)
		if err != nil {
			return nil, fmt.Errorf("diff.where: %w", err)
		}
		if !starlarkEqual(valA, valB) {
			entry := starlark.NewDict(4)
			entry.SetKey(starlark.String("vm_a"), vmA)
			entry.SetKey(starlark.String("vm_b"), vmB)
			entry.SetKey(starlark.String("old"), valA)
			entry.SetKey(starlark.String("new"), valB)
			results = append(results, entry)
		}
	}
	return starlark.NewList(results), nil
}

func diffDistribution(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var dictA, dictB *starlark.Dict
	var topN starlark.Int
	if err := starlark.UnpackPositionalArgs("diff.distribution", args, kwargs, 3, &dictA, &dictB, &topN); err != nil {
		return nil, err
	}
	n, _ := topN.Int64()

	type entry struct {
		key   string
		total int64
	}
	counts := map[string]int64{}
	for _, item := range dictA.Items() {
		if k, ok := item[0].(starlark.String); ok {
			if v, ok := item[1].(starlark.Int); ok {
				i, _ := v.Int64()
				counts[string(k)] += i
			}
		}
	}
	for _, item := range dictB.Items() {
		if k, ok := item[0].(starlark.String); ok {
			if v, ok := item[1].(starlark.Int); ok {
				i, _ := v.Int64()
				counts[string(k)] += i
			}
		}
	}

	entries := make([]entry, 0, len(counts))
	for k, v := range counts {
		entries = append(entries, entry{k, v})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].total > entries[j].total })

	var labels []string
	needOther := int64(len(entries)) > n
	if needOther {
		for i := 0; i < int(n); i++ {
			labels = append(labels, entries[i].key)
		}
		labels = append(labels, "Other")
	} else {
		for _, e := range entries {
			labels = append(labels, e.key)
		}
	}

	lookup := func(d *starlark.Dict, key string) int64 {
		v, found, _ := d.Get(starlark.String(key))
		if !found {
			return 0
		}
		if iv, ok := v.(starlark.Int); ok {
			i, _ := iv.Int64()
			return i
		}
		return 0
	}

	labelsOut := make([]starlark.Value, len(labels))
	valsA := make([]starlark.Value, len(labels))
	valsB := make([]starlark.Value, len(labels))
	for i, label := range labels {
		labelsOut[i] = starlark.String(label)
		if label == "Other" {
			var otherA, otherB int64
			for j := int(n); j < len(entries); j++ {
				otherA += lookup(dictA, entries[j].key)
				otherB += lookup(dictB, entries[j].key)
			}
			valsA[i] = starlark.MakeInt64(otherA)
			valsB[i] = starlark.MakeInt64(otherB)
		} else {
			valsA[i] = starlark.MakeInt64(lookup(dictA, label))
			valsB[i] = starlark.MakeInt64(lookup(dictB, label))
		}
	}

	result := starlark.NewDict(3)
	result.SetKey(starlark.String("labels"), starlark.NewList(labelsOut))
	result.SetKey(starlark.String("values_a"), starlark.NewList(valsA))
	result.SetKey(starlark.String("values_b"), starlark.NewList(valsB))
	return result, nil
}

func diffResourceDelta(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var listA, listB *VirtualMachineList
	if err := starlark.UnpackPositionalArgs("diff.resource_delta", args, kwargs, 2, &listA, &listB); err != nil {
		return nil, err
	}

	sumField := func(vms []*VirtualMachine, field string) int64 {
		var total int64
		for _, vm := range vms {
			val, _ := vm.Attr(field)
			if v, ok := val.(starlark.Int); ok {
				i, _ := v.Int64()
				total += i
			}
		}
		return total
	}

	cpuA := sumField(listA.vms, "cpu_count")
	cpuB := sumField(listB.vms, "cpu_count")
	memA := sumField(listA.vms, "memory_mb")
	memB := sumField(listB.vms, "memory_mb")
	diskA := sumField(listA.vms, "disk_size")
	diskB := sumField(listB.vms, "disk_size")

	d := starlark.NewDict(3)
	d.SetKey(starlark.String("cpu"), starlark.MakeInt64(cpuB-cpuA))
	d.SetKey(starlark.String("memory_mb"), starlark.MakeInt64(memB-memA))
	d.SetKey(starlark.String("disk_mb"), starlark.MakeInt64(diskB-diskA))
	return d, nil
}

func diffTimeline(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var collections *starlark.List
	var fn starlark.Callable
	if err := starlark.UnpackPositionalArgs("diff.timeline", args, kwargs, 2, &collections, &fn); err != nil {
		return nil, err
	}

	elems := make([]starlark.Value, collections.Len())
	for i := 0; i < collections.Len(); i++ {
		col := collections.Index(i)
		result, err := starlark.Call(thread, fn, starlark.Tuple{col}, nil)
		if err != nil {
			return nil, fmt.Errorf("diff.timeline: callback error at index %d: %w", i, err)
		}
		elems[i] = result
	}
	return starlark.NewList(elems), nil
}
