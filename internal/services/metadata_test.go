package services

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

var _ = Describe("Metadata category names", func() {
	It("retains successful names when another category cannot be resolved", func() {
		calls := map[string]int{}
		names := resolveTagCategories(context.Background(), []string{"cat1", "cat2", "cat3", "cat4"}, func(_ context.Context, id string) (string, error) {
			calls[id]++
			switch id {
			case "cat1":
				return "Owner's; \"name\"", nil
			case "cat2":
				return "", errors.New("forbidden")
			case "cat4":
				return "  ", nil
			default:
				return "", nil
			}
		})
		Expect(names).To(Equal(map[string]string{"cat1": "Owner's; \"name\""}))
		Expect(calls).To(Equal(map[string]int{"cat1": 1, "cat2": 1, "cat3": 1, "cat4": 1}))
		Expect(tagCategoryNames(context.Background(), nil, models.Credentials{})).To(BeNil())
	})

	It("stops lookups on cancellation and retains names already resolved", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		calls := 0
		names := resolveTagCategories(ctx, []string{"cat1", "cat2"}, func(context.Context, string) (string, error) {
			calls++
			cancel()
			return "Owner", nil
		})
		Expect(names).To(Equal(map[string]string{"cat1": "Owner"}))
		Expect(calls).To(Equal(1))
	})
})
