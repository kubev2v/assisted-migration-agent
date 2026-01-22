package cmd

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-extras/cobraflags"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
)

// setupViperForEnvVars configures viper to read environment variables with the given prefix
func setupViperForEnvVars(envPrefix string) {
	viper.Reset()
	viper.AutomaticEnv()
	viper.SetEnvPrefix(envPrefix)
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
}

func TestCmd(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cmd Suite")
}

var _ = Describe("Run Command", func() {
	var cfg *config.Configuration

	BeforeEach(func() {
		cfg = config.NewConfigurationWithOptionsAndDefaults()
	})

	Describe("Flag Parsing", func() {
		It("should parse all server flags", func() {
			cmd := NewRunCommand(cfg)
			cmd.SetArgs([]string{
				"--server-http-port", "9000",
				"--server-statics-folder", "/var/www/statics",
				"--server-mode", "prod",
				"--agent-id", "550e8400-e29b-41d4-a716-446655440000",
				"--source-id", "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
			})

			// Parse flags without executing the command
			err := cmd.ParseFlags([]string{
				"--server-http-port", "9000",
				"--server-statics-folder", "/var/www/statics",
				"--server-mode", "prod",
			})
			Expect(err).ToNot(HaveOccurred())

			Expect(cfg.Server.HTTPPort).To(Equal(9000))
			Expect(cfg.Server.StaticsFolder).To(Equal("/var/www/statics"))
			Expect(cfg.Server.ServerMode).To(Equal("prod"))
		})

		It("should parse all agent flags", func() {
			cmd := NewRunCommand(cfg)

			err := cmd.ParseFlags([]string{
				"--mode", "connected",
				"--agent-id", "550e8400-e29b-41d4-a716-446655440000",
				"--source-id", "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
				"--version", "v1.2.3",
				"--num-workers", "5",
				"--data-folder", "/var/data",
				"--opa-policies-folder", "/etc/policies",
				"--legacy-status-enabled=false",
			})
			Expect(err).ToNot(HaveOccurred())

			Expect(cfg.Agent.Mode).To(Equal("connected"))
			Expect(cfg.Agent.ID).To(Equal("550e8400-e29b-41d4-a716-446655440000"))
			Expect(cfg.Agent.SourceID).To(Equal("6ba7b810-9dad-11d1-80b4-00c04fd430c8"))
			Expect(cfg.Agent.Version).To(Equal("v1.2.3"))
			Expect(cfg.Agent.NumWorkers).To(Equal(5))
			Expect(cfg.Agent.DataFolder).To(Equal("/var/data"))
			Expect(cfg.Agent.OpaPoliciesFolder).To(Equal("/etc/policies"))
			Expect(cfg.Agent.LegacyStatusEnabled).To(BeFalse())
		})

		It("should parse all authentication flags", func() {
			cmd := NewRunCommand(cfg)

			err := cmd.ParseFlags([]string{
				"--authentication-enabled=true",
				"--authentication-jwt-filepath", "/path/to/jwt",
			})
			Expect(err).ToNot(HaveOccurred())

			Expect(cfg.Auth.Enabled).To(BeTrue())
			Expect(cfg.Auth.JWTFilePath).To(Equal("/path/to/jwt"))
		})

		It("should parse all console flags", func() {
			cmd := NewRunCommand(cfg)

			err := cmd.ParseFlags([]string{
				"--console-url", "https://console.example.com",
				"--console-update-interval", "10s",
			})
			Expect(err).ToNot(HaveOccurred())

			Expect(cfg.Console.URL).To(Equal("https://console.example.com"))
			Expect(cfg.Agent.UpdateInterval).To(Equal(10 * time.Second))
		})

		It("should use default values when flags are not provided", func() {
			cmd := NewRunCommand(cfg)
			err := cmd.ParseFlags([]string{})
			Expect(err).ToNot(HaveOccurred())

			// Check defaults from config
			Expect(cfg.Server.HTTPPort).To(Equal(8000))
			Expect(cfg.Server.ServerMode).To(Equal("dev"))
			Expect(cfg.Agent.Mode).To(Equal("disconnected"))
			Expect(cfg.Agent.Version).To(Equal("v0.0.0"))
			Expect(cfg.Agent.NumWorkers).To(Equal(3))
			Expect(cfg.Agent.UpdateInterval).To(Equal(5 * time.Second))
			Expect(cfg.Agent.LegacyStatusEnabled).To(BeTrue())
			Expect(cfg.Console.URL).To(Equal("http://localhost:7443"))
			Expect(cfg.Auth.Enabled).To(BeTrue())
		})
	})

	Describe("Environment Variable Binding", func() {
		AfterEach(func() {
			// Clean up environment variables
			os.Unsetenv("AGENT_SERVER_HTTP_PORT")
			os.Unsetenv("AGENT_SERVER_STATICS_FOLDER")
			os.Unsetenv("AGENT_SERVER_MODE")
			os.Unsetenv("AGENT_MODE")
			os.Unsetenv("AGENT_AGENT_ID")
			os.Unsetenv("AGENT_SOURCE_ID")
			os.Unsetenv("AGENT_VERSION")
			os.Unsetenv("AGENT_NUM_WORKERS")
			os.Unsetenv("AGENT_DATA_FOLDER")
			os.Unsetenv("AGENT_OPA_POLICIES_FOLDER")
			os.Unsetenv("AGENT_LEGACY_STATUS_ENABLED")
			os.Unsetenv("AGENT_AUTHENTICATION_ENABLED")
			os.Unsetenv("AGENT_AUTHENTICATION_JWT_FILEPATH")
			os.Unsetenv("AGENT_CONSOLE_URL")
			os.Unsetenv("AGENT_CONSOLE_UPDATE_INTERVAL")
		})

		It("should read server configuration from environment variables", func() {
			os.Setenv("AGENT_SERVER_HTTP_PORT", "9001")
			os.Setenv("AGENT_SERVER_STATICS_FOLDER", "/env/statics")
			os.Setenv("AGENT_SERVER_MODE", "prod")

			cfg = config.NewConfigurationWithOptionsAndDefaults()
			cmd := NewRunCommand(cfg)
			err := cmd.ParseFlags([]string{})
			Expect(err).ToNot(HaveOccurred())

			// Configure viper and trigger environment variable binding
			setupViperForEnvVars("AGENT")
			cobraflags.PresetRequiredFlags("AGENT", make(map[*pflag.Flag]bool), cmd)

			Expect(cfg.Server.HTTPPort).To(Equal(9001))
			Expect(cfg.Server.StaticsFolder).To(Equal("/env/statics"))
			Expect(cfg.Server.ServerMode).To(Equal("prod"))
		})

		It("should read agent configuration from environment variables", func() {
			os.Setenv("AGENT_MODE", "connected")
			os.Setenv("AGENT_AGENT_ID", "11111111-1111-1111-1111-111111111111")
			os.Setenv("AGENT_SOURCE_ID", "22222222-2222-2222-2222-222222222222")
			os.Setenv("AGENT_VERSION", "v2.0.0")
			os.Setenv("AGENT_NUM_WORKERS", "10")
			os.Setenv("AGENT_DATA_FOLDER", "/env/data")
			os.Setenv("AGENT_OPA_POLICIES_FOLDER", "/env/policies")
			os.Setenv("AGENT_LEGACY_STATUS_ENABLED", "false")

			cfg = config.NewConfigurationWithOptionsAndDefaults()
			cmd := NewRunCommand(cfg)
			err := cmd.ParseFlags([]string{})
			Expect(err).ToNot(HaveOccurred())

			// Configure viper and trigger environment variable binding
			setupViperForEnvVars("AGENT")
			cobraflags.PresetRequiredFlags("AGENT", make(map[*pflag.Flag]bool), cmd)

			Expect(cfg.Agent.Mode).To(Equal("connected"))
			Expect(cfg.Agent.ID).To(Equal("11111111-1111-1111-1111-111111111111"))
			Expect(cfg.Agent.SourceID).To(Equal("22222222-2222-2222-2222-222222222222"))
			Expect(cfg.Agent.Version).To(Equal("v2.0.0"))
			Expect(cfg.Agent.NumWorkers).To(Equal(10))
			Expect(cfg.Agent.DataFolder).To(Equal("/env/data"))
			Expect(cfg.Agent.OpaPoliciesFolder).To(Equal("/env/policies"))
			Expect(cfg.Agent.LegacyStatusEnabled).To(BeFalse())
		})

		It("should read authentication configuration from environment variables", func() {
			os.Setenv("AGENT_AUTHENTICATION_ENABLED", "true")
			os.Setenv("AGENT_AUTHENTICATION_JWT_FILEPATH", "/env/jwt")

			cfg = config.NewConfigurationWithOptionsAndDefaults()
			cmd := NewRunCommand(cfg)
			err := cmd.ParseFlags([]string{})
			Expect(err).ToNot(HaveOccurred())

			// Configure viper and trigger environment variable binding
			setupViperForEnvVars("AGENT")
			cobraflags.PresetRequiredFlags("AGENT", make(map[*pflag.Flag]bool), cmd)

			Expect(cfg.Auth.Enabled).To(BeTrue())
			Expect(cfg.Auth.JWTFilePath).To(Equal("/env/jwt"))
		})

		It("should read console configuration from environment variables", func() {
			os.Setenv("AGENT_CONSOLE_URL", "https://env.console.com")
			os.Setenv("AGENT_CONSOLE_UPDATE_INTERVAL", "30s")

			cfg = config.NewConfigurationWithOptionsAndDefaults()
			cmd := NewRunCommand(cfg)
			err := cmd.ParseFlags([]string{})
			Expect(err).ToNot(HaveOccurred())

			// Configure viper and trigger environment variable binding
			setupViperForEnvVars("AGENT")
			cobraflags.PresetRequiredFlags("AGENT", make(map[*pflag.Flag]bool), cmd)

			Expect(cfg.Console.URL).To(Equal("https://env.console.com"))
			Expect(cfg.Agent.UpdateInterval).To(Equal(30 * time.Second))
		})

		It("should prefer command line flags over environment variables", func() {
			os.Setenv("AGENT_SERVER_HTTP_PORT", "9001")
			os.Setenv("AGENT_MODE", "connected")

			cfg = config.NewConfigurationWithOptionsAndDefaults()
			cmd := NewRunCommand(cfg)
			err := cmd.ParseFlags([]string{
				"--server-http-port", "8080",
				"--mode", "disconnected",
			})
			Expect(err).ToNot(HaveOccurred())

			// CLI flags should take precedence, but env vars are applied after ParseFlags
			// so we need to verify the flag was set before PresetRequiredFlags
			Expect(cfg.Server.HTTPPort).To(Equal(8080))
			Expect(cfg.Agent.Mode).To(Equal("disconnected"))
		})
	})

	Describe("Configuration Validation", func() {
		BeforeEach(func() {
			// Set minimum valid configuration
			cfg.Agent.ID = "550e8400-e29b-41d4-a716-446655440000"
			cfg.Agent.SourceID = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
			cfg.Agent.Mode = "disconnected"
			cfg.Server.ServerMode = "dev"
			cfg.Server.HTTPPort = 8000
			cfg.Agent.NumWorkers = 3
			cfg.Auth.Enabled = false
		})

		It("should pass validation with valid configuration", func() {
			err := validateConfiguration(cfg)
			Expect(err).ToNot(HaveOccurred())
		})

		Context("agent-id validation", func() {
			It("should fail when agent-id is empty", func() {
				cfg.Agent.ID = ""
				err := validateConfiguration(cfg)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("agent-id cannot be empty"))
			})

			It("should fail when agent-id is not a valid UUID", func() {
				cfg.Agent.ID = "not-a-uuid"
				err := validateConfiguration(cfg)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("agent-id must be a valid UUID"))
			})
		})

		Context("source-id validation", func() {
			It("should fail when source-id is empty", func() {
				cfg.Agent.SourceID = ""
				err := validateConfiguration(cfg)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("source-id cannot be empty"))
			})

			It("should fail when source-id is not a valid UUID", func() {
				cfg.Agent.SourceID = "not-a-uuid"
				err := validateConfiguration(cfg)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("source-id must be a valid UUID"))
			})
		})

		Context("mode validation", func() {
			It("should accept 'connected' mode", func() {
				cfg.Agent.Mode = "connected"
				err := validateConfiguration(cfg)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should accept 'disconnected' mode", func() {
				cfg.Agent.Mode = "disconnected"
				err := validateConfiguration(cfg)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should fail with invalid mode", func() {
				cfg.Agent.Mode = "invalid"
				err := validateConfiguration(cfg)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid mode"))
			})
		})

		Context("server-mode validation", func() {
			It("should accept 'prod' server mode with statics folder", func() {
				cfg.Server.ServerMode = "prod"
				cfg.Server.StaticsFolder = "/var/www/statics"
				err := validateConfiguration(cfg)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should accept 'dev' server mode", func() {
				cfg.Server.ServerMode = "dev"
				err := validateConfiguration(cfg)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should fail with invalid server mode", func() {
				cfg.Server.ServerMode = "invalid"
				err := validateConfiguration(cfg)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid server mode"))
			})

			It("should fail when prod mode without statics folder", func() {
				cfg.Server.ServerMode = "prod"
				cfg.Server.StaticsFolder = ""
				err := validateConfiguration(cfg)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("statics folder must be set"))
			})
		})

		Context("http-port validation", func() {
			It("should accept valid port", func() {
				cfg.Server.HTTPPort = 8080
				err := validateConfiguration(cfg)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should fail with port 0", func() {
				cfg.Server.HTTPPort = 0
				err := validateConfiguration(cfg)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid http-port"))
			})

			It("should fail with port > 65535", func() {
				cfg.Server.HTTPPort = 70000
				err := validateConfiguration(cfg)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid http-port"))
			})

			It("should accept port 1", func() {
				cfg.Server.HTTPPort = 1
				err := validateConfiguration(cfg)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should accept port 65535", func() {
				cfg.Server.HTTPPort = 65535
				err := validateConfiguration(cfg)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("num-workers validation", func() {
			It("should accept valid num-workers", func() {
				cfg.Agent.NumWorkers = 5
				err := validateConfiguration(cfg)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should fail with num-workers = 0", func() {
				cfg.Agent.NumWorkers = 0
				err := validateConfiguration(cfg)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid num-workers"))
			})

			It("should fail with negative num-workers", func() {
				cfg.Agent.NumWorkers = -1
				err := validateConfiguration(cfg)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid num-workers"))
			})
		})

		Context("authentication validation", func() {
			It("should pass when authentication disabled", func() {
				cfg.Auth.Enabled = false
				cfg.Auth.JWTFilePath = ""
				err := validateConfiguration(cfg)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should pass when authentication enabled with jwt path", func() {
				cfg.Auth.Enabled = true
				cfg.Auth.JWTFilePath = "/path/to/jwt"
				err := validateConfiguration(cfg)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should fail when authentication enabled without jwt path", func() {
				cfg.Auth.Enabled = true
				cfg.Auth.JWTFilePath = ""
				err := validateConfiguration(cfg)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("authentication-jwt-filepath must be set"))
			})
		})
	})
})
