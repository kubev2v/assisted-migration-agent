package store_test

import (
	"context"
	"database/sql"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

var _ = Describe("ConfigurationStore", func() {
	var (
		ctx context.Context
		s   *store.Store
		db  *sql.DB
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		db, err = store.NewDB(":memory:")
		Expect(err).NotTo(HaveOccurred())

		err = migrations.Run(ctx, db)
		Expect(err).NotTo(HaveOccurred())

		s = store.NewStore(db)
	})

	AfterEach(func() {
		if db != nil {
			db.Close()
		}
	})

	Context("Get", func() {
		// Given an empty configuration store
		// When we try to get the configuration
		// Then it should return ConfigurationNotFoundError
		It("should return ConfigurationNotFoundError when no configuration exists", func() {
			// Act
			_, err := s.Configuration().Get(ctx)

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		// Given a saved configuration in the store
		// When we retrieve the configuration
		// Then it should return the saved agent mode
		It("should return saved configuration", func() {
			// Arrange
			cfg := &models.Configuration{AgentMode: models.AgentModeConnected}
			err := s.Configuration().Save(ctx, cfg)
			Expect(err).NotTo(HaveOccurred())

			// Act
			retrieved, err := s.Configuration().Get(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.AgentMode).To(Equal(models.AgentModeConnected))
		})
	})

	Context("Save", func() {
		// Given valid configuration data
		// When we save the configuration
		// Then it should save successfully without error
		It("should save new configuration", func() {
			// Arrange
			cfg := &models.Configuration{AgentMode: models.AgentModeDisconnected}

			// Act
			err := s.Configuration().Save(ctx, cfg)

			// Assert
			Expect(err).NotTo(HaveOccurred())
		})

		// Given existing configuration in the store
		// When we save a new configuration
		// Then it should update the existing record (upsert)
		It("should upsert existing configuration", func() {
			// Arrange
			cfg1 := &models.Configuration{AgentMode: models.AgentModeDisconnected}
			err := s.Configuration().Save(ctx, cfg1)
			Expect(err).NotTo(HaveOccurred())

			// Act
			cfg2 := &models.Configuration{AgentMode: models.AgentModeConnected}
			err = s.Configuration().Save(ctx, cfg2)
			Expect(err).NotTo(HaveOccurred())

			// Assert
			retrieved, err := s.Configuration().Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.AgentMode).To(Equal(models.AgentModeConnected))
		})

		// Given different agent mode values
		// When we save each mode
		// Then it should store and retrieve the correct mode
		It("should save different agent mode values", func() {
			// Arrange & Act - save connected
			cfg := &models.Configuration{AgentMode: models.AgentModeConnected}
			err := s.Configuration().Save(ctx, cfg)
			Expect(err).NotTo(HaveOccurred())

			retrieved, err := s.Configuration().Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.AgentMode).To(Equal(models.AgentModeConnected))

			// Arrange & Act - save disconnected
			cfg = &models.Configuration{AgentMode: models.AgentModeDisconnected}
			err = s.Configuration().Save(ctx, cfg)
			Expect(err).NotTo(HaveOccurred())

			retrieved, err = s.Configuration().Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.AgentMode).To(Equal(models.AgentModeDisconnected))
		})
	})
})
