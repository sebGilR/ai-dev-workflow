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

// MCPMergeResult reports what MergeMCPJSONTo applied.
type MCPMergeResult struct {
	Added   []string
	Updated []string
}

// MergeMCPJSON merges the hardcoded MCP server definitions into
// ~/.claude/mcp.json and prints a status message to stdout. Existing entries
// are only overwritten when their command or args differ from the canonical
// value (stale detection).
func MergeMCPJSON() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	result, err := MergeMCPJSONTo(filepath.Join(home, ".claude", "mcp.json"))
	if err != nil {
		return err
	}
	if len(result.Added) == 0 && len(result.Updated) == 0 {
		fmt.Println("MCP servers already configured (no changes made).")
		return nil
	}
	if len(result.Updated) > 0 {
		fmt.Printf("MCP servers updated (config corrected): %s\n", joinNames(result.Updated))
	}
	if len(result.Added) > 0 {
		fmt.Printf("MCP servers added: %s\n", joinNames(result.Added))
	}
	return nil
}

// MergeMCPJSONTo merges the hardcoded MCP server definitions into the given
// mcp.json path. It returns a structured result without printing anything.
// Existing entries are only overwritten when their command or args differ from
// the canonical value (stale detection).
func MergeMCPJSONTo(mcpPath string) (MCPMergeResult, error) {
	var result MCPMergeResult

	var data map[string]any
	if raw, err := os.ReadFile(mcpPath); err == nil {
		if jerr := json.Unmarshal(raw, &data); jerr != nil {
			return result, fmt.Errorf("invalid JSON in %s: %w", mcpPath, jerr)
		}
	} else if os.IsNotExist(err) {
		data = map[string]any{}
	} else {
		return result, fmt.Errorf("read %s: %w", mcpPath, err)
	}

	servers, _ := data["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
		data["mcpServers"] = servers
	}

	for name, cfg := range mcpServers {
		existing, ok := servers[name]
		if !ok {
			servers[name] = cfg
			result.Added = append(result.Added, name)
			continue
		}
		existingMap, _ := existing.(map[string]any)
		if existingMap == nil {
			// Non-map entry (corrupted config) — replace entirely.
			servers[name] = cfg
			result.Updated = append(result.Updated, name)
			continue
		}
		if isStale(existingMap, cfg) {
			// Preserve user-added fields; only update managed keys.
			existingMap["command"] = cfg["command"]
			existingMap["args"] = cfg["args"]
			result.Updated = append(result.Updated, name)
		}
	}

	if len(result.Added) == 0 && len(result.Updated) == 0 {
		return result, nil
	}

	if err := os.MkdirAll(filepath.Dir(mcpPath), 0o755); err != nil {
		return result, err
	}
	out, _ := json.MarshalIndent(data, "", "  ")
	tmp := mcpPath + ".tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o644); err != nil {
		return result, err
	}
	if err := os.Rename(tmp, mcpPath); err != nil {
		return result, err
	}

	return result, nil
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
