package install

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"aidw/cmd/aidw/internal/util"
)

// mcpServers contains the hardcoded servers to add/update.
var mcpServers = map[string]map[string]any{
	"serena": {
		"command": "serena",
		"args": []any{
			"--context=claude-code",
			"start-mcp-server",
		},
	},
	"context7": {
		"command": "npx",
		"args":    []any{"-y", "@upstash/context7-mcp@2.1.3"},
	},
	"sequential-thinking": {
		"command": "npx",
		"args":    []any{"-y", "@anthropic-ai/sequential-thinking-mcp"},
	},
}

// MergeMCPJSON merges the hardcoded MCP server definitions into
// ~/.claude/mcp.json. Existing entries are only overwritten when their
// command or args differ from the canonical value (stale detection).
// Informational messages are written to w (pass os.Stderr from commands that
// output structured JSON to stdout, or os.Stdout for interactive use).
func MergeMCPJSON(w io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return mergeMCPJSONToPath(filepath.Join(home, ".claude", "mcp.json"), w)
}

// mergeMCPJSONToPath is the path-injectable core used by both MergeMCPJSON
// and tests.
func mergeMCPJSONToPath(mcpPath string, w io.Writer) error {
	var data map[string]any
	if raw, err := os.ReadFile(mcpPath); err == nil {
		if jerr := json.Unmarshal(raw, &data); jerr != nil {
			return fmt.Errorf("invalid JSON in %s: %w", mcpPath, jerr)
		}
	} else if os.IsNotExist(err) {
		data = map[string]any{}
	} else {
		return fmt.Errorf("read %s: %w", mcpPath, err)
	}

	servers, _ := data["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
		data["mcpServers"] = servers
	}

	var added, updated []string
	for name, cfg := range mcpServers {
		existing, ok := servers[name]
		if !ok {
			servers[name] = cfg
			added = append(added, name)
			continue
		}
		existingMap, _ := existing.(map[string]any)
		if existingMap == nil {
			// Non-map entry (corrupted config) — replace entirely.
			servers[name] = cfg
			updated = append(updated, name)
			continue
		}
		if isStale(existingMap, cfg) {
			// Preserve user-added fields; only update managed keys.
			existingMap["command"] = cfg["command"]
			existingMap["args"] = cfg["args"]
			updated = append(updated, name)
		}
	}

	if len(added) == 0 && len(updated) == 0 {
		fmt.Fprintln(w, "MCP servers already configured (no changes made).")
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(mcpPath), 0o755); err != nil {
		return err
	}
	if err := util.WriteJSON(mcpPath, data); err != nil {
		return err
	}

	if len(updated) > 0 {
		fmt.Fprintf(w, "MCP servers updated (config corrected): %s\n", joinNames(updated))
	}
	if len(added) > 0 {
		fmt.Fprintf(w, "MCP servers added: %s\n", joinNames(added))
	}
	return nil
}

// isStale reports whether an existing server entry differs from the canonical one.
func isStale(existing, canonical map[string]any) bool {
	return existing["command"] != canonical["command"] ||
		!reflect.DeepEqual(existing["args"], canonical["args"])
}

func joinNames(names []string) string {
	result := ""
	for i, n := range names {
		if i > 0 {
			result += ", "
		}
		result += n
	}
	return result
}
