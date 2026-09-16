package scripting

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"go.starlark.net/starlark"
)

func csvReport(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var headers, rows *starlark.List
	if err := starlark.UnpackPositionalArgs("csv", args, kwargs, 2, &headers, &rows); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)

	hdrs := make([]string, headers.Len())
	for i := range headers.Len() {
		hdrs[i] = sanitizeCSVCell(starlarkCellToString(headers.Index(i)))
	}
	w.Write(hdrs)

	for i := range rows.Len() {
		row, ok := rows.Index(i).(*starlark.List)
		if !ok {
			return nil, fmt.Errorf("csv: row %d is not a list", i)
		}
		cells := make([]string, row.Len())
		for j := range row.Len() {
			cells[j] = sanitizeCSVCell(starlarkCellToString(row.Index(j)))
		}
		w.Write(cells)
	}
	w.Flush()
	return starlark.String(buf.String()), nil
}

func sanitizeCSVCell(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '@', '\t', '\r':
		return "'" + s
	case '-':
		if _, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err != nil {
			return "'" + s
		}
	}
	return s
}

func jsonReport(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var data starlark.Value
	indent := starlark.MakeInt(2)
	if err := starlark.UnpackArgs("json", args, kwargs, "data", &data, "indent?", &indent); err != nil {
		return nil, err
	}
	i64, _ := indent.Int64()
	goVal := starlarkToGo(data)
	b, err := json.MarshalIndent(goVal, "", strings.Repeat(" ", int(i64)))
	if err != nil {
		return nil, fmt.Errorf("json: %w", err)
	}
	return starlark.String(b), nil
}
