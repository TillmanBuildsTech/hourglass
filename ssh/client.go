package ssh

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

type Client struct {
	Host       string
	Port       int
	User       string
	KeyPath    string
	connection *ssh.Client
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

func (c *Client) getAuthMethods() ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	// Try SSH agent if available
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if ag, err := net.Dial("unix", sock); err == nil {
			agentClient := agent.NewClient(ag)
			methods = append(methods, ssh.PublicKeysCallback(agentClient.Signers))
		}
	}

	// Try key file (unencrypted only)
	if c.KeyPath != "" {
		key, err := os.ReadFile(c.KeyPath)
		if err == nil {
			if signer, err := ssh.ParsePrivateKey(key); err == nil {
				methods = append(methods, ssh.PublicKeys(signer))
			}
		}
	}

	if len(methods) == 0 {
		return nil, fmt.Errorf("no valid SSH keys found; use unencrypted keys or configure SSH agent")
	}

	return methods, nil
}

func (c *Client) Connect() error {
	authMethods, err := c.getAuthMethods()
	if err != nil {
		return err
	}

	config := &ssh.ClientConfig{
		User:            c.User,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", c.Host, c.Port)
	conn, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", addr, err)
	}

	c.connection = conn
	return nil
}

func (c *Client) Execute(command string) (string, error) {
	if c.connection == nil {
		return "", fmt.Errorf("not connected")
	}

	// Create new session for each command
	session, err := c.connection.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	var out bytes.Buffer
	var errOut bytes.Buffer
	session.Stdout = &out
	session.Stderr = &errOut

	err = session.Run(command)
	output := out.String()

	if err != nil {
		return output + errOut.String(), err
	}

	return output, nil
}

func (c *Client) Close() error {
	if c.connection != nil {
		return c.connection.Close()
	}
	return nil
}

func (c *Client) IsConnected() bool {
	return c.connection != nil
}

func TestConnection(host string, port int, user, keyPath string) error {
	if port == 0 {
		port = 22
	}

	// Try key file (unencrypted only)
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("failed to read SSH key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return fmt.Errorf("failed to parse SSH key: %w - ensure key is unencrypted", err)
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return err
	}
	defer conn.Close()

	return nil
}
