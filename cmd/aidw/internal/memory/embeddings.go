package memory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// EmbeddingClient handles generating vector embeddings by delegating to a shell command.
type EmbeddingClient struct {
	command string
}

// NewEmbeddingClient looks for a bridge script at ~/.claude/get-embeddings.sh
func NewEmbeddingClient() (*EmbeddingClient, error) {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".claude", "get-embeddings.sh")

	// If the script doesn't exist, we'll try to use a default environment variable
	if _, err := os.Stat(path); err != nil {
		cmd := os.Getenv("AIDW_EMBEDDING_CMD")
		if cmd == "" {
			return nil, fmt.Errorf("embedding bridge script not found at %s and AIDW_EMBEDDING_CMD not set", path)
		}
		return &EmbeddingClient{command: cmd}, nil
	}

	return &EmbeddingClient{command: path}, nil
}

// Embed generates a vector for the given text by executing the command and passing text on stdin.
func (c *EmbeddingClient) Embed(text string) ([]float32, error) {
	cmd := exec.Command("bash", "-c", c.command)
	cmd.Stdin = bytes.NewBufferString(text)

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("embedding command failed: %w (stderr: %s)", err, stderr.String())
	}

	var values []float32
	if err := json.Unmarshal(out.Bytes(), &values); err != nil {
		return nil, fmt.Errorf("failed to parse embedding output: %w (output: %s)", err, out.String())
	}

	if len(values) == 0 {
		return nil, fmt.Errorf("empty embedding returned")
	}

	return values, nil
}

