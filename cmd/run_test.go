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
		// Given a run command with server flags
		// When we parse the flags
		// Then the server configuration should be updated
		It("should parse all server flags", func() {
			// Arrange
			cmd := NewRunCommand(cfg)
			cmd.SetArgs([]string{
				"--server-http-port", "9000",
				"--server-statics-folder", "/var/www/statics",
				"--server-mode", "prod",
				"--agent-id", "550e8400-e29b-41d4-a716-446655440000",
				"--source-id", "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
			})

			// Act
			err := cmd.ParseFlags([]string{
				"--server-http-port", "9000",
				"--server-statics-folder", "/var/www/statics",
				"--server-mode", "prod",
			})

			// Assert
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.Server.HTTPPort).To(Equal(9000))
			Expect(cfg.Server.StaticsFolder).To(Equal("/var/www/statics"))
			Expect(cfg.Server.ServerMode).To(Equal("prod"))
		})

		// Given a run command with agent flags
		// When we parse the flags
		// Then the agent configuration should be updated
		It("should parse all agent flags", func() {
			// Arrange
			cmd := NewRunCommand(cfg)

			// Act
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

			// Assert
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

		// Given a run command with authentication flags
		// When we parse the flags
		// Then the authentication configuration should be updated
		It("should parse all authentication flags", func() {
			// Arrange
			cmd := NewRunCommand(cfg)

			// Act
			err := cmd.ParseFlags([]string{
				"--authentication-enabled=true",
				"--authentication-jwt-filepath", "/path/to/jwt",
			})

			// Assert
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.Auth.Enabled).To(BeTrue())
			Expect(cfg.Auth.JWTFilePath).To(Equal("/path/to/jwt"))
		})

		// Given a run command with console flags
		// When we parse the flags
		// Then the console configuration should be updated
		It("should parse all console flags", func() {
			// Arrange
			cmd := NewRunCommand(cfg)

			// Act
			err := cmd.ParseFlags([]string{
				"--console-url", "https://console.example.com",
				"--console-update-interval", "10s",
			})

			// Assert
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.Console.URL).To(Equal("https://console.example.com"))
			Expect(cfg.Agent.UpdateInterval).To(Equal(10 * time.Second))
		})

		// Given a run command without any flags
		// When we parse the flags
		// Then the default configuration values should be used
		It("should use default values when flags are not provided", func() {
			// Arrange
			cmd := NewRunCommand(cfg)

			// Act
			err := cmd.ParseFlags([]string{})

			// Assert
			Expect(err).ToNot(HaveOccurred())
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

		// Given server environment variables are set
		// When we create and parse the run command
		// Then the server configuration should be read from environment
		It("should read server configuration from environment variables", func() {
			// Arrange
			os.Setenv("AGENT_SERVER_HTTP_PORT", "9001")
			os.Setenv("AGENT_SERVER_STATICS_FOLDER", "/env/statics")
			os.Setenv("AGENT_SERVER_MODE", "prod")

			cfg = config.NewConfigurationWithOptionsAndDefaults()
			cmd := NewRunCommand(cfg)

			// Act
			err := cmd.ParseFlags([]string{})
			Expect(err).ToNot(HaveOccurred())
			setupViperForEnvVars("AGENT")
			cobraflags.PresetRequiredFlags("AGENT", make(map[*pflag.Flag]bool), cmd)

			// Assert
			Expect(cfg.Server.HTTPPort).To(Equal(9001))
			Expect(cfg.Server.StaticsFolder).To(Equal("/env/statics"))
			Expect(cfg.Server.ServerMode).To(Equal("prod"))
		})

		// Given agent environment variables are set
		// When we create and parse the run command
		// Then the agent configuration should be read from environment
		It("should read agent configuration from environment variables", func() {
			// Arrange
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

			// Act
			err := cmd.ParseFlags([]string{})
			Expect(err).ToNot(HaveOccurred())
			setupViperForEnvVars("AGENT")
			cobraflags.PresetRequiredFlags("AGENT", make(map[*pflag.Flag]bool), cmd)

			// Assert
			Expect(cfg.Agent.Mode).To(Equal("connected"))
			Expect(cfg.Agent.ID).To(Equal("11111111-1111-1111-1111-111111111111"))
			Expect(cfg.Agent.SourceID).To(Equal("22222222-2222-2222-2222-222222222222"))
			Expect(cfg.Agent.Version).To(Equal("v2.0.0"))
			Expect(cfg.Agent.NumWorkers).To(Equal(10))
			Expect(cfg.Agent.DataFolder).To(Equal("/env/data"))
			Expect(cfg.Agent.OpaPoliciesFolder).To(Equal("/env/policies"))
			Expect(cfg.Agent.LegacyStatusEnabled).To(BeFalse())
		})

		// Given authentication environment variables are set
		// When we create and parse the run command
		// Then the authentication configuration should be read from environment
		It("should read authentication configuration from environment variables", func() {
			// Arrange
			os.Setenv("AGENT_AUTHENTICATION_ENABLED", "true")
			os.Setenv("AGENT_AUTHENTICATION_JWT_FILEPATH", "/env/jwt")

			cfg = config.NewConfigurationWithOptionsAndDefaults()
			cmd := NewRunCommand(cfg)

			// Act
			err := cmd.ParseFlags([]string{})
			Expect(err).ToNot(HaveOccurred())
			setupViperForEnvVars("AGENT")
			cobraflags.PresetRequiredFlags("AGENT", make(map[*pflag.Flag]bool), cmd)

			// Assert
			Expect(cfg.Auth.Enabled).To(BeTrue())
			Expect(cfg.Auth.JWTFilePath).To(Equal("/env/jwt"))
		})

		// Given console environment variables are set
		// When we create and parse the run command
		// Then the console configuration should be read from environment
		It("should read console configuration from environment variables", func() {
			// Arrange
			os.Setenv("AGENT_CONSOLE_URL", "https://env.console.com")
			os.Setenv("AGENT_CONSOLE_UPDATE_INTERVAL", "30s")

			cfg = config.NewConfigurationWithOptionsAndDefaults()
			cmd := NewRunCommand(cfg)

			// Act
			err := cmd.ParseFlags([]string{})
			Expect(err).ToNot(HaveOccurred())
			setupViperForEnvVars("AGENT")
			cobraflags.PresetRequiredFlags("AGENT", make(map[*pflag.Flag]bool), cmd)

			// Assert
			Expect(cfg.Console.URL).To(Equal("https://env.console.com"))
			Expect(cfg.Agent.UpdateInterval).To(Equal(30 * time.Second))
		})

		// Given both environment variables and CLI flags are set
		// When we parse the flags
		// Then CLI flags should take precedence over environment variables
		It("should prefer command line flags over environment variables", func() {
			// Arrange
			os.Setenv("AGENT_SERVER_HTTP_PORT", "9001")
			os.Setenv("AGENT_MODE", "connected")

			cfg = config.NewConfigurationWithOptionsAndDefaults()
			cmd := NewRunCommand(cfg)

			// Act
			err := cmd.ParseFlags([]string{
				"--server-http-port", "8080",
				"--mode", "disconnected",
			})

			// Assert
			Expect(err).ToNot(HaveOccurred())
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

		// Given a valid configuration
		// When we validate it
		// Then validation should pass
		It("should pass validation with valid configuration", func() {
			// Act
			err := validateConfiguration(cfg)

			// Assert
			Expect(err).ToNot(HaveOccurred())
		})

		Context("agent-id validation", func() {
			// Given an empty agent-id
			// When we validate the configuration
			// Then it should fail with appropriate error
			It("should fail when agent-id is empty", func() {
				// Arrange
				cfg.Agent.ID = ""

				// Act
				err := validateConfiguration(cfg)

				// Assert
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("agent-id cannot be empty"))
			})

			// Given an invalid UUID as agent-id
			// When we validate the configuration
			// Then it should fail with appropriate error
			It("should fail when agent-id is not a valid UUID", func() {
				// Arrange
				cfg.Agent.ID = "not-a-uuid"

				// Act
				err := validateConfiguration(cfg)

				// Assert
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("agent-id must be a valid UUID"))
			})
		})

		Context("source-id validation", func() {
			// Given an empty source-id
			// When we validate the configuration
			// Then it should fail with appropriate error
			It("should fail when source-id is empty", func() {
				// Arrange
				cfg.Agent.SourceID = ""

				// Act
				err := validateConfiguration(cfg)

				// Assert
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("source-id cannot be empty"))
			})

			// Given an invalid UUID as source-id
			// When we validate the configuration
			// Then it should fail with appropriate error
			It("should fail when source-id is not a valid UUID", func() {
				// Arrange
				cfg.Agent.SourceID = "not-a-uuid"

				// Act
				err := validateConfiguration(cfg)

				// Assert
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("source-id must be a valid UUID"))
			})
		})

		Context("mode validation", func() {
			// Given mode set to 'connected'
			// When we validate the configuration
			// Then validation should pass
			It("should accept 'connected' mode", func() {
				// Arrange
				cfg.Agent.Mode = "connected"

				// Act
				err := validateConfiguration(cfg)

				// Assert
				Expect(err).ToNot(HaveOccurred())
			})

			// Given mode set to 'disconnected'
			// When we validate the configuration
			// Then validation should pass
			It("should accept 'disconnected' mode", func() {
				// Arrange
				cfg.Agent.Mode = "disconnected"

				// Act
				err := validateConfiguration(cfg)

				// Assert
				Expect(err).ToNot(HaveOccurred())
			})

			// Given an invalid mode value
			// When we validate the configuration
			// Then it should fail with appropriate error
			It("should fail with invalid mode", func() {
				// Arrange
				cfg.Agent.Mode = "invalid"

				// Act
				err := validateConfiguration(cfg)

				// Assert
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid mode"))
			})
		})

		Context("server-mode validation", func() {
			// Given server mode set to 'prod' with statics folder
			// When we validate the configuration
			// Then validation should pass
			It("should accept 'prod' server mode with statics folder", func() {
				// Arrange
				cfg.Server.ServerMode = "prod"
				cfg.Server.StaticsFolder = "/var/www/statics"

				// Act
				err := validateConfiguration(cfg)

				// Assert
				Expect(err).ToNot(HaveOccurred())
			})

			// Given server mode set to 'dev'
			// When we validate the configuration
			// Then validation should pass
			It("should accept 'dev' server mode", func() {
				// Arrange
				cfg.Server.ServerMode = "dev"

				// Act
				err := validateConfiguration(cfg)

				// Assert
				Expect(err).ToNot(HaveOccurred())
			})

			// Given an invalid server mode value
			// When we validate the configuration
			// Then it should fail with appropriate error
			It("should fail with invalid server mode", func() {
				// Arrange
				cfg.Server.ServerMode = "invalid"

				// Act
				err := validateConfiguration(cfg)

				// Assert
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid server mode"))
			})

			// Given server mode set to 'prod' without statics folder
			// When we validate the configuration
			// Then it should fail with appropriate error
			It("should fail when prod mode without statics folder", func() {
				// Arrange
				cfg.Server.ServerMode = "prod"
				cfg.Server.StaticsFolder = ""

				// Act
				err := validateConfiguration(cfg)

				// Assert
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("statics folder must be set"))
			})
		})

		Context("http-port validation", func() {
			// Given a valid port number
			// When we validate the configuration
			// Then validation should pass
			It("should accept valid port", func() {
				// Arrange
				cfg.Server.HTTPPort = 8080

				// Act
				err := validateConfiguration(cfg)

				// Assert
				Expect(err).ToNot(HaveOccurred())
			})

			// Given port 0
			// When we validate the configuration
			// Then it should fail with appropriate error
			It("should fail with port 0", func() {
				// Arrange
				cfg.Server.HTTPPort = 0

				// Act
				err := validateConfiguration(cfg)

				// Assert
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid http-port"))
			})

			// Given a port greater than 65535
			// When we validate the configuration
			// Then it should fail with appropriate error
			It("should fail with port > 65535", func() {
				// Arrange
				cfg.Server.HTTPPort = 70000

				// Act
				err := validateConfiguration(cfg)

				// Assert
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid http-port"))
			})

			// Given port 1 (minimum valid)
			// When we validate the configuration
			// Then validation should pass
			It("should accept port 1", func() {
				// Arrange
				cfg.Server.HTTPPort = 1

				// Act
				err := validateConfiguration(cfg)

				// Assert
				Expect(err).ToNot(HaveOccurred())
			})

			// Given port 65535 (maximum valid)
			// When we validate the configuration
			// Then validation should pass
			It("should accept port 65535", func() {
				// Arrange
				cfg.Server.HTTPPort = 65535

				// Act
				err := validateConfiguration(cfg)

				// Assert
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("num-workers validation", func() {
			// Given a valid num-workers value
			// When we validate the configuration
			// Then validation should pass
			It("should accept valid num-workers", func() {
				// Arrange
				cfg.Agent.NumWorkers = 5

				// Act
				err := validateConfiguration(cfg)

				// Assert
				Expect(err).ToNot(HaveOccurred())
			})

			// Given num-workers set to 0
			// When we validate the configuration
			// Then it should fail with appropriate error
			It("should fail with num-workers = 0", func() {
				// Arrange
				cfg.Agent.NumWorkers = 0

				// Act
				err := validateConfiguration(cfg)

				// Assert
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid num-workers"))
			})

			// Given a negative num-workers value
			// When we validate the configuration
			// Then it should fail with appropriate error
			It("should fail with negative num-workers", func() {
				// Arrange
				cfg.Agent.NumWorkers = -1

				// Act
				err := validateConfiguration(cfg)

				// Assert
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid num-workers"))
			})
		})

		Context("authentication validation", func() {
			// Given authentication is disabled
			// When we validate the configuration
			// Then validation should pass
			It("should pass when authentication disabled", func() {
				// Arrange
				cfg.Auth.Enabled = false
				cfg.Auth.JWTFilePath = ""

				// Act
				err := validateConfiguration(cfg)

				// Assert
				Expect(err).ToNot(HaveOccurred())
			})

			// Given authentication is enabled with jwt path
			// When we validate the configuration
			// Then validation should pass
			It("should pass when authentication enabled with jwt path", func() {
				// Arrange
				cfg.Auth.Enabled = true
				cfg.Auth.JWTFilePath = "/path/to/jwt"

				// Act
				err := validateConfiguration(cfg)

				// Assert
				Expect(err).ToNot(HaveOccurred())
			})

			// Given authentication is enabled without jwt path
			// When we validate the configuration
			// Then it should fail with appropriate error
			It("should fail when authentication enabled without jwt path", func() {
				// Arrange
				cfg.Auth.Enabled = true
				cfg.Auth.JWTFilePath = ""

				// Act
				err := validateConfiguration(cfg)

				// Assert
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("authentication-jwt-filepath must be set"))
			})
		})
	})
})
