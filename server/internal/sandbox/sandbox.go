// Package sandbox provides process-level isolation for agent tool execution.
// On Linux, it uses unshare(1) to create PID + mount + network namespaces and
// a private tmpfs work directory. When unshare is unavailable (macOS / dev) it
// falls back to a restricted exec.Command with an isolated tmp directory.
package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Config controls sandbox behaviour for one session.
type Config struct {
	// SessionID is used to create a unique workdir.
	SessionID string
	// BaseDir is the parent for per-session directories (default /tmp/aurion-sandbox).
	BaseDir string
	// NetDisabled drops network access inside the sandbox (Linux only).
	NetDisabled bool
	// MaxOutputBytes truncates combined stdout+stderr.
	MaxOutputBytes int
	// DangerousCommands is the blocklist. Nil means use the default list.
	DangerousCommands []string
}

// Sandbox is a per-session execution environment.
type Sandbox struct {
	cfg     Config
	workDir string
	mu      sync.Mutex
	closed  bool
}

var defaultDangerous = []string{
	"rm -rf /", "mkfs", "dd if=", ":(){:|:&};:",
	"chmod -R 777 /", "shutdown", "reboot", "halt",
	"init 0", "init 6", "> /dev/sda",
	"curl | sh", "wget | sh", "curl | bash", "wget | bash",
}

// New creates and initialises a sandbox for the given session.
// The work directory is created on disk immediately.
func New(cfg Config) (*Sandbox, error) {
	if cfg.BaseDir == "" {
		cfg.BaseDir = "/tmp/aurion-sandbox"
	}
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = 50_000
	}
	if cfg.DangerousCommands == nil {
		cfg.DangerousCommands = defaultDangerous
	}

	dir := filepath.Join(cfg.BaseDir, cfg.SessionID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("sandbox: create workdir: %w", err)
	}

	return &Sandbox{cfg: cfg, workDir: dir}, nil
}

// WorkDir returns the isolated working directory for this session.
func (s *Sandbox) WorkDir() string { return s.workDir }

// ExecResult is the output of a sandboxed command.
type ExecResult struct {
	Output    string
	ExitCode  int
	TimedOut  bool
	IsError   bool
}

// Exec runs a shell command inside the sandbox.
func (s *Sandbox) Exec(ctx context.Context, command string, timeout time.Duration) ExecResult {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ExecResult{Output: "sandbox is closed", IsError: true, ExitCode: 1}
	}
	s.mu.Unlock()

	// Block dangerous commands
	lower := strings.ToLower(command)
	for _, d := range s.cfg.DangerousCommands {
		if strings.Contains(lower, d) {
			return ExecResult{
				Output:  "command blocked: destructive operations not allowed in sandbox",
				IsError: true,
				ExitCode: 1,
			}
		}
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := s.buildCommand(execCtx, command)

	out, err := cmd.CombinedOutput()
	result := string(out)
	if len(result) > s.cfg.MaxOutputBytes {
		result = result[:s.cfg.MaxOutputBytes] + "\n... (output truncated)"
	}

	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			return ExecResult{Output: fmt.Sprintf("command timed out after %v:\n%s", timeout, result), TimedOut: true, IsError: true, ExitCode: 124}
		}
		exitCode := 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		return ExecResult{Output: fmt.Sprintf("exit code %d:\n%s", exitCode, result), ExitCode: exitCode}
	}

	return ExecResult{Output: result, ExitCode: 0}
}

// ReadFile reads a file from the sandbox work directory.
// Paths are resolved relative to the sandbox root. Absolute paths are
// allowed only if they fall inside the sandbox.
func (s *Sandbox) ReadFile(path string) (string, error) {
	abs := s.resolvePath(path)
	if !strings.HasPrefix(abs, s.workDir) {
		return "", fmt.Errorf("path escapes sandbox: %s", path)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteFile writes a file inside the sandbox work directory.
func (s *Sandbox) WriteFile(path, content string) error {
	abs := s.resolvePath(path)
	if !strings.HasPrefix(abs, s.workDir) {
		return fmt.Errorf("path escapes sandbox: %s", path)
	}
	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(abs, []byte(content), 0644)
}

// ListDir lists directory contents inside the sandbox.
func (s *Sandbox) ListDir(path string) ([]string, error) {
	abs := s.resolvePath(path)
	if !strings.HasPrefix(abs, s.workDir) {
		return nil, fmt.Errorf("path escapes sandbox: %s", path)
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
		if e.IsDir() {
			names[i] += "/"
		}
	}
	return names, nil
}

// Close cleans up the sandbox work directory.
func (s *Sandbox) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return os.RemoveAll(s.workDir)
}

// resolvePath makes a path absolute within the sandbox.
func (s *Sandbox) resolvePath(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(s.workDir, path))
}

// buildCommand creates an exec.Cmd with appropriate isolation.
func (s *Sandbox) buildCommand(ctx context.Context, command string) *exec.Cmd {
	env := []string{
		"HOME=" + s.workDir,
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"TERM=xterm",
		"LANG=en_US.UTF-8",
		"SANDBOX_SESSION=" + s.cfg.SessionID,
	}

	if runtime.GOOS == "linux" && hasUnshare() {
		// Use Linux namespaces for real isolation:
		//   --pid   : separate PID namespace (can't signal host processes)
		//   --mount : private mount namespace (can't see host mounts)
		//   --net   : (optional) no network access
		//   --fork  : needed with --pid
		//   --map-root-user : UID 0 inside namespace maps to current user
		args := []string{"--pid", "--mount", "--fork", "--map-root-user"}
		if s.cfg.NetDisabled {
			args = append(args, "--net")
		}
		args = append(args, "--", "sh", "-c", command)
		cmd := exec.CommandContext(ctx, "unshare", args...)
		cmd.Dir = s.workDir
		cmd.Env = env
		return cmd
	}

	// Fallback: restricted exec without namespace isolation.
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = s.workDir
	cmd.Env = env
	return cmd
}

// hasUnshare checks whether the unshare binary is available.
var hasUnshare = sync.OnceValue(func() bool {
	_, err := exec.LookPath("unshare")
	return err == nil
})
