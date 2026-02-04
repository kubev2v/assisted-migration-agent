package main

import (
	"context"
	"fmt"
	"time"

	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/containers/podman/v5/pkg/bindings/network"
	"github.com/containers/podman/v5/pkg/bindings/volumes"
	"github.com/containers/podman/v5/pkg/specgen"
	nettypes "go.podman.io/common/libnetwork/types"
)

const NetworkName = "planner"

type ContainerConfig struct {
	name    string
	image   string
	cmd     []string
	ports   map[int]int
	envVars map[string]string
	volumes map[string]string
}

// NewContainerConfig creates a new ContainerConfig with mandatory name and image.
func NewContainerConfig(name, image string) *ContainerConfig {
	return &ContainerConfig{
		name:    name,
		image:   image,
		ports:   make(map[int]int),
		envVars: make(map[string]string),
		volumes: make(map[string]string),
	}
}

// WithPort adds a port mapping (hostPort -> containerPort).
func (c *ContainerConfig) WithPort(hostPort, containerPort int) *ContainerConfig {
	c.ports[hostPort] = containerPort
	return c
}

// WithEnvVar adds a single environment variable.
func (c *ContainerConfig) WithEnvVar(key, value string) *ContainerConfig {
	c.envVars[key] = value
	return c
}

// WithEnvVars adds multiple environment variables.
func (c *ContainerConfig) WithEnvVars(envVars map[string]string) *ContainerConfig {
	for k, v := range envVars {
		c.envVars[k] = v
	}
	return c
}

// WithVolume adds a named volume mapping (volumeName -> containerPath).
func (c *ContainerConfig) WithVolume(volumeName, containerPath string) *ContainerConfig {
	c.volumes[volumeName] = containerPath
	return c
}

// WithCmd sets the command to run in the container.
func (c *ContainerConfig) WithCmd(cmd ...string) *ContainerConfig {
	c.cmd = cmd
	return c
}

type PodmanRunner struct {
	conn context.Context
}

func NewPodmanRunner(socket string) (*PodmanRunner, error) {
	conn, err := bindings.NewConnection(context.Background(), socket)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to podman: %w", err)
	}
	return &PodmanRunner{conn: conn}, nil
}

func (p *PodmanRunner) StartContainer(cfg *ContainerConfig) (string, error) {
	s := specgen.NewSpecGenerator(cfg.image, false)
	s.Name = cfg.name
	s.Command = cfg.cmd
	s.Env = cfg.envVars
	s.NetNS = specgen.Namespace{NSMode: specgen.Host}

	if len(cfg.ports) > 0 {
		s.PortMappings = make([]nettypes.PortMapping, 0, len(cfg.ports))
		for hostPort, containerPort := range cfg.ports {
			s.PortMappings = append(s.PortMappings, nettypes.PortMapping{
				HostPort:      uint16(hostPort),
				ContainerPort: uint16(containerPort),
				Protocol:      "tcp",
			})
		}
	}

	if len(cfg.volumes) > 0 {
		s.Volumes = make([]*specgen.NamedVolume, 0, len(cfg.volumes))
		for volumeName, containerPath := range cfg.volumes {
			s.Volumes = append(s.Volumes, &specgen.NamedVolume{
				Name: volumeName,
				Dest: containerPath,
			})
		}
	}

	createResponse, err := containers.CreateWithSpec(p.conn, s, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	if err := containers.Start(p.conn, createResponse.ID, nil); err != nil {
		return "", fmt.Errorf("failed to start container: %w", err)
	}

	return createResponse.ID, nil
}

func (p *PodmanRunner) WaitContainer(id string) (int32, error) {
	exitCode, err := containers.Wait(p.conn, id, nil)
	if err != nil {
		return -1, fmt.Errorf("failed to wait for container: %w", err)
	}
	return exitCode, nil
}

func (p *PodmanRunner) StopContainer(id string) error {
	if err := containers.Stop(p.conn, id, nil); err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}
	return nil
}

func (p *PodmanRunner) RestartContainer(id string) error {
	if err := containers.Stop(p.conn, id, nil); err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}
	if err := containers.Start(p.conn, id, nil); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}
	return nil
}

func (p *PodmanRunner) RemoveContainer(id string) error {
	_, err := containers.Remove(p.conn, id, nil)
	if err != nil {
		return fmt.Errorf("failed to remove container: %w", err)
	}
	return nil
}

func (p *PodmanRunner) IsRunning(id string) (bool, error) {
	data, err := containers.Inspect(p.conn, id, nil)
	if err != nil {
		return false, fmt.Errorf("failed to inspect container: %w", err)
	}
	return data.State.Running, nil
}

func (p *PodmanRunner) WaitForRunning(id string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		running, err := p.IsRunning(id)
		if err != nil {
			return err
		}
		if running {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("container %s did not start within %v", id, timeout)
}

func (p *PodmanRunner) Logs(id string) (string, error) {
	var stdout, stderr []string
	stdoutChan := make(chan string)
	stderrChan := make(chan string)

	go func() {
		for line := range stdoutChan {
			stdout = append(stdout, line)
		}
	}()
	go func() {
		for line := range stderrChan {
			stderr = append(stderr, line)
		}
	}()

	opts := &containers.LogOptions{}
	if err := containers.Logs(p.conn, id, opts, stdoutChan, stderrChan); err != nil {
		return "", fmt.Errorf("failed to get logs: %w", err)
	}
	return fmt.Sprintf("stdout: %v\nstderr: %v", stdout, stderr), nil
}

func (p *PodmanRunner) CreateNetwork() error {
	exists, err := network.Exists(p.conn, NetworkName, nil)
	if err != nil {
		return fmt.Errorf("failed to check network: %w", err)
	}
	if exists {
		return nil
	}
	_, err = network.Create(p.conn, &nettypes.Network{Name: NetworkName})
	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}
	return nil
}

func (p *PodmanRunner) RemoveVolume(name string) error {
	if err := volumes.Remove(p.conn, name, nil); err != nil {
		return fmt.Errorf("failed to remove volume: %w", err)
	}
	return nil
}
