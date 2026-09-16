package scripting

import (
	"bytes"
	"fmt"
	"text/template"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

func templateModule() *starlarkstruct.Module {
	return &starlarkstruct.Module{
		Name: "template",
		Members: starlark.StringDict{
			"render": starlark.NewBuiltin("template.render", templateRender),
		},
	}
}

func templateRender(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var tmplStr starlark.String
	var data *starlark.Dict
	if err := starlark.UnpackPositionalArgs("template.render", args, kwargs, 2, &tmplStr, &data); err != nil {
		return nil, err
	}
	tmpl, err := template.New("").Parse(string(tmplStr))
	if err != nil {
		return nil, fmt.Errorf("template.render: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, starlarkToGo(data)); err != nil {
		return nil, fmt.Errorf("template.render: %w", err)
	}
	return starlark.String(buf.String()), nil
}
