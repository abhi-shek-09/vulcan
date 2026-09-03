package provisioner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProcessProvisionerProvisionsMissingWorkers(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "worker-started")
	script := filepath.Join(dir, "worker.sh")

	scriptContents := "#!/bin/sh\n" +
		"echo \"$WORKER_HOSTNAME\" >> \"" + marker + "\"\n" +
		"sleep 1\n"

	if err := os.WriteFile(script, []byte(scriptContents), 0755); err != nil {
		t.Fatal(err)
	}

	p := NewProcessProvisioner(script, dir, "nats://localhost:4222", "http://localhost:8080")

	if err := p.Provision(context.Background(), 2); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(marker)
		if err == nil && strings.Count(string(data), "\n") == 2 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}

	data, _ := os.ReadFile(marker)
	t.Fatalf("provisioned %d workers, want 2", strings.Count(string(data), "\n"))
}

func TestProcessProvisionerZeroWorkers(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "worker.sh")

	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	p := NewProcessProvisioner(script, dir, "nats://localhost:4222", "http://localhost:8080")
	if err := p.Provision(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
}

func TestProcessProvisionerTerminatesWorker(t *testing.T) {
	dir := t.TempDir()

	script := filepath.Join(dir, "worker.sh")

	if err := os.WriteFile(
		script,
		[]byte("#!/bin/sh\nsleep 30\n"),
		0755,
	); err != nil {
		t.Fatal(err)
	}

	p := NewProcessProvisioner(
		script,
		dir,
		"nats://localhost:4222",
		"http://localhost:8080",
	)

	if err := p.Provision(context.Background(), 1); err != nil {
		t.Fatal(err)
	}

	var hostname string

	p.mu.Lock()
	for h := range p.processes {
		hostname = h
		break
	}
	p.mu.Unlock()

	if hostname == "" {
		t.Fatal("expected provisioned worker process")
	}

	if err := p.Terminate(
		context.Background(),
		[]string{hostname},
	); err != nil {
		t.Fatalf("terminate failed: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)

	for time.Now().Before(deadline) {
		p.mu.Lock()
		_, exists := p.processes[hostname]
		p.mu.Unlock()

		if !exists {
			return
		}

		time.Sleep(25 * time.Millisecond)
	}

	t.Fatalf("worker process %q was not terminated", hostname)
}
