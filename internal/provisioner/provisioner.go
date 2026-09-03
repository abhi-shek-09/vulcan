package provisioner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

type Provisioner interface {
	Provision(ctx context.Context, count int) error
	Terminate(ctx context.Context, hostnames []string) error
}

type ProcessProvisioner struct {
	binaryPath      string
	workingDir      string
	natsURL         string
	controlPlaneURL string

	mu        sync.Mutex
	processes map[string]*exec.Cmd
}

func NewProcessProvisioner(binaryPath, workingDir, natsURL, controlPlaneURL string) *ProcessProvisioner {
	return &ProcessProvisioner{
		binaryPath:      binaryPath,
		workingDir:      workingDir,
		natsURL:         natsURL,
		controlPlaneURL: controlPlaneURL,
		processes:       make(map[string]*exec.Cmd),
	}
}

func (p *ProcessProvisioner) Provision(ctx context.Context, count int) error {
	if count <= 0 {
		return nil
	}
	if p.binaryPath == "" {
		return fmt.Errorf("worker binary path is required")
	}

	binary := p.binaryPath
	if !filepath.IsAbs(binary) {
		binary = filepath.Join(p.workingDir, binary)
	}

	for i := 0; i < count; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		hostname := "vulcan-auto-worker-" + strconv.FormatInt(time.Now().UnixNano(), 10) + "-" + strconv.Itoa(i)

		cmd := exec.Command(binary)
		cmd.Dir = p.workingDir
		cmd.Env = append(os.Environ(),
			"WORKER_HOSTNAME="+hostname,
			"NATS_URL="+p.natsURL,
			"CONTROL_PLANE_URL="+p.controlPlaneURL,
		)

		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start worker %d: %w", i+1, err)
		}

		p.mu.Lock()
		p.processes[hostname] = cmd
		p.mu.Unlock()

		go func(hostname string, cmd *exec.Cmd) {
			_ = cmd.Wait()

			p.mu.Lock()
			delete(p.processes, hostname)
			p.mu.Unlock()
		}(hostname, cmd)
	}

	return nil
}

func (p *ProcessProvisioner) Terminate(
	ctx context.Context,
	hostnames []string,
) error {
	if len(hostnames) == 0 {
		return nil
	}

	for _, hostname := range hostnames {
		if err := ctx.Err(); err != nil {
			return err
		}

		p.mu.Lock()
		cmd, ok := p.processes[hostname]
		p.mu.Unlock()

		if !ok {
			// The process may already have exited.
			continue
		}

		if cmd.Process == nil {
			continue
		}

		if err := cmd.Process.Kill(); err != nil {
			return fmt.Errorf(
				"terminate worker %s: %w",
				hostname,
				err,
			)
		}
	}

	return nil
}
