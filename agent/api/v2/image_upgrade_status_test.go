package v2_test

import (
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v2 "github.com/kubev2v/assisted-migration-agent/agent/api/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

var _ = Describe("Image upgrade status API", func() {
	DescribeTable("validates a null host result without conflating upload and read errors", func(uploadError, readError *string) {
		model := models.AgentStatus{ImageUpgradeStatus: models.ImageUpgradeStatus{
			CurrentVersion: "v0.31.0",
			Upload:         models.ImageUploadStatus{State: "idle", LastError: uploadError},
			UpgradeError:   readError,
		}}
		var status v2.AgentStatus
		status.FromModel(model)
		data, err := json.Marshal(status.ImageUpgradeStatus)
		Expect(err).NotTo(HaveOccurred())
		var fields map[string]any
		Expect(json.Unmarshal(data, &fields)).To(Succeed())
		Expect(fields).To(HaveKeyWithValue("upgrade", BeNil()))
		Expect(status.ImageUpgradeStatus.Upload.LastError).To(Equal(uploadError))
		Expect(status.ImageUpgradeStatus.UpgradeError).To(Equal(readError))
		for _, field := range []string{"state", "lastError", "lastResult"} {
			Expect(fields).NotTo(HaveKey(field))
		}
		spec, err := v2.GetSwagger()
		Expect(err).NotTo(HaveOccurred())
		Expect(spec.Components.Schemas["ImageUpgradeStatus"].Value.VisitJSON(fields)).To(Succeed())
	},
		Entry("no host result and no errors", nil, nil),
		Entry("upload failure only", new("upload rejected"), nil),
		Entry("host-result read failure only", nil, new("host image upgrade result is unavailable")),
		Entry("independent upload and read failures", new("upload rejected"), new("host image upgrade result is unavailable")),
	)

	It("maps the host result to outcome and camelCase API fields while retaining host JSON compatibility", func() {
		var result models.ImageUpgradeResult
		Expect(json.Unmarshal([]byte(`{"file":"image.tar","result":"failed","time":"2026-11-03T00:00:00Z","error":"validation failed","error_stage":"validation","running_image":{"version":"v0.31.0","digest":"sha256:old"},"last_successful_update":{"version":"v0.31.0","digest":"sha256:old","time":"2026-11-02T00:00:00Z"}}`), &result)).To(Succeed())
		model := models.AgentStatus{ImageUpgradeStatus: models.ImageUpgradeStatus{
			CurrentVersion: "v0.31.0",
			Upload:         models.ImageUploadStatus{State: "idle"},
			Upgrade:        &result,
		}}
		var status v2.AgentStatus
		status.FromModel(model)
		Expect(status.ImageUpgradeStatus.Upgrade.Outcome).To(Equal(v2.Failed))
		Expect(status.ImageUpgradeStatus.Upgrade.Time).To(Equal(time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)))
		Expect(status.ImageUpgradeStatus.Upgrade.LastSuccessfulUpdate.Version).To(Equal("v0.31.0"))
		data, err := json.Marshal(status.ImageUpgradeStatus)
		Expect(err).NotTo(HaveOccurred())
		var fields map[string]any
		Expect(json.Unmarshal(data, &fields)).To(Succeed())
		upgrade, ok := fields["upgrade"].(map[string]any)
		Expect(ok).To(BeTrue())
		Expect(upgrade).To(HaveKeyWithValue("outcome", "failed"))
		Expect(upgrade).To(HaveKeyWithValue("errorStage", "validation"))
		Expect(upgrade).To(HaveKey("runningImage"))
		Expect(upgrade).To(HaveKey("lastSuccessfulUpdate"))
		for _, field := range []string{"result", "error_stage", "running_image", "last_successful_update"} {
			Expect(upgrade).NotTo(HaveKey(field))
		}
		spec, err := v2.GetSwagger()
		Expect(err).NotTo(HaveOccurred())
		Expect(spec.Components.Schemas["ImageUpgradeStatus"].Value.VisitJSON(fields)).To(Succeed())
		upgrade["outcome"] = "invalid"
		Expect(spec.Components.Schemas["ImageUpgradeStatus"].Value.VisitJSON(fields)).NotTo(Succeed())

		data, err = json.Marshal(result)
		Expect(err).NotTo(HaveOccurred())
		fields = nil
		Expect(json.Unmarshal(data, &fields)).To(Succeed())
		Expect(fields).To(HaveKeyWithValue("result", "failed"))
		Expect(fields).To(HaveKeyWithValue("error_stage", "validation"))
		Expect(fields).To(HaveKey("running_image"))
		Expect(fields).To(HaveKey("last_successful_update"))
		for _, field := range []string{"outcome", "errorStage", "runningImage", "lastSuccessfulUpdate"} {
			Expect(fields).NotTo(HaveKey(field))
		}
	})
})
