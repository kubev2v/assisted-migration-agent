package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/pkg/scripting"
)

type ScriptingService struct {
	pool *store.Pool
}

func NewScriptingService(pool *store.Pool) *ScriptingService {
	return &ScriptingService{
		pool: pool,
	}
}

func (s *ScriptingService) Execute(ctx context.Context, scriptBase64 string) (scripting.Result, error) {
	data, err := base64.StdEncoding.DecodeString(scriptBase64)
	if err != nil {
		return scripting.Result{}, fmt.Errorf("decoding script: %w", err)
	}
	engine := scripting.NewEngine(s.pool)
	return engine.Execute(ctx, bytes.NewReader(data)), nil
}
