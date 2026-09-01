package config

import "time"

type ServerModeType string

const (
	ServerModeProd ServerModeType = "prod"
	ServerModeDev  ServerModeType = "dev"
)

//go:generate go run github.com/ecordell/optgen -output zz_generated.configuration.go . Configuration Server Agent Console Authentication
type Configuration struct {
	Server  Server         `debugmap:"visible"`
	Agent   Agent          `debugmap:"visible"`
	Auth    Authentication `debugmap:"visible"`
	Console Console        `debugmap:"visible"`

	// Log
	LogFormat string `debugmap:"visible"`
	LogLevel  string `debugmap:"visible"`
}

type Server struct {
	ServerMode    string `debugmap:"visible" default:"dev"`
	HTTPPort      int    `debugmap:"visible" default:"8000"`
	StaticsFolder string `debugmap:"visible"`
	API           string `debugmap:"visible" default:"v2"`
}

type Agent struct {
	Mode                string        `debugmap:"visible" default:"disconnected"`
	ID                  string        `debugmap:"visible"`
	SourceID            string        `debugmap:"visible"`
	Version             string        `debugmap:"visible" default:"v0.0.0"`
	GitCommit           string        `debugmap:"visible" default:"unknown"`
	UIGitCommit         string        `debugmap:"visible" default:"unknown"`
	DataFolder          string        `debugmap:"visible"`
	OpaPoliciesFolder   string        `debugmap:"visible"`
	UpdateInterval      time.Duration `debugmap:"visible" default:"5s"`
	LegacyStatusEnabled bool          `debugmap:"visible" default:"true"`
	RetainCollections   int           `debugmap:"visible" default:"1"`
	RVToolsMode         bool          `debugmap:"visible" default:"false"`

	// Inspection configurations.
	// V2V dry-runs have NO wall-clock timeout by default: their duration cannot be
	// guessed (disk size, SAN load, LUKS), so they are guarded by a vCenter
	// liveness health check instead. V2VHealthCheckInterval controls how often the
	// guard probes the session during a running dry-run. V2VInspectionTimeout is an
	// optional escape hatch: 0 (default) = no deadline; set it only if an operator
	// deliberately wants a hard backstop.
	V2VInspectionTimeout   time.Duration `debugmap:"visible" default:"0s"`
	V2VHealthCheckInterval time.Duration `debugmap:"visible" default:"30s"`
}

type Console struct {
	URL string `debugmap:"visible" default:"http://localhost:7443"`
}

type Authentication struct {
	Enabled     bool   `debugmap:"visible" default:"true"`
	JWTFilePath string `debugmap:"visible"`
}
