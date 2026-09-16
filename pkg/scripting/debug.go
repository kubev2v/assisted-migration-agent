package scripting

import (
	"fmt"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

func debugModule() *starlarkstruct.Module {
	return &starlarkstruct.Module{
		Name: "debug",
		Members: starlark.StringDict{
			"dump":     starlark.NewBuiltin("debug.dump", debugDump),
			"describe": starlark.NewBuiltin("debug.describe", debugDescribe),
			"peek":     starlark.NewBuiltin("debug.peek", debugPeek),
		},
	}
}

func debugDump(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var val starlark.Value
	if err := starlark.UnpackPositionalArgs("debug.dump", args, kwargs, 1, &val); err != nil {
		return nil, err
	}
	out := dumpValue(val, 0)
	if thread.Print != nil {
		thread.Print(thread, out)
	}
	return starlark.String(out), nil
}

func debugDescribe(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var val starlark.Value
	if err := starlark.UnpackPositionalArgs("debug.describe", args, kwargs, 1, &val); err != nil {
		return nil, err
	}
	var b strings.Builder
	fmt.Fprint(&b, val.Type())
	if ha, ok := val.(starlark.HasAttrs); ok {
		fmt.Fprintf(&b, " — attributes:\n  %s", strings.Join(ha.AttrNames(), ", "))
	}
	return starlark.String(b.String()), nil
}

func debugPeek(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var vms *VirtualMachineList
	var fields *starlark.List
	limit := starlark.MakeInt(20)
	if err := starlark.UnpackArgs("debug.peek", args, kwargs, "vms", &vms, "fields", &fields, "limit?", &limit); err != nil {
		return nil, err
	}
	lim, _ := limit.Int64()

	fieldNames := make([]string, fields.Len())
	for i := range fields.Len() {
		s, ok := fields.Index(i).(starlark.String)
		if !ok {
			return nil, fmt.Errorf("debug.peek: field name must be string, got %s", fields.Index(i).Type())
		}
		fieldNames[i] = string(s)
	}

	widths := make([]int, len(fieldNames))
	for i, name := range fieldNames {
		widths[i] = len(name)
	}

	rows := make([][]string, 0)
	count := len(vms.vms)
	showCount := count
	if int64(showCount) > lim {
		showCount = int(lim)
	}

	for i := range showCount {
		vm := vms.vms[i]
		row := make([]string, len(fieldNames))
		for j, name := range fieldNames {
			v, err := vm.Attr(name)
			if err != nil {
				row[j] = "?"
			} else {
				row[j] = formatSimple(v)
			}
			if len(row[j]) > widths[j] {
				widths[j] = len(row[j])
			}
		}
		rows = append(rows, row)
	}

	var b strings.Builder
	for i, name := range fieldNames {
		if i > 0 {
			fmt.Fprint(&b, "  ")
		}
		fmt.Fprint(&b, padRight(name, widths[i]))
	}
	fmt.Fprint(&b, "\n")

	for _, row := range rows {
		for i, cell := range row {
			if i > 0 {
				fmt.Fprint(&b, "  ")
			}
			fmt.Fprint(&b, padRight(cell, widths[i]))
		}
		fmt.Fprint(&b, "\n")
	}

	if count > showCount {
		fmt.Fprintf(&b, "... (%d more, %d total)\n", count-showCount, count)
	}

	out := b.String()
	if thread.Print != nil {
		thread.Print(thread, out)
	}
	return starlark.String(out), nil
}

func dumpValue(val starlark.Value, indent int) string {
	prefix := strings.Repeat("  ", indent)
	childPrefix := strings.Repeat("  ", indent+1)

	switch v := val.(type) {
	case *VirtualMachineList:
		var b strings.Builder
		fmt.Fprintf(&b, "%sVirtualMachineList (%d items):\n", prefix, len(v.vms))
		showFirst := 5
		showLast := 5
		if len(v.vms) <= showFirst+showLast {
			for i, vm := range v.vms {
				fmt.Fprintf(&b, "%s[%d] %s\n", childPrefix, i, vmOneLiner(vm))
			}
		} else {
			for i := range showFirst {
				fmt.Fprintf(&b, "%s[%d] %s\n", childPrefix, i, vmOneLiner(v.vms[i]))
			}
			fmt.Fprintf(&b, "%s... (%d more)\n", childPrefix, len(v.vms)-showFirst-showLast)
			for i := len(v.vms) - showLast; i < len(v.vms); i++ {
				fmt.Fprintf(&b, "%s[%d] %s\n", childPrefix, i, vmOneLiner(v.vms[i]))
			}
		}
		return strings.TrimRight(b.String(), "\n")

	case *CollectionList:
		var b strings.Builder
		fmt.Fprintf(&b, "%sCollectionList (%d items):\n", prefix, len(v.cols))
		for i, c := range v.cols {
			fmt.Fprintf(&b, "%s[%d] %s  %s\n", childPrefix, i, c.id, c.timestamp)
		}
		return strings.TrimRight(b.String(), "\n")

	case *starlark.Dict:
		var b strings.Builder
		fmt.Fprintf(&b, "%sdict:\n", prefix)
		for _, item := range v.Items() {
			key := formatSimple(item[0])
			value := formatSimple(item[1])
			fmt.Fprintf(&b, "%s%-20s %s\n", childPrefix, key+":", value)
		}
		return strings.TrimRight(b.String(), "\n")

	case *starlark.List:
		var b strings.Builder
		fmt.Fprintf(&b, "%slist (%d items):\n", prefix, v.Len())
		show := min(v.Len(), 10)
		for i := range show {
			fmt.Fprintf(&b, "%s[%d] %s\n", childPrefix, i, formatSimple(v.Index(i)))
		}
		if v.Len() > show {
			fmt.Fprintf(&b, "%s... (%d more)\n", childPrefix, v.Len()-show)
		}
		return strings.TrimRight(b.String(), "\n")

	default:
		if ha, ok := val.(starlark.HasAttrs); ok {
			var b strings.Builder
			fmt.Fprintf(&b, "%s%s:\n", prefix, val.Type())
			for _, name := range ha.AttrNames() {
				attrVal, err := ha.Attr(name)
				if err != nil {
					continue
				}
				if _, isBuiltin := attrVal.(*starlark.Builtin); isBuiltin {
					continue
				}
				fmt.Fprintf(&b, "%s%-28s %s\n", childPrefix, name+":", formatSimple(attrVal))
			}
			return strings.TrimRight(b.String(), "\n")
		}
		return prefix + formatSimple(val)
	}
}

func vmOneLiner(vm *VirtualMachine) string {
	cpu, _ := vm.cpuCount.Int64()
	mem, _ := vm.memoryMB.Int64()
	return fmt.Sprintf("%s  %s  %d vCPU  %d MB", vm.name, vm.cluster, cpu, mem)
}

func formatSimple(v starlark.Value) string {
	switch v := v.(type) {
	case starlark.String:
		return fmt.Sprintf("%q", string(v))
	case starlark.Int:
		return v.String()
	case starlark.Float:
		return fmt.Sprintf("%g", float64(v))
	case starlark.Bool:
		if v {
			return "True"
		}
		return "False"
	case starlark.NoneType:
		return "None"
	case *starlark.List:
		if v.Len() == 0 {
			return "[]"
		}
		elems := make([]string, v.Len())
		for i := range v.Len() {
			elems[i] = formatSimple(v.Index(i))
		}
		return "[" + strings.Join(elems, ", ") + "]"
	default:
		return v.String()
	}
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
