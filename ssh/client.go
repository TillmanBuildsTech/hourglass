package ssh

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// execTimeout bounds a single SSH invocation end-to-end (DNS resolution,
// TCP connect, auth, and command execution). ConnectTimeout below only
// bounds the TCP connect phase - on some platforms/OpenSSH versions a
// stalled DNS lookup (routine right after a machine wakes from sleep,
// before networking has fully reinitialized) is not covered by it and
// can hang far longer. Without this ceiling, a single stuck poll can
// block the next one from ever running.
const execTimeout = 20 * time.Second

type Client struct {
	Host    string
	Port    int
	User    string
	KeyPath string
}

func NewClient(host string, port int, user, keyPath string) (*Client, error) {
	if port == 0 {
		port = 22
	}
	return &Client{
		Host:    host,
		Port:    port,
		User:    user,
		KeyPath: keyPath,
	}, nil
}

// controlPath returns a stable per-connection path for SSH's control
// socket, used to multiplex repeated invocations over one authenticated
// connection instead of re-authenticating (and re-prompting for a key
// passphrase) on every single call.
func controlPath(host string, port int, user string) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%s:%d:%s", host, port, user))
	dir := filepath.Join(os.TempDir(), "hourglass-ssh")
	os.MkdirAll(dir, 0700)
	return filepath.Join(dir, hex.EncodeToString(sum[:])[:16]+".sock")
}

// commonArgs returns SSH options shared by every invocation:
//   - ConnectTimeout bounds the TCP connect phase.
//   - ServerAliveInterval/CountMax detect a connection gone dead mid-session
//     (e.g. after the machine sleeps) within ~10s instead of relying on the
//     OS's default TCP retransmission timeout, which can be minutes.
//   - ControlMaster/ControlPersist let successive calls reuse one
//     authenticated connection. When the underlying connection dies, the
//     next call transparently establishes a fresh master (prompting for a
//     passphrase again if needed) exactly once, rather than every poll
//     independently retrying a cold handshake.
func commonArgs(host string, port int, user, keyPath string) []string {
	return []string{
		"-i", keyPath,
		"-p", fmt.Sprintf("%d", port),
		"-o", "StrictHostKeyChecking=no",
		"-o", "ConnectTimeout=10",
		"-o", "ServerAliveInterval=5",
		"-o", "ServerAliveCountMax=2",
		"-o", "ControlMaster=auto",
		"-o", "ControlPersist=60s",
		"-o", "ControlPath=" + controlPath(host, port, user),
	}
}

func (c *Client) Execute(command string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()

	args := commonArgs(c.Host, c.Port, c.User, c.KeyPath)
	args = append(args, fmt.Sprintf("%s@%s", c.User, c.Host), command)
	sshCmd := exec.CommandContext(ctx, "ssh", args...)

	var out bytes.Buffer
	var errOut bytes.Buffer
	sshCmd.Stdout = &out
	sshCmd.Stderr = &errOut

	err := sshCmd.Run()
	output := out.String()
	errOutput := errOut.String()

	// For crontab -l, exit code 1 with "no crontab" message is not an error
	if err != nil && strings.Contains(errOutput, "no crontab") {
		return "", nil
	}

	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("SSH command timed out after %s", execTimeout)
	}

	if err != nil {
		return output + errOutput, err
	}

	return output, nil
}

func (c *Client) Connect() error {
	// Just test the connection
	return TestConnection(c.Host, c.Port, c.User, c.KeyPath)
}

// Close tears down the shared control connection (if any), so a stale
// master isn't left behind once this client is no longer in use.
func (c *Client) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	args := []string{
		"-O", "exit",
		"-o", "ControlPath=" + controlPath(c.Host, c.Port, c.User),
		fmt.Sprintf("%s@%s", c.User, c.Host),
	}
	// Best-effort: if there's no master running, this simply errors, which
	// is expected and not worth surfacing.
	_ = exec.CommandContext(ctx, "ssh", args...).Run()
	return nil
}

func (c *Client) IsConnected() bool {
	return true
}

func TestConnection(host string, port int, user, keyPath string) error {
	if port == 0 {
		port = 22
	}

	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()

	args := commonArgs(host, port, user, keyPath)
	args = append(args, fmt.Sprintf("%s@%s", user, host), "true")
	cmd := exec.CommandContext(ctx, "ssh", args...)

	var errOut bytes.Buffer
	cmd.Stderr = &errOut

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("SSH connection timed out after %s", execTimeout)
		}
		errMsg := errOut.String()
		if errMsg != "" {
			return fmt.Errorf("SSH connection failed: %s", strings.TrimSpace(errMsg))
		}
		return fmt.Errorf("SSH connection failed: %w", err)
	}

	return nil
}
