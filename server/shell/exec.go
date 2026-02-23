package shell

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Run executes a command and returns its stdout output.
func Run(name string, args ...string) (string, error) {
	return RunWithTimeout(30*time.Second, name, args...)
}

// RunWithTimeout executes a command with a timeout and returns its stdout output.
func RunWithTimeout(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("command timed out after %v", timeout)
		}
		return "", fmt.Errorf("command failed: %v, stderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// curlProgressRe matches curl's progress meter lines that leak into stderr.
var curlProgressRe = regexp.MustCompile(`(?m)^\s*%\s+Total.*$|^\s*\d+\s+\d+\s+\d+\s+\d+\s+\d+\s+\d+.*$`)

// CleanStderr strips noisy tool output (e.g. curl progress) from an error
// message so the user sees only the meaningful part.
func CleanStderr(msg string) string {
	// Remove curl progress meter lines
	msg = curlProgressRe.ReplaceAllString(msg, "")
	// Collapse multiple blank lines / whitespace runs
	msg = regexp.MustCompile(`\n{3,}`).ReplaceAllString(msg, "\n\n")
	return strings.TrimSpace(msg)
}
