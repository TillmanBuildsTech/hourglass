package ssh

import (
	"bytes"
	"fmt"
	"os"
	"time"

	"golang.org/x/crypto/ssh"
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

func (c *Client) Connect() error {
	key, err := os.ReadFile(c.KeyPath)
	if err != nil {
		return fmt.Errorf("failed to read SSH key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return fmt.Errorf("failed to parse SSH key: %w", err)
	}

	config := &ssh.ClientConfig{
		User: c.User,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
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
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("failed to read SSH key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return fmt.Errorf("failed to parse SSH key: %w", err)
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	if port == 0 {
		port = 22
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return err
	}
	defer conn.Close()

	return nil
}
