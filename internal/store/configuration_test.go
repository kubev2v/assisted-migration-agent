package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/test"
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
		db, err = store.NewDB(nil, ":memory:")
		Expect(err).NotTo(HaveOccurred())

		err = migrations.Run(ctx, db)
		Expect(err).NotTo(HaveOccurred())

		s = store.NewStore(db, test.NewMockValidator())
	})

	AfterEach(func() {
		if db != nil {
			_ = db.Close()
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

	Context("Concurrent writes", func() {
		// Given multiple goroutines writing to the same configuration
		// When all goroutines attempt to save configuration simultaneously
		// Then all writes should succeed and the final value should match the last written value
		It("should handle concurrent writes from multiple goroutines", func() {
			const numGoroutines = 50
			var wg sync.WaitGroup
			errors := make(chan error, numGoroutines)
			// Buffered channel to track write order - last value written is the expected final value
			writes := make(chan models.AgentMode, numGoroutines)

			// Launch multiple goroutines that all write at the same time
			for i := 0; i < numGoroutines; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					// Alternate between connected and disconnected modes
					var mode models.AgentMode
					if idx%2 == 0 {
						mode = models.AgentModeConnected
					} else {
						mode = models.AgentModeDisconnected
					}
					cfg := &models.Configuration{AgentMode: mode}
					if err := s.Configuration().Save(ctx, cfg); err != nil {
						errors <- fmt.Errorf("goroutine %d: %w", idx, err)
						return
					}
					// Record the value that was successfully written
					writes <- mode
				}(i)
			}

			// Wait for all goroutines to complete
			wg.Wait()
			close(errors)
			close(writes)

			// Assert no errors occurred
			var errs []error
			for err := range errors {
				errs = append(errs, err)
			}
			Expect(errs).To(BeEmpty(), "Expected no errors from concurrent writes, got: %v", errs)

			// Get the last written value from the channel
			var expectedMode models.AgentMode
			for mode := range writes {
				expectedMode = mode
			}

			// Verify the final value matches the last write
			retrieved, err := s.Configuration().Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.AgentMode).To(Equal(expectedMode))
		})

		// Given multiple goroutines doing rapid successive writes
		// When each goroutine performs multiple writes in sequence
		// Then the final value should match the last written value
		It("should handle rapid successive writes from multiple goroutines", func() {
			const numGoroutines = 10
			const writesPerGoroutine = 20
			var wg sync.WaitGroup
			errors := make(chan error, numGoroutines*writesPerGoroutine)
			writes := make(chan models.AgentMode, numGoroutines*writesPerGoroutine)

			for i := 0; i < numGoroutines; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					for j := 0; j < writesPerGoroutine; j++ {
						var mode models.AgentMode
						if (idx+j)%2 == 0 {
							mode = models.AgentModeConnected
						} else {
							mode = models.AgentModeDisconnected
						}
						cfg := &models.Configuration{AgentMode: mode}
						if err := s.Configuration().Save(ctx, cfg); err != nil {
							errors <- fmt.Errorf("goroutine %d, write %d: %w", idx, j, err)
							return
						}
						writes <- mode
					}
				}(i)
			}

			wg.Wait()
			close(errors)
			close(writes)

			var errs []error
			for err := range errors {
				errs = append(errs, err)
			}
			Expect(errs).To(BeEmpty(), "Expected no errors from rapid successive writes, got: %v", errs)

			// Get the last written value
			var expectedMode models.AgentMode
			for mode := range writes {
				expectedMode = mode
			}

			// Verify final state matches the last write
			retrieved, err := s.Configuration().Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.AgentMode).To(Equal(expectedMode))
		})
	})
})
