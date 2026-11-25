package main

import (
	"archive/tar"
	"bytes"
	"crypto/tls"
	"io"
	"mime/multipart"
	"net/http"
	"path"
	"time"

	"github.com/google/uuid"
	"github.com/onsi/ginkgo/v2"
	gm "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/pkg/e2e/infra"
	"github.com/kubev2v/assisted-migration-agent/test"
	"github.com/kubev2v/assisted-migration-agent/test/e2e-v2/service"
)

var _ = ginkgo.Describe("Image upgrade v2 e2e tests", func() {
	ginkgo.It("publishes a valid upload through POST /image-upgrades", func() {
		manager, ok := infraManager.(*infra.ContainerInfraManager)
		if !ok {
			ginkgo.Skip("requires container filesystem access without the host updater consuming the archive")
		}
		ginkgo.DeferCleanup(manager.RemoveAgent)
		_, err := manager.StartAgent(infra.AgentConfig{
			AgentID:    uuid.NewString(),
			SourceID:   uuid.NewString(),
			Mode:       "disconnected",
			APIVersion: "v2",
			Version:    "v0.31.0",
			ConsoleURL: cfg.AgentProxyUrl,
		})
		gm.Expect(err).NotTo(gm.HaveOccurred())
		agentSvc := service.DefaultAgentSvc(cfg.AgentAPIUrl)
		gm.Eventually(func() error {
			_, err := agentSvc.Status()
			return err
		}, 30*time.Second, time.Second).Should(gm.Succeed())

		// The appliance Containerfile builds linux/amd64 images, even on ARM test hosts.
		archive, _ := test.ImageUpgradeArchive("v0.32.0", "v0.30.0", "amd64")
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		file, err := writer.CreateFormFile("file", "image.tar")
		gm.Expect(err).NotTo(gm.HaveOccurred())
		_, err = file.Write(archive)
		gm.Expect(err).NotTo(gm.HaveOccurred())
		gm.Expect(writer.Close()).To(gm.Succeed())
		request, err := http.NewRequest(http.MethodPost, cfg.AgentAPIUrl+"/api/v2/image-upgrades", &body)
		gm.Expect(err).NotTo(gm.HaveOccurred())
		request.Header.Set("Content-Type", writer.FormDataContentType())
		transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
		defer transport.CloseIdleConnections()
		client := &http.Client{Timeout: 30 * time.Second, Transport: transport}
		response, err := client.Do(request)
		gm.Expect(err).NotTo(gm.HaveOccurred())
		defer func() { _ = response.Body.Close() }()
		responseBody, err := io.ReadAll(response.Body)
		gm.Expect(err).NotTo(gm.HaveOccurred())
		gm.Expect(response.StatusCode).To(gm.Equal(http.StatusAccepted), string(responseBody))
		gm.Expect(responseBody).To(gm.BeEmpty())

		var published bytes.Buffer
		gm.Expect(manager.CopyAgentFiles("/var/lib/agent/updates", &published)).To(gm.Succeed())
		reader := tar.NewReader(&published)
		var archives []string
		for {
			header, err := reader.Next()
			if err == io.EOF {
				break
			}
			gm.Expect(err).NotTo(gm.HaveOccurred())
			if header.Typeflag == tar.TypeDir {
				continue
			}
			gm.Expect(header.Typeflag).To(gm.Equal(byte(tar.TypeReg)))
			gm.Expect(path.Dir(header.Name)).To(gm.Equal("updates"))
			gm.Expect(path.Ext(header.Name)).To(gm.Equal(".tar"))
			data, err := io.ReadAll(reader)
			gm.Expect(err).NotTo(gm.HaveOccurred())
			gm.Expect(data).To(gm.Equal(archive))
			archives = append(archives, header.Name)
		}
		gm.Expect(archives).To(gm.HaveLen(1))
	})
})
