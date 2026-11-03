// Package v1 defines the Analyzer v1 API and converts internal models into its API types.
// Analyzer v1 and Agent v2 keep their API types and conversions in separate packages
// so either API can change its response format independently.
package v1

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.3.0 --config=types.gen.cfg openapi.yaml
//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.3.0  --config=spec.gen.cfg openapi.yaml
