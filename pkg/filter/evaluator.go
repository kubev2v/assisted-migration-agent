package filter

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// ParseExpression parses src into an Expression that can be evaluated with Eval.
func ParseExpression(src []byte) (Expression, error) {
	return parse(src)
}

// Eval evaluates a parsed expression against model.
// Variables in the expression are field names in model.
func Eval(e Expression, model any) (bool, error) {
	v, err := eval(e, model)
	if err != nil {
		return false, err
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("%s expression is not boolean", e.Type())
	}
	return b, nil
}

func eval(e Expression, model any) (any, error) {
	switch e := e.(type) {
	case *binaryExpression:
		return evalBinary(e, model)
	case *varExpression:
		return lookup(model, e.Name)
	case *stringExpression:
		return e.Value, nil
	case *booleanExpression:
		return e.Value, nil
	case *quantityExpression:
		return e.Value * unitFactor(e.Unit), nil
	case *regexExpression:
		return regexp.Compile(e.Pattern)
	case *inExpression:
		left, err := eval(e.Left, model)
		if err != nil {
			return nil, err
		}
		field, ok := left.(string)
		if !ok {
			return nil, fmt.Errorf("%s is %T, not a string", e.Left.String(), left)
		}
		found := false
		for _, s := range e.Values {
			if field == s {
				found = true
				break
			}
		}
		return found != e.Negated, nil
	case *containsExpression:
		left, err := eval(e.Left, model)
		if err != nil {
			return nil, err
		}
		ok, err := evalContains(left, e.Value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Left.String(), err)
		}
		return ok != e.Negated, nil
	}
	return nil, fmt.Errorf("unsupported expression %T", e)
}

func evalBinary(e *binaryExpression, model any) (any, error) {
	switch e.Op {
	case and, or:
		left, err := evalBool(e.Left, model)
		if err != nil {
			return nil, err
		}
		if e.Op == and && !left {
			return false, nil
		}
		if e.Op == or && left {
			return true, nil
		}
		return evalBool(e.Right, model)
	}

	left, err := eval(e.Left, model)
	if err != nil {
		return nil, err
	}
	right, err := eval(e.Right, model)
	if err != nil {
		return nil, err
	}

	switch e.Op {
	case equal:
		cmp, ok := compare(left, right)
		if !ok {
			return false, fmt.Errorf("%s: cannot compare %T and %T", e.String(), left, right)
		}
		return cmp == 0, nil
	case notEqual:
		cmp, ok := compare(left, right)
		if !ok {
			return false, fmt.Errorf("%s: cannot compare %T and %T", e.String(), left, right)
		}
		return cmp != 0, nil
	case greater:
		cmp, ok := compare(left, right)
		if !ok {
			return false, fmt.Errorf("%s: cannot compare %T and %T", e.String(), left, right)
		}
		return cmp > 0, nil
	case gte:
		cmp, ok := compare(left, right)
		if !ok {
			return false, fmt.Errorf("%s: cannot compare %T and %T", e.String(), left, right)
		}
		return cmp >= 0, nil
	case less:
		cmp, ok := compare(left, right)
		if !ok {
			return false, fmt.Errorf("%s: cannot compare %T and %T", e.String(), left, right)
		}
		return cmp < 0, nil
	case lte:
		cmp, ok := compare(left, right)
		if !ok {
			return false, fmt.Errorf("%s: cannot compare %T and %T", e.String(), left, right)
		}
		return cmp <= 0, nil
	case like, notLike:
		re, ok := right.(*regexp.Regexp)
		if !ok {
			return nil, fmt.Errorf("pattern for %s must be a regex", e.Op)
		}
		field, ok := left.(string)
		if !ok {
			return nil, fmt.Errorf("%s is %T, not a string", e.Left.String(), left)
		}
		return re.MatchString(field) != (e.Op == notLike), nil
	}
	return nil, fmt.Errorf("unsupported operator %s", e.Op)
}

func evalBool(e Expression, model any) (bool, error) {
	v, err := eval(e, model)
	if err != nil {
		return false, err
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("%s expression is not boolean", e.Type())
	}
	return b, nil
}

// compare returns -1/0/1 for numbers, strings or bools.
func compare(left, right any) (int, bool) {
	l, r := reflect.ValueOf(left), reflect.ValueOf(right)

	lf, lok := floatOf(l)
	rf, rok := floatOf(r)
	if lok && rok {
		switch {
		case lf < rf:
			return -1, true
		case lf > rf:
			return 1, true
		}
		return 0, true
	}

	// We need to relax the rule here because quantity accept floats
	// Therefore, we need to allow comparing int to floats
	// This is the reason why this condition is here and not at the top.
	if l.Kind() != r.Kind() {
		return 0, false
	}

	switch l.Kind() {
	case reflect.String:
		return strings.Compare(l.String(), r.String()), true
	case reflect.Bool:
		switch {
		case l.Bool() == r.Bool():
			return 0, true
		case !l.Bool():
			return -1, true
		}
		return 1, true
	}
	return 0, false
}

func floatOf(v reflect.Value) (float64, bool) {
	switch {
	case v.CanInt():
		return float64(v.Int()), true
	case v.CanUint():
		return float64(v.Uint()), true
	case v.CanFloat():
		return v.Float(), true
	}
	return 0, false
}

// unitFactor converts a quantity to the MB baseline.
func unitFactor(u QuantityUnit) float64 {
	switch u {
	case KbQuantityUnit:
		return 1.0 / 1024
	case GbQuantityUnit:
		return 1024
	case TbQuantityUnit:
		return 1024 * 1024
	}
	return 1
}

// evalContains reports whether the list field contains value.
// value is parsed according to the field element type (string, bool or numeric).
func evalContains(field any, value string) (bool, error) {
	rv := reflect.ValueOf(field)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return false, fmt.Errorf("field is %T, expected a list", field)
	}

	var (
		want any
		err  error
	)

	switch rv.Type().Elem().Kind() {
	case reflect.String:
		want = value
	case reflect.Bool:
		want, err = strconv.ParseBool(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		want, err = strconv.ParseInt(value, 10, 64)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		want, err = strconv.ParseUint(value, 10, 64)
	case reflect.Float32, reflect.Float64:
		want, err = strconv.ParseFloat(value, 64)
	default:
		return false, fmt.Errorf("field is %T, expected a list of strings, bools or numbers", field)
	}

	if err != nil {
		return false, fmt.Errorf("field is %T, cannot parse %q: %w", field, value, err)
	}

	for i := 0; i < rv.Len(); i++ {
		if cmp, ok := compare(rv.Index(i).Interface(), want); ok && cmp == 0 {
			return true, nil
		}
	}

	return false, nil
}

// lookup resolves a (possibly dotted) field name in model.
// Struct fields match by name, falling back to a case-insensitive match.
func lookup(model any, name string) (any, error) {
	v := reflect.ValueOf(model)
	for _, part := range strings.Split(name, ".") {
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return nil, fmt.Errorf("cannot resolve %q on nil %s", part, v.Type())
			}
			v = v.Elem()
		}
		switch v.Kind() {
		case reflect.Struct:
			f, ok := fieldByName(v, part)
			if !ok {
				return nil, fmt.Errorf("unknown field %q in %s", part, v.Type())
			}
			v = f
		case reflect.Map:
			v = v.MapIndex(reflect.ValueOf(part))
			if !v.IsValid() {
				return nil, fmt.Errorf("unknown key %q", part)
			}
		default:
			return nil, fmt.Errorf("cannot resolve %q on %T", part, model)
		}
	}
	return v.Interface(), nil
}

func fieldByName(v reflect.Value, name string) (reflect.Value, bool) {
	if f, ok := v.Type().FieldByName(name); ok {
		return v.FieldByIndex(f.Index), true
	}
	for i := 0; i < v.NumField(); i++ {
		if strings.EqualFold(v.Type().Field(i).Name, name) {
			return v.Field(i), true
		}
	}
	return reflect.Value{}, false
}
