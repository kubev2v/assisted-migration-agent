package scripting

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

type CollectionInfo struct {
	ID        string
	CreatedAt time.Time
}

type OutputType string

const (
	OutputStdout OutputType = "stdout"
	OutputFile   OutputType = "file"
)

type Output struct {
	Type OutputType
	Name string
	Data []byte
}

type Result struct {
	Err     error
	Stdout  string
	Outputs []Output
}

type Engine struct {
	rt runtime
}

func NewEngine(pool *store.Pool) *Engine {
	return &Engine{rt: runtime{pool: pool}}
}

func (e *Engine) Execute(ctx context.Context, script io.Reader) Result {
	src, err := io.ReadAll(script)
	if err != nil {
		return Result{Err: fmt.Errorf("reading script: %w", err)}
	}

	var outputs []Output
	addOutput := func(o Output) {
		outputs = append(outputs, o)
	}

	builtins := NewBuiltins(e.rt, addOutput)

	stdoutWriter := strings.Builder{}

	thread := &starlark.Thread{
		Name: "script",
		Print: func(_ *starlark.Thread, msg string) {
			fmt.Fprintln(&stdoutWriter, msg)
		},
	}

	opts := &syntax.FileOptions{
		TopLevelControl: true,
		GlobalReassign:  true,
	}
	_, err = starlark.ExecFileOptions(opts, thread, "script.star", src, builtins.Globals(ctx))
	return Result{Err: err, Outputs: outputs, Stdout: stdoutWriter.String()}
}
