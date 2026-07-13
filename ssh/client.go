package ssh

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

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

func (c *Client) Execute(command string) (string, error) {
	// Use ssh command directly - handles SSH agent, key files, everything
	sshCmd := exec.Command(
		"ssh",
		"-i", c.KeyPath,
		"-p", fmt.Sprintf("%d", c.Port),
		"-o", "StrictHostKeyChecking=no",
		"-o", "ConnectTimeout=10",
		fmt.Sprintf("%s@%s", c.User, c.Host),
		command,
	)

	var out bytes.Buffer
	var errOut bytes.Buffer
	sshCmd.Stdout = &out
	sshCmd.Stderr = &errOut

	err := sshCmd.Run()
	output := out.String()

	if err != nil {
		return output + errOut.String(), err
	}

	return output, nil
}

func (c *Client) Connect() error {
	// Just test the connection
	return TestConnection(c.Host, c.Port, c.User, c.KeyPath)
}

func (c *Client) Close() error {
	return nil
}

func (c *Client) IsConnected() bool {
	return true
}

func TestConnection(host string, port int, user, keyPath string) error {
	if port == 0 {
		port = 22
	}

	// Use ssh command directly to test connection
	cmd := exec.Command(
		"ssh",
		"-i", keyPath,
		"-p", fmt.Sprintf("%d", port),
		"-o", "StrictHostKeyChecking=no",
		"-o", "ConnectTimeout=10",
		fmt.Sprintf("%s@%s", user, host),
		"true",
	)

	var errOut bytes.Buffer
	cmd.Stderr = &errOut

	if err := cmd.Run(); err != nil {
		errMsg := errOut.String()
		if errMsg != "" {
			return fmt.Errorf("SSH connection failed: %s", strings.TrimSpace(errMsg))
		}
		return fmt.Errorf("SSH connection failed: %w", err)
	}

	return nil
}
