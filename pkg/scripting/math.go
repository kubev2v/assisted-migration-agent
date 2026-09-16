package scripting

import (
	"fmt"
	gomath "math"
	"sort"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

func mathModule() *starlarkstruct.Module {
	return &starlarkstruct.Module{
		Name: "math",
		Members: starlark.StringDict{
			"ceil":       starlark.NewBuiltin("math.ceil", mathCeil),
			"floor":      starlark.NewBuiltin("math.floor", mathFloor),
			"round":      starlark.NewBuiltin("math.round", mathRound),
			"abs":        starlark.NewBuiltin("math.abs", mathAbs),
			"min":        starlark.NewBuiltin("math.min", mathMin),
			"max":        starlark.NewBuiltin("math.max", mathMax),
			"pow":        starlark.NewBuiltin("math.pow", mathPow),
			"sqrt":       starlark.NewBuiltin("math.sqrt", mathSqrt),
			"sum":        starlark.NewBuiltin("math.sum", mathSum),
			"mean":       starlark.NewBuiltin("math.mean", mathMean),
			"median":     starlark.NewBuiltin("math.median", mathMedian),
			"stddev":     starlark.NewBuiltin("math.stddev", mathStddev),
			"percentile": starlark.NewBuiltin("math.percentile", mathPercentile),
		},
	}
}

func toFloat64(v starlark.Value) (float64, error) {
	switch v := v.(type) {
	case starlark.Int:
		i, _ := v.Int64()
		return float64(i), nil
	case starlark.Float:
		return float64(v), nil
	default:
		return 0, fmt.Errorf("expected number, got %s", v.Type())
	}
}

func isInt(v starlark.Value) bool {
	_, ok := v.(starlark.Int)
	return ok
}

func mathCeil(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x starlark.Value
	if err := starlark.UnpackPositionalArgs("math.ceil", args, kwargs, 1, &x); err != nil {
		return nil, err
	}
	f, err := toFloat64(x)
	if err != nil {
		return nil, fmt.Errorf("math.ceil: %w", err)
	}
	return starlark.MakeInt64(int64(gomath.Ceil(f))), nil
}

func mathFloor(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x starlark.Value
	if err := starlark.UnpackPositionalArgs("math.floor", args, kwargs, 1, &x); err != nil {
		return nil, err
	}
	f, err := toFloat64(x)
	if err != nil {
		return nil, fmt.Errorf("math.floor: %w", err)
	}
	return starlark.MakeInt64(int64(gomath.Floor(f))), nil
}

func mathRound(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x starlark.Value
	if err := starlark.UnpackPositionalArgs("math.round", args, kwargs, 1, &x); err != nil {
		return nil, err
	}
	f, err := toFloat64(x)
	if err != nil {
		return nil, fmt.Errorf("math.round: %w", err)
	}
	return starlark.MakeInt64(int64(gomath.Round(f))), nil
}

func mathAbs(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x starlark.Value
	if err := starlark.UnpackPositionalArgs("math.abs", args, kwargs, 1, &x); err != nil {
		return nil, err
	}
	switch v := x.(type) {
	case starlark.Int:
		i, _ := v.Int64()
		if i < 0 {
			i = -i
		}
		return starlark.MakeInt64(i), nil
	case starlark.Float:
		return starlark.Float(gomath.Abs(float64(v))), nil
	default:
		return nil, fmt.Errorf("math.abs: expected number, got %s", x.Type())
	}
}

func mathMin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var a, b starlark.Value
	if err := starlark.UnpackPositionalArgs("math.min", args, kwargs, 2, &a, &b); err != nil {
		return nil, err
	}
	fa, err := toFloat64(a)
	if err != nil {
		return nil, fmt.Errorf("math.min: first arg: %w", err)
	}
	fb, err := toFloat64(b)
	if err != nil {
		return nil, fmt.Errorf("math.min: second arg: %w", err)
	}
	if fa <= fb {
		if isInt(a) && isInt(b) {
			return a, nil
		}
		return starlark.Float(fa), nil
	}
	if isInt(a) && isInt(b) {
		return b, nil
	}
	return starlark.Float(fb), nil
}

func mathMax(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var a, b starlark.Value
	if err := starlark.UnpackPositionalArgs("math.max", args, kwargs, 2, &a, &b); err != nil {
		return nil, err
	}
	fa, err := toFloat64(a)
	if err != nil {
		return nil, fmt.Errorf("math.max: first arg: %w", err)
	}
	fb, err := toFloat64(b)
	if err != nil {
		return nil, fmt.Errorf("math.max: second arg: %w", err)
	}
	if fa >= fb {
		if isInt(a) && isInt(b) {
			return a, nil
		}
		return starlark.Float(fa), nil
	}
	if isInt(a) && isInt(b) {
		return b, nil
	}
	return starlark.Float(fb), nil
}

func mathPow(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var base, exp starlark.Value
	if err := starlark.UnpackPositionalArgs("math.pow", args, kwargs, 2, &base, &exp); err != nil {
		return nil, err
	}
	fb, err := toFloat64(base)
	if err != nil {
		return nil, fmt.Errorf("math.pow: base: %w", err)
	}
	fe, err := toFloat64(exp)
	if err != nil {
		return nil, fmt.Errorf("math.pow: exp: %w", err)
	}
	return starlark.Float(gomath.Pow(fb, fe)), nil
}

func mathSqrt(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x starlark.Value
	if err := starlark.UnpackPositionalArgs("math.sqrt", args, kwargs, 1, &x); err != nil {
		return nil, err
	}
	f, err := toFloat64(x)
	if err != nil {
		return nil, fmt.Errorf("math.sqrt: %w", err)
	}
	return starlark.Float(gomath.Sqrt(f)), nil
}

func toFloatSlice(list *starlark.List, name string) ([]float64, error) {
	if list.Len() == 0 {
		return nil, fmt.Errorf("%s: empty list", name)
	}
	vals := make([]float64, list.Len())
	for i := range list.Len() {
		f, err := toFloat64(list.Index(i))
		if err != nil {
			return nil, fmt.Errorf("%s: element %d: %w", name, i, err)
		}
		vals[i] = f
	}
	return vals, nil
}

func mathSum(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var list *starlark.List
	if err := starlark.UnpackPositionalArgs("math.sum", args, kwargs, 1, &list); err != nil {
		return nil, err
	}
	if list.Len() == 0 {
		return starlark.Float(0), nil
	}
	vals, err := toFloatSlice(list, "math.sum")
	if err != nil {
		return nil, err
	}
	total := 0.0
	for _, v := range vals {
		total += v
	}
	return starlark.Float(total), nil
}

func mathMean(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var list *starlark.List
	if err := starlark.UnpackPositionalArgs("math.mean", args, kwargs, 1, &list); err != nil {
		return nil, err
	}
	vals, err := toFloatSlice(list, "math.mean")
	if err != nil {
		return nil, err
	}
	total := 0.0
	for _, v := range vals {
		total += v
	}
	return starlark.Float(total / float64(len(vals))), nil
}

func mathMedian(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var list *starlark.List
	if err := starlark.UnpackPositionalArgs("math.median", args, kwargs, 1, &list); err != nil {
		return nil, err
	}
	vals, err := toFloatSlice(list, "math.median")
	if err != nil {
		return nil, err
	}
	sort.Float64s(vals)
	n := len(vals)
	if n%2 == 0 {
		return starlark.Float((vals[n/2-1] + vals[n/2]) / 2), nil
	}
	return starlark.Float(vals[n/2]), nil
}

func mathStddev(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var list *starlark.List
	if err := starlark.UnpackPositionalArgs("math.stddev", args, kwargs, 1, &list); err != nil {
		return nil, err
	}
	vals, err := toFloatSlice(list, "math.stddev")
	if err != nil {
		return nil, err
	}
	mean := 0.0
	for _, v := range vals {
		mean += v
	}
	mean /= float64(len(vals))

	variance := 0.0
	for _, v := range vals {
		d := v - mean
		variance += d * d
	}
	variance /= float64(len(vals))
	return starlark.Float(gomath.Sqrt(variance)), nil
}

func mathPercentile(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var list *starlark.List
	var p starlark.Value
	if err := starlark.UnpackPositionalArgs("math.percentile", args, kwargs, 2, &list, &p); err != nil {
		return nil, err
	}
	pf, err := toFloat64(p)
	if err != nil {
		return nil, fmt.Errorf("math.percentile: p: %w", err)
	}
	if pf < 0 || pf > 100 {
		return nil, fmt.Errorf("math.percentile: p must be 0-100, got %g", pf)
	}
	vals, err := toFloatSlice(list, "math.percentile")
	if err != nil {
		return nil, err
	}
	sort.Float64s(vals)
	n := len(vals)
	rank := pf / 100 * float64(n-1)
	lower := int(rank)
	frac := rank - float64(lower)
	if lower >= n-1 {
		return starlark.Float(vals[n-1]), nil
	}
	return starlark.Float(vals[lower] + frac*(vals[lower+1]-vals[lower])), nil
}
