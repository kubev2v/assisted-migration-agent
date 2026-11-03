// Package v2 defines the Agent v2 API and converts internal models into its API types.
// Agent v2 and Analyzer v1 keep their API types and conversions in separate packages
// so either API can change its response format independently.
package v2

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.3.0 --config=types.gen.cfg openapi.yaml
//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.3.0  --config=spec.gen.cfg openapi.yaml
