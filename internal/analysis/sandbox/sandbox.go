// Package sandbox runs untrusted commands (linter binaries operating on PR
// source) inside isolated containers. Phase 4 ships a Docker shell-out
// runner; Phase 4.x can swap in Firecracker via firecracker-go-sdk behind
// the same Runner interface.
package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"time"
)

// Spec describes one sandbox invocation.
type Spec struct {
	Image       string
	Cmd         []string
	WorkDir     string            // working dir inside the container
	Mounts      map[string]string // host path → container path; mounted read-only
	Env         map[string]string
	Timeout     time.Duration // 0 == 60s default
	NoNetwork   bool          // adds --network=none
	MemoryLimit string        // e.g. "512m"; empty means no limit
	CPULimit    string        // e.g. "1"; empty means no limit
}

// Result captures the outcome of a sandbox run. A non-zero ExitCode is not
// an error from the runner's perspective — many linters use exit codes to
// signal "findings present". Callers parse stdout regardless.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Duration time.Duration
}

// Runner executes Specs.
type Runner interface {
	Run(ctx context.Context, spec Spec) (*Result, error)
}

// DockerRunner shells out to the docker CLI.
type DockerRunner struct {
	// DockerBin overrides the docker binary; defaults to "docker".
	DockerBin string
}

// Run implements Runner.
func (r *DockerRunner) Run(ctx context.Context, spec Spec) (*Result, error) {
	bin := r.DockerBin
	if bin == "" {
		bin = "docker"
	}
	timeout := spec.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{"run", "--rm"}
	if spec.WorkDir != "" {
		args = append(args, "-w", spec.WorkDir)
	}
	for _, host := range sortedKeys(spec.Mounts) {
		args = append(args, "-v", host+":"+spec.Mounts[host]+":ro")
	}
	for _, k := range sortedKeys(spec.Env) {
		args = append(args, "-e", k+"="+spec.Env[k])
	}
	if spec.NoNetwork {
		args = append(args, "--network=none")
	}
	if spec.MemoryLimit != "" {
		args = append(args, "--memory="+spec.MemoryLimit)
	}
	if spec.CPULimit != "" {
		args = append(args, "--cpus="+spec.CPULimit)
	}
	args = append(args, spec.Image)
	args = append(args, spec.Cmd...)

	cmd := exec.CommandContext(runCtx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	res := &Result{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		Duration: time.Since(start),
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		return res, nil
	}
	if err != nil {
		return res, fmt.Errorf("docker run %s: %w", spec.Image, err)
	}
	return res, nil
}

// MockRunner is a Runner used in tests; it returns canned Results keyed by
// image name. If no entry matches, Run returns an error.
type MockRunner struct {
	Responses map[string]*Result
	Calls     []Spec
}

// Run implements Runner.
func (m *MockRunner) Run(_ context.Context, spec Spec) (*Result, error) {
	m.Calls = append(m.Calls, spec)
	if r, ok := m.Responses[spec.Image]; ok {
		return r, nil
	}
	return nil, fmt.Errorf("mock runner: no canned response for image %q", spec.Image)
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
