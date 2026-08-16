package provisioner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

type Provisioner interface {
	Provision(ctx context.Context, count int) error
}

type ProcessProvisioner struct {
	binaryPath      string
	workingDir      string
	natsURL         string
	controlPlaneURL string
}

func NewProcessProvisioner(binaryPath, workingDir, natsURL, controlPlaneURL string) *ProcessProvisioner {
	return &ProcessProvisioner{
		binaryPath:      binaryPath,
		workingDir:      workingDir,
		natsURL:         natsURL,
		controlPlaneURL: controlPlaneURL,
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
	}

	return nil
}
