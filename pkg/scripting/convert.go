package scripting

import (
	"encoding/json"
	"fmt"

	"go.starlark.net/starlark"
)

func starlarkToGo(v starlark.Value) any {
	switch v := v.(type) {
	case starlark.String:
		return string(v)
	case starlark.Int:
		i, _ := v.Int64()
		return i
	case starlark.Float:
		return float64(v)
	case starlark.Bool:
		return bool(v)
	case starlark.NoneType:
		return nil
	case *starlark.List:
		result := make([]any, v.Len())
		for i := 0; i < v.Len(); i++ {
			result[i] = starlarkToGo(v.Index(i))
		}
		return result
	case *starlark.Dict:
		result := make(map[string]any)
		for _, item := range v.Items() {
			if key, ok := item[0].(starlark.String); ok {
				result[string(key)] = starlarkToGo(item[1])
			}
		}
		return result
	default:
		return v.String()
	}
}

func goToStarlark(v any) starlark.Value {
	switch v := v.(type) {
	case nil:
		return starlark.None
	case bool:
		return starlark.Bool(v)
	case float64:
		if v == float64(int64(v)) {
			return starlark.MakeInt64(int64(v))
		}
		return starlark.Float(v)
	case string:
		return starlark.String(v)
	case []any:
		elems := make([]starlark.Value, len(v))
		for i, e := range v {
			elems[i] = goToStarlark(e)
		}
		return starlark.NewList(elems)
	case map[string]any:
		d := starlark.NewDict(len(v))
		for k, val := range v {
			d.SetKey(starlark.String(k), goToStarlark(val))
		}
		return d
	default:
		return starlark.String(fmt.Sprintf("%v", v))
	}
}

func jsonBytesToStarlarkDict(b []byte) (*starlark.Dict, error) {
	var raw any
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	val := goToStarlark(raw)
	if d, ok := val.(*starlark.Dict); ok {
		return d, nil
	}
	d := starlark.NewDict(1)
	d.SetKey(starlark.String("data"), val)
	return d, nil
}

func starlarkCellToString(v starlark.Value) string {
	if s, ok := v.(starlark.String); ok {
		return string(s)
	}
	return v.String()
}
