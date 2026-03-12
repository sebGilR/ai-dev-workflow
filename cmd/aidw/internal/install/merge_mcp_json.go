package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

// mcpServers contains the hardcoded servers to add/update.
var mcpServers = map[string]map[string]any{
	"serena": {
		"command": "uvx",
		"args": []any{
			"--from",
			"git+https://github.com/oraios/serena@v0.1.4",
			"serena",
			"start-mcp-server",
		},
	},
	"context7": {
		"command": "npx",
		"args":    []any{"-y", "@upstash/context7-mcp@2.1.3"},
	},
}

// MergeMCPJSON merges the hardcoded MCP server definitions into
// ~/.claude/mcp.json. Existing entries are only overwritten when their
// command or args differ from the canonical value (stale detection).
func MergeMCPJSON() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return mergeMCPJSONToPath(filepath.Join(home, ".claude", "mcp.json"))
}

// mergeMCPJSONToPath is the path-injectable core used by both MergeMCPJSON
// and tests.
func mergeMCPJSONToPath(mcpPath string) error {
	var data map[string]any
	if raw, err := os.ReadFile(mcpPath); err == nil {
		if jerr := json.Unmarshal(raw, &data); jerr != nil {
			return fmt.Errorf("invalid JSON in %s: %w", mcpPath, jerr)
		}
	} else {
		data = map[string]any{}
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
		if existingMap == nil || isStale(existingMap, cfg) {
			servers[name] = cfg
			updated = append(updated, name)
		}
	}

	if len(added) == 0 && len(updated) == 0 {
		fmt.Println("MCP servers already configured (no changes made).")
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(mcpPath), 0o755); err != nil {
		return err
	}
	out, _ := json.MarshalIndent(data, "", "  ")
	tmp := mcpPath + ".tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, mcpPath); err != nil {
		return err
	}

	if len(updated) > 0 {
		fmt.Printf("MCP servers updated (config corrected): %s\n", joinNames(updated))
	}
	if len(added) > 0 {
		fmt.Printf("MCP servers added: %s\n", joinNames(added))
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
